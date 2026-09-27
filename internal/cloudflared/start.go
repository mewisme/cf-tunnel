package cloudflared

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"

	"github.com/mewisme/cf-tunnel/internal/cloudflared/client"
	cfconfig "github.com/mewisme/cf-tunnel/internal/cloudflared/config"
	"github.com/mewisme/cf-tunnel/internal/cloudflared/connection"
	"github.com/mewisme/cf-tunnel/internal/cloudflared/edgediscovery/allregions"
	"github.com/mewisme/cf-tunnel/internal/cloudflared/features"
	"github.com/mewisme/cf-tunnel/internal/cloudflared/ingress"
	"github.com/mewisme/cf-tunnel/internal/cloudflared/ingress/origins"
	"github.com/mewisme/cf-tunnel/internal/cloudflared/orchestration"
	"github.com/mewisme/cf-tunnel/internal/cloudflared/signal"
	"github.com/mewisme/cf-tunnel/internal/cloudflared/supervisor"
	"github.com/mewisme/cf-tunnel/internal/cloudflared/tlsconfig"
	"github.com/mewisme/cf-tunnel/internal/cloudflared/tunnelrpc/pogs"
)

const (
	reportedVersion = "2026.9.1"
	httpTimeout     = 15 * time.Second
)

// Config starts one Quick Tunnel to a single HTTP origin.
type Config struct {
	OriginURL string
	// QuickService defaults to DefaultQuickService. Tests may override.
	QuickService string
}

// Tunnel is one ephemeral Quick Tunnel. Cancel the Start context to shut it down.
type Tunnel struct {
	URL string

	ready <-chan struct{}
	done  chan struct{}
	once  sync.Once
	mu    sync.RWMutex
	err   error
}

func (t *Tunnel) Wait() error {
	if t == nil || t.done == nil {
		return fmt.Errorf("quick tunnel is not initialized")
	}
	<-t.done
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.err

}

