package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	cftunnel "go.mewis.me/cf-tunnel"
	"go.mewis.me/cf-tunnel/internal/buildinfo"
)

type lifecycleEvent struct {
	State    string `json:"state"`
	Origin   string `json:"origin,omitempty"`
	URL      string `json:"url,omitempty"`
	TunnelID string `json:"tunnel_id,omitempty"`
	Error    string `json:"error,omitempty"`
}

type runtimeTunnel interface {
	PublicURL() string
	WaitUntilReady(context.Context) error
	Wait() error
}

type startFunc func(context.Context, cftunnel.Config) (runtimeTunnel, error)

type runner struct {
	start startFunc
	out   io.Writer
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("cf-tunnel", flag.ContinueOnError)
	flags.SetOutput(stderr)
	origin := flags.String("origin", "", "single local HTTP origin to expose")
	token := flags.String("token", "", "Dashboard-managed Cloudflare tunnel token (prefer CF_TUNNEL_TOKEN environment variable)")
	version := flags.Bool("version", false, "print runtime version")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *version {
		_, _ = fmt.Fprintln(stdout, buildinfo.Version())
		return 0
	}
	cfg := cftunnel.Config{OriginURL: strings.TrimSpace(*origin), Token: strings.TrimSpace(*token)}
	if cfg.Token == "" && cfg.OriginURL == "" {
		cfg.Token = strings.TrimSpace(os.Getenv("CF_TUNNEL_TOKEN"))
	}
	if cfg.OriginURL == "" && cfg.Token == "" {
		_, _ = fmt.Fprintln(stderr, "origin or token is required")
		return 2
	}
	if cfg.OriginURL != "" && cfg.Token != "" {
		_, _ = fmt.Fprintln(stderr, "origin and token cannot be combined")
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return runner{start: startQuickTunnel, out: stdout}.runConfig(ctx, cfg)
}

func startQuickTunnel(ctx context.Context, cfg cftunnel.Config) (runtimeTunnel, error) {
	return cftunnel.Start(ctx, cfg)
}

func (r runner) run(ctx context.Context, origin string) int {
	return r.runConfig(ctx, cftunnel.Config{OriginURL: origin})
}

func (r runner) runConfig(ctx context.Context, cfg cftunnel.Config) int {
	encoder := json.NewEncoder(r.out)
	event := lifecycleEvent{State: "connecting", Origin: cfg.OriginURL}
	mode := "quick tunnel"
	if cfg.Token != "" {
		mode = "managed tunnel"
	}
	if err := encoder.Encode(event); err != nil {
		return 1
	}
	tunnel, err := r.start(ctx, cfg)
	if err != nil {
		_ = encoder.Encode(lifecycleEvent{State: "degraded", Origin: cfg.OriginURL, Error: mode + " start failed"})
		return 1
	}
	if identified, ok := tunnel.(interface{ TunnelID() string }); ok {
		event.TunnelID = identified.TunnelID()
	}
	if err := tunnel.WaitUntilReady(ctx); err != nil {
		if ctx.Err() != nil {
			event.State = "stopped"
			_ = encoder.Encode(event)
			return 0
		}
		event.State = "degraded"
		event.Error = mode + " connection failed"
		_ = encoder.Encode(event)
		return 1
	}
	event.State = "ready"
	event.URL = tunnel.PublicURL()
	if err := encoder.Encode(event); err != nil {
		return 1
	}
	err = tunnel.Wait()
	if ctx.Err() != nil {
		event.State = "stopped"
		event.URL = ""
		_ = encoder.Encode(event)
		return 0
	}
	_ = err
	event.State = "degraded"
	event.URL = ""
	event.Error = mode + " connection stopped unexpectedly"
	_ = encoder.Encode(event)
	return 1
}
