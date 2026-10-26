package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	cftunnel "go.mewis.me/cf-tunnel"
)

func testConnectorToken(t *testing.T) string {
	t.Helper()
	data, err := json.Marshal(map[string]string{
		"a": "test-account",
		"t": "7af68d8a-91bf-4835-a369-3f15cd36ae91",
		"s": base64.StdEncoding.EncodeToString([]byte("test-secret-not-visible")),
	})
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(data)
}

func TestTokenInspectCommandOffline(t *testing.T) {
	token := testConnectorToken(t)
	t.Setenv("CF_TUNNEL_TOKEN", token)
	var out, stderr bytes.Buffer
	if code := run([]string{"token", "inspect"}, &out, &stderr); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	var info cftunnel.TokenInfo
	if err := json.Unmarshal(out.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.AccountID != "test-account" || info.TunnelID != "7af68d8a-91bf-4835-a369-3f15cd36ae91" {
		t.Fatalf("info=%+v", info)
	}
	if strings.Contains(out.String(), token) || strings.Contains(out.String(), "test-secret-not-visible") {
		t.Fatal("inspect leaked connector secret")
	}
}

func TestTokenVerifyCommandConnectsAndPrintsVerified(t *testing.T) {
	token := testConnectorToken(t)
	var out, stderr bytes.Buffer
	called := false
	code := runTokenCommand([]string{"verify", "--token", token, "--timeout", "1s"}, &out, &stderr, func(_ context.Context, raw string) (*cftunnel.VerificationResult, error) {
		called = true
		if raw != token {
			t.Fatal("wrong token forwarded")
		}
		return &cftunnel.VerificationResult{
			TokenInfo:    cftunnel.TokenInfo{AccountID: "test-account", TunnelID: "7af68d8a-91bf-4835-a369-3f15cd36ae91"},
			Verified:     true,
			EdgeLocation: "SIN",
		}, nil
	})
	if code != 0 || !called {
		t.Fatalf("code=%d called=%t stderr=%q", code, called, stderr.String())
	}
	var got cftunnel.VerificationResult
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Verified || got.EdgeLocation != "SIN" || strings.Contains(out.String(), token) || strings.Contains(out.String(), "test-secret-not-visible") {
		t.Fatalf("output=%q", out.String())
	}
}

func TestTokenVerifyUnavailableIsNotVerifiedAndDoesNotLeak(t *testing.T) {
	token := testConnectorToken(t)
	var out, stderr bytes.Buffer
	code := runTokenCommand([]string{"verify", "--token", token}, &out, &stderr, func(context.Context, string) (*cftunnel.VerificationResult, error) {
		return nil, errors.New("secret-must-not-appear")
	})
	if code != 1 || !strings.Contains(out.String(), `"verified":false`) || !strings.Contains(out.String(), `"error":"verification_failed"`) {
		t.Fatalf("code=%d output=%q", code, out.String())
	}
	if strings.Contains(out.String()+stderr.String(), token) || strings.Contains(out.String()+stderr.String(), "secret-must-not-appear") {
		t.Fatal("token verification error leaked a secret")
	}
}

func TestTokenVerifyRejectsInvalidFlagsAndInput(t *testing.T) {
	token := testConnectorToken(t)
	never := func(context.Context, string) (*cftunnel.VerificationResult, error) {
		t.Fatal("verify callback should not run")
		return nil, nil
	}
	for _, args := range [][]string{
		{"inspect", "--token", "private-invalid-token"},
		{"verify", "--token", "private-invalid-token"},
	} {
		var out, stderr bytes.Buffer
		if code := runTokenCommand(args, &out, &stderr, never); code != 1 || !strings.Contains(out.String(), "invalid_token") {
			t.Fatalf("args=%v code=%d output=%q", args[:1], code, out.String())
		}
		if strings.Contains(out.String()+stderr.String(), "private-invalid-token") {
			t.Fatal("invalid token leaked")
		}
	}
	var out, stderr bytes.Buffer
	if code := runTokenCommand([]string{"verify", "--token", token, "--timeout", "0s"}, &out, &stderr, never); code != 2 {
		t.Fatalf("invalid timeout code=%d", code)
	}
}

type fakeTunnel struct {
	url      string
	id       string
	readyErr error
	done     chan error
}

func newFakeTunnel(url string) *fakeTunnel {
	return &fakeTunnel{url: url, done: make(chan error, 1)}
}

func (t *fakeTunnel) PublicURL() string { return t.url }

func (t *fakeTunnel) TunnelID() string { return t.id }

func (t *fakeTunnel) WaitUntilReady(context.Context) error { return t.readyErr }

func (t *fakeTunnel) Wait() error { return <-t.done }

func (t *fakeTunnel) complete(err error) { t.done <- err }

type synchronizedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *synchronizedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func (b *synchronizedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.b.Bytes()...)
}

func TestRunnerEmitsReadyAndStoppedForSingleOrigin(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var out synchronizedBuffer
	tunnel := newFakeTunnel("https://example.trycloudflare.com")
	r := runner{out: &out, start: func(context.Context, cftunnel.Config) (runtimeTunnel, error) { return tunnel, nil }}
	done := make(chan int, 1)
	go func() { done <- r.run(ctx, "http://127.0.0.1:37421") }()
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(out.String(), `"state":"ready"`) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	tunnel.complete(context.Canceled)
	if code := <-done; code != 0 {
		t.Fatalf("code=%d output=%s", code, out.String())
	}
	events := decodeEvents(t, out.Bytes())
	if len(events) != 3 || events[0].State != "connecting" || events[1].State != "ready" || events[2].State != "stopped" {
		t.Fatalf("events=%#v", events)
	}
	if events[1].Origin != "http://127.0.0.1:37421" || events[1].URL != "https://example.trycloudflare.com" {
		t.Fatalf("ready=%#v", events[1])
	}
}

