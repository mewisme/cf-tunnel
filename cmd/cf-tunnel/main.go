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

	cftunnel "github.com/mewisme/cf-tunnel"
	"github.com/mewisme/cf-tunnel/internal/buildinfo"
)

type lifecycleEvent struct {
	State  string `json:"state"`
	Origin string `json:"origin,omitempty"`
	URL    string `json:"url,omitempty"`
	Error  string `json:"error,omitempty"`
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
	version := flags.Bool("version", false, "print runtime version")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *version {
		_, _ = fmt.Fprintln(stdout, buildinfo.Version())
		return 0
	}
	if strings.TrimSpace(*origin) == "" {
		_, _ = fmt.Fprintln(stderr, "origin is required")
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return runner{start: startQuickTunnel, out: stdout}.run(ctx, strings.TrimSpace(*origin))
}

func startQuickTunnel(ctx context.Context, cfg cftunnel.Config) (runtimeTunnel, error) {
	return cftunnel.Start(ctx, cfg)
}

func (r runner) run(ctx context.Context, origin string) int {
	encoder := json.NewEncoder(r.out)
	if err := encoder.Encode(lifecycleEvent{State: "connecting", Origin: origin}); err != nil {
		return 1
	}
	tunnel, err := r.start(ctx, cftunnel.Config{OriginURL: origin})
	if err != nil {
		_ = encoder.Encode(lifecycleEvent{State: "degraded", Origin: origin, Error: "quick tunnel start failed"})
		return 1
	}
	if err := tunnel.WaitUntilReady(ctx); err != nil {
		if ctx.Err() != nil {
			_ = encoder.Encode(lifecycleEvent{State: "stopped", Origin: origin})
			return 0
		}
		_ = encoder.Encode(lifecycleEvent{State: "degraded", Origin: origin, Error: "quick tunnel connection failed"})
		return 1
	}
	if err := encoder.Encode(lifecycleEvent{State: "ready", Origin: origin, URL: tunnel.PublicURL()}); err != nil {
		return 1
	}
	err = tunnel.Wait()
	if ctx.Err() != nil {
		_ = encoder.Encode(lifecycleEvent{State: "stopped", Origin: origin})
		return 0
	}
	_ = err
	_ = encoder.Encode(lifecycleEvent{State: "degraded", Origin: origin, Error: "quick tunnel connection stopped unexpectedly"})
	return 1
}
