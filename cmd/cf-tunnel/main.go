package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

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
	if len(args) > 0 && args[0] == "token" {
		return runTokenCommand(args[1:], stdout, stderr, cftunnel.VerifyToken)
	}
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

type tokenCommandResponse struct {
	cftunnel.VerificationResult
	Error string `json:"error,omitempty"`
}

type verifyFunc func(context.Context, string) (*cftunnel.VerificationResult, error)

// runTokenCommand prints one JSON object; no token or tunnel secret is output.
// "inspect" only decodes credentials, while "verify" makes an authenticated,
// short-lived Cloudflare Edge connection.
func runTokenCommand(args []string, stdout, stderr io.Writer, verify verifyFunc) int {
	if len(args) == 0 || (args[0] != "inspect" && args[0] != "verify") {
		_, _ = fmt.Fprintln(stderr, "usage: cf-tunnel token inspect|verify [--token TOKEN] [--timeout 30s]")
		return 2
	}
	action := args[0]
	flags := flag.NewFlagSet("cf-tunnel token "+action, flag.ContinueOnError)
	flags.SetOutput(stderr)
	raw := flags.String("token", "", "connector token (prefer CF_TUNNEL_TOKEN)")
	timeout := flags.Duration("timeout", 30*time.Second, "verification deadline")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if flags.NArg() > 0 {
		_, _ = fmt.Fprintln(stderr, "unexpected positional argument")
		return 2
	}
	token := strings.TrimSpace(*raw)
	if token == "" {
		token = strings.TrimSpace(os.Getenv("CF_TUNNEL_TOKEN"))
	}
	if token == "" {
		_, _ = fmt.Fprintln(stderr, "token is required (--token or CF_TUNNEL_TOKEN)")
		return 2
	}
	encoder := json.NewEncoder(stdout)
	info, err := cftunnel.ParseToken(token)
	if err != nil {
		_ = encoder.Encode(tokenCommandResponse{Error: "invalid_token"})
		return 1
	}
	if action == "inspect" {
		if err := encoder.Encode(info); err != nil {
			return 1
		}
		return 0
	}
	if *timeout <= 0 {
		_, _ = fmt.Fprintln(stderr, "timeout must be greater than zero")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	result, err := verify(ctx, token)
	if err != nil {
		response := tokenCommandResponse{
			VerificationResult: cftunnel.VerificationResult{TokenInfo: info},
			Error:              "verification_failed",
		}
		if result != nil {
			response.VerificationResult = *result
		}
		response.Verified = false
		if ctx.Err() != nil {
			response.Error = "verification_timeout_or_cancelled"
		}
		_ = encoder.Encode(response)
		return 1
	}
	if result == nil || !result.Verified {
		_ = encoder.Encode(tokenCommandResponse{
			VerificationResult: cftunnel.VerificationResult{TokenInfo: info},
			Error:              "verification_failed",
		})
		return 1
	}
	if err := encoder.Encode(result); err != nil {
		return 1
	}
	return 0
}

func startQuickTunnel(ctx context.Context, cfg cftunnel.Config) (runtimeTunnel, error) {
	return cftunnel.Start(ctx, cfg)
}

func (r runner) run(ctx context.Context, origin string) int {
	return r.runConfig(ctx, cftunnel.Config{OriginURL: origin})
}

func (r runner) runConfig(ctx context.Context, cfg cftunnel.Config) int {
	encoder := json.NewEncoder(r.out)
	event := lifecycleEvent{State: "connecting", Origin: displayOrigin(cfg.OriginURL)}
	mode := "quick tunnel"
	if cfg.Token != "" {
		mode = "managed tunnel"
	}
	if err := encoder.Encode(event); err != nil {
		return 1
	}
	tunnel, err := r.start(ctx, cfg)
	if err != nil {
		_ = encoder.Encode(lifecycleEvent{State: "degraded", Origin: event.Origin, Error: mode + " start failed"})
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

// displayOrigin excludes URL userinfo, paths and queries that may hold secrets.
// A lifecycle event is diagnostic output, not the configuration sent to Start.
func displayOrigin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return (&url.URL{Scheme: u.Scheme, Host: u.Host}).String()
}