func TestRunnerManagedTunnelReceivesTokenWithoutExposingIt(t *testing.T) {
	const token = "sensitive-connector-token"
	const id = "128edfa3-4561-4d2e-ae88-8caef9b3551a"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out synchronizedBuffer
	tunnel := newFakeTunnel("")
	tunnel.id = id
	r := runner{out: &out, start: func(_ context.Context, cfg cftunnel.Config) (runtimeTunnel, error) {
		if cfg.Token != token || cfg.OriginURL != "" {
			t.Errorf("unexpected tunnel config: token present=%t origin=%q", cfg.Token != "", cfg.OriginURL)
		}
		return tunnel, nil
	}}
	done := make(chan int, 1)
	go func() { done <- r.runConfig(ctx, cftunnel.Config{Token: token}) }()
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(out.String(), `"state":"ready"`) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	tunnel.complete(context.Canceled)
	if code := <-done; code != 0 {
		t.Fatalf("code=%d output=%s", code, out.String())
	}
	if strings.Contains(out.String(), token) {
		t.Fatal("token leaked to lifecycle output")
	}
	events := decodeEvents(t, out.Bytes())
	if len(events) != 3 || events[1].State != "ready" || events[1].TunnelID != id || events[1].Origin != "" || events[1].URL != "" {
		t.Fatalf("unexpected lifecycle: %#v", events)
	}
}

func TestRunRejectsConflictingFlagsWithoutLeakingToken(t *testing.T) {
	const token = "private-cli-token"
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--origin", "http://127.0.0.1:8080", "--token", token}, &stdout, &stderr); code != 2 {
		t.Fatalf("code=%d", code)
	}
	if strings.Contains(stdout.String()+stderr.String(), token) {
		t.Fatal("CLI token was leaked")
	}
}

func TestRunRejectsInvalidTokenWithoutLeakingIt(t *testing.T) {
	const token = "private-invalid-token"
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--token", token}, &stdout, &stderr); code != 1 {
		t.Fatalf("code=%d output=%s", code, stdout.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), token) {
		t.Fatal("CLI token was leaked")
	}
}

func TestRunnerDoesNotEchoStartError(t *testing.T) {
	const secret = "account-tag-and-tunnel-secret-must-not-leak"
	var out synchronizedBuffer
	r := runner{out: &out, start: func(context.Context, cftunnel.Config) (runtimeTunnel, error) { return nil, errors.New(secret) }}
	if code := r.run(context.Background(), "http://127.0.0.1:37421"); code != 1 {
		t.Fatalf("code=%d", code)
	}
	if strings.Contains(out.String(), secret) || !strings.Contains(out.String(), "quick tunnel start failed") {
		t.Fatalf("output=%q", out.String())
	}
}

func TestRunnerRedactsSensitiveOriginComponents(t *testing.T) {
	const secret = "private-password"
	const querySecret = "private-api-key"
	const pathSecret = "private-path"
	origin := "http://user:" + secret + "@127.0.0.1:37421/" + pathSecret + "?api_key=" + querySecret
	var out synchronizedBuffer
	r := runner{out: &out, start: func(_ context.Context, cfg cftunnel.Config) (runtimeTunnel, error) {
		if cfg.OriginURL != origin {
			t.Fatalf("actual origin was unexpectedly modified: %q", cfg.OriginURL)
		}
		return nil, errors.New("start failed")
	}}
	if code := r.run(context.Background(), origin); code != 1 {
		t.Fatalf("code=%d", code)
	}
	output := out.String()
	for _, value := range []string{secret, querySecret, pathSecret, "user:"} {
		if strings.Contains(output, value) {
			t.Fatalf("sensitive origin component %q leaked in %q", value, output)
		}
	}
	events := decodeEvents(t, out.Bytes())
	if len(events) != 2 || events[0].Origin != "http://127.0.0.1:37421" || events[1].Origin != events[0].Origin {
		t.Fatalf("unexpected sanitized lifecycle: %#v", events)
	}
}

func TestRunnerDoesNotEchoReadinessError(t *testing.T) {
	const secret = "edge-diagnostic-must-not-leak"
	var out synchronizedBuffer
	tunnel := newFakeTunnel("https://example.trycloudflare.com")
	tunnel.readyErr = errors.New(secret)
	r := runner{out: &out, start: func(context.Context, cftunnel.Config) (runtimeTunnel, error) { return tunnel, nil }}
	if code := r.run(context.Background(), "http://127.0.0.1:37421"); code != 1 {
		t.Fatalf("code=%d", code)
	}
	if strings.Contains(out.String(), secret) || !strings.Contains(out.String(), "quick tunnel connection failed") {
		t.Fatalf("output=%q", out.String())
	}
}

func TestRunnerDoesNotEchoUnexpectedWaitError(t *testing.T) {
	const secret = "runtime-secret-must-not-leak"
	var out synchronizedBuffer
	tunnel := newFakeTunnel("https://example.trycloudflare.com")
	tunnel.complete(errors.New(secret))
	r := runner{out: &out, start: func(context.Context, cftunnel.Config) (runtimeTunnel, error) { return tunnel, nil }}
	if code := r.run(context.Background(), "http://127.0.0.1:37421"); code != 1 {
		t.Fatalf("code=%d", code)
	}
	if strings.Contains(out.String(), secret) || !strings.Contains(out.String(), "connection stopped unexpectedly") {
		t.Fatalf("output=%q", out.String())
	}
}

func decodeEvents(t *testing.T, raw []byte) []lifecycleEvent {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var events []lifecycleEvent
	for decoder.More() {
		var event lifecycleEvent
		if err := decoder.Decode(&event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	return events
}