func (t *Tunnel) WaitUntilReady(ctx context.Context) error {
	if t == nil || t.ready == nil || t.done == nil {
		return fmt.Errorf("quick tunnel is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-t.ready:
		return nil
	case <-t.done:
		t.mu.RLock()
		err := t.err
		t.mu.RUnlock()
		if err != nil {
			return err
		}
		return fmt.Errorf("quick tunnel stopped before becoming ready")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (t *Tunnel) complete(err error) {
	if t == nil || t.done == nil {
		return
	}
	t.once.Do(func() {
		t.mu.Lock()
		t.err = err
		t.mu.Unlock()
		close(t.done)
	})
}

// Stub returns a Tunnel that never talks to Cloudflare. Tests use it as Start.
func Stub(url string) *Tunnel {
	ready := make(chan struct{})
	close(ready)
	return &Tunnel{URL: strings.TrimSpace(url), ready: ready, done: make(chan struct{})}
}

func (t *Tunnel) Complete(err error) {
	t.complete(err)
}

func Start(ctx context.Context, cfg Config) (*Tunnel, error) {
	origin, err := validateOriginURL(cfg.OriginURL)
	if err != nil {
		return nil, err
	}
	service := strings.TrimRight(cfg.QuickService, "/")
	if service == "" {
		service = DefaultQuickService
	}

	provisioned, err := provisionQuickTunnel(ctx, service)
	if err != nil {
		return nil, err
	}

	log := zerolog.New(io.Discard)
	ing, err := ingress.ParseIngress(&cfconfig.Configuration{
		Ingress: []cfconfig.UnvalidatedIngressRule{{Service: origin}},
	})
	if err != nil {
		return nil, err
	}
	if err := ing.StartOrigins(&log, ctx.Done()); err != nil {
		return nil, err
	}

	featureSelector, err := features.NewFeatureSelector(ctx, provisioned.credentials.AccountTag, nil, false, &log)
	if err != nil {
		return nil, err
	}
	clientConfig, err := client.NewConfig(reportedVersion, runtime.GOOS+"_"+runtime.GOARCH, featureSelector)
	if err != nil {
		return nil, err
	}

	protocolSelector, err := connection.NewProtocolSelector("quic", &log)
	if err != nil {
		return nil, err
	}
	edgeTLSConfigs := make(map[connection.Protocol]*tls.Config, len(connection.ProtocolList))
	for _, p := range connection.ProtocolList {
		tlsSettings := p.TLSSettings()
		if tlsSettings == nil {
			return nil, fmt.Errorf("%s has unknown TLS settings", p)
		}
		edgeTLSConfig, err := tlsconfig.CreateTunnelConfig("", tlsSettings.ServerName)
		if err != nil {
			return nil, err
		}
		if len(tlsSettings.NextProtos) > 0 {
			edgeTLSConfig.NextProtos = tlsSettings.NextProtos
		}
		edgeTLSConfigs[p] = edgeTLSConfig
	}

	warpRouting := ingress.NewWarpRoutingConfig(&cfconfig.WarpRoutingConfig{})
	originDialer := ingress.NewOriginDialer(ingress.OriginConfig{
		DefaultDialer: ingress.NewDialer(warpRouting),
	}, &log)
	dnsService := origins.NewDNSResolverService(origins.NewDNSDialer(), &log, origins.NewMetrics(prometheus.NewRegistry()))
	originDialer.AddReservedService(dnsService, []netip.AddrPort{origins.VirtualDNSServiceAddr})

	observer := connection.NewObserver(&log)
	observer.SendURL(provisioned.hostname)
	namedTunnel := &connection.TunnelProperties{
		Credentials:    provisioned.credentials,
		QuickTunnelUrl: provisioned.hostname,
	}
	tunnelConfig := &supervisor.TunnelConfig{
		ClientConfig:                        clientConfig,
		GracePeriod:                         30 * time.Second,
		CloseConnOnce:                       &sync.Once{},
		EdgeIPVersion:                       allregions.Auto,
		HAConnections:                       1,
		Tags:                                []pogs.Tag{{Name: "ID", Value: clientConfig.ConnectorID.String()}},
		Log:                                 &log,
		Observer:                            observer,
		ReportedVersion:                     reportedVersion,
		Retries:                             5,
		NamedTunnel:                         namedTunnel,
		ProtocolSelector:                    protocolSelector,
		EdgeTLSConfigs:                      edgeTLSConfigs,
		MaxEdgeAddrRetries:                  8,
		RPCTimeout:                          5 * time.Second,
		QUICConnectionLevelFlowControlLimit: 30 * (1 << 20),
		QUICStreamLevelFlowControlLimit:     6 * (1 << 20),
		OriginDNSService:                    dnsService,
		OriginDialerService:                 originDialer,
	}
	orchestrator, err := orchestration.NewOrchestrator(ctx, &orchestration.Config{
		Ingress:             &ing,
		WarpRouting:         warpRouting,
		OriginDialerService: originDialer,
	}, tunnelConfig.Tags, nil, &log)
	if err != nil {
		return nil, err
	}

	connectedSignal := signal.New(make(chan struct{}))
	t := &Tunnel{
		URL:   provisioned.url,
		ready: connectedSignal.Wait(),
		done:  make(chan struct{}),
	}
	go func() {
		t.complete(supervisor.StartTunnelDaemon(ctx, tunnelConfig, orchestrator, connectedSignal, ctx.Done()))
	}()
	return t, nil
}

func validateOriginURL(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("origin URL is required")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid origin URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("origin URL must be http or https")
	}
	return u.String(), nil
}

func provisionQuickTunnel(ctx context.Context, service string) (*provision, error) {
	httpClient := http.Client{
		Transport: &http.Transport{
			TLSHandshakeTimeout:   httpTimeout,
			ResponseHeaderTimeout: httpTimeout,
		},
		Timeout: httpTimeout,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, service+"/tunnel", bytes.NewReader(nil))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to request quick Tunnel")
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read quick-tunnel response")
	}
	return parseProvisionResponse(resp.StatusCode, body)
}
