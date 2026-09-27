package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	cftunnel "github.com/mewisme/cf-tunnel"
)

type fakeTunnel struct {
	url      string
	readyErr error
	done     chan error
}

func newFakeTunnel(url string) *fakeTunnel {
	return &fakeTunnel{url: url, done: make(chan error, 1)}
}

func (t *fakeTunnel) PublicURL() string { return t.url }

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
