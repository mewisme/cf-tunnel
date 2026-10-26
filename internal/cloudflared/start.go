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

	"go.mewis.me/cf-tunnel/internal/buildinfo"
	"go.mewis.me/cf-tunnel/internal/cloudflared/client"
	cfconfig "go.mewis.me/cf-tunnel/internal/cloudflared/config"
	"go.mewis.me/cf-tunnel/internal/cloudflared/connection"
	"go.mewis.me/cf-tunnel/internal/cloudflared/edgediscovery/allregions"
	"go.mewis.me/cf-tunnel/internal/cloudflared/features"
	"go.mewis.me/cf-tunnel/internal/cloudflared/ingress"
	"go.mewis.me/cf-tunnel/internal/cloudflared/ingress/origins"
	"go.mewis.me/cf-tunnel/internal/cloudflared/orchestration"
	"go.mewis.me/cf-tunnel/internal/cloudflared/signal"
	"go.mewis.me/cf-tunnel/internal/cloudflared/supervisor"
	"go.mewis.me/cf-tunnel/internal/cloudflared/tlsconfig"
	"go.mewis.me/cf-tunnel/internal/cloudflared/tunnelrpc/pogs"
)

const (
	httpTimeout = 15 * time.Second
)

// Config starts one Quick Tunnel to a single HTTP origin or in-memory handler.
type Config struct {
	OriginURL string
	Handler   http.Handler
	Token     string
	// QuickService defaults to DefaultQuickService. Tests may override.
	QuickService string
}

// Tunnel is one ephemeral Quick Tunnel. Cancel the Start context to shut it down.
type Tunnel struct {
	URL string
	ID  string

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
	ing, err := newIngress(cfg)
	if err != nil {
		return nil, err
	}
	var managed *connection.Credentials
	if cfg.Token != "" {
		creds, err := parseTunnelToken(cfg.Token)
		if err != nil {
			return nil, err
		}
		managed = &creds
	}
	service := strings.TrimRight(cfg.QuickService, "/")
	if service == "" {
		service = DefaultQuickService
	}

	var provisioned *provision
	if managed == nil {
		provisioned, err = provisionQuickTunnel(ctx, service)
		if err != nil {
			return nil, err
		}
	}

	log := zerolog.New(io.Discard)
	if err := ing.StartOrigins(&log, ctx.Done()); err != nil {
		return nil, err
	}

	credentials := connection.Credentials{}
	if managed == nil {
		credentials = provisioned.credentials
	} else {
		credentials = *managed
	}
	featureSelector, err := features.NewFeatureSelector(ctx, credentials.AccountTag, nil, false, &log)
	if err != nil {
		return nil, err
	}
	reportedVersion := buildinfo.EdgeVersion()
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
	namedTunnel := &connection.TunnelProperties{Credentials: credentials}
	if provisioned != nil {
		observer.SendURL(provisioned.hostname)
		namedTunnel.QuickTunnelUrl = provisioned.hostname
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
		ready: connectedSignal.Wait(),
		done:  make(chan struct{}),
	}
	if provisioned != nil {
		t.URL = provisioned.url
	} else {
		t.ID = credentials.TunnelID.String()
	}
	go func() {
		t.complete(supervisor.StartTunnelDaemon(ctx, tunnelConfig, orchestrator, connectedSignal, ctx.Done()))
	}()
	return t, nil
}

func newIngress(cfg Config) (ingress.Ingress, error) {
	if cfg.Token != "" {
		if cfg.OriginURL != "" || cfg.Handler != nil {
			return ingress.Ingress{}, fmt.Errorf("token cannot be combined with origin URL or handler")
		}
		if cfg.QuickService != "" {
			return ingress.Ingress{}, fmt.Errorf("quick service cannot be combined with token")
		}
		// The edge sends the dashboard ingress configuration after connection.
		// Until then the orchestrator serves the default 503 catch-all.
		return ingress.Ingress{}, nil
	}
	if cfg.Handler != nil {
		if cfg.OriginURL != "" {
			return ingress.Ingress{}, fmt.Errorf("origin URL and handler are mutually exclusive")
		}
		return ingress.Ingress{Rules: []ingress.Rule{{Service: ingress.NewHandlerService(cfg.Handler)}}}, nil
	}
	origin, err := validateOriginURL(cfg.OriginURL)
	if err != nil {
		return ingress.Ingress{}, err
	}
	return ingress.ParseIngress(&cfconfig.Configuration{
		Ingress: []cfconfig.UnvalidatedIngressRule{{Service: origin}},
	})
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
	const maxProvisionResponse = 1 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxProvisionResponse+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read quick-tunnel response")
	}
	if len(body) > maxProvisionResponse {
		return nil, fmt.Errorf("quick-tunnel response too large")
	}
	return parseProvisionResponse(resp.StatusCode, body)
}
