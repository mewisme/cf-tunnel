package orchestration

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rs/zerolog"

	"go.mewis.me/cf-tunnel/internal/cloudflared/config"
	"go.mewis.me/cf-tunnel/internal/cloudflared/ingress"
	"go.mewis.me/cf-tunnel/internal/cloudflared/tracing"
)

type managedRecorder struct{ *httptest.ResponseRecorder }

func (w *managedRecorder) WriteRespHeaders(status int, headers http.Header) error {
	for k, vv := range headers {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(status)
	return nil
}
func (w *managedRecorder) AddTrailer(k, v string) { w.Header().Add(k, v) }
func (*managedRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, http.ErrNotSupported
}

func newManagedTestOrchestrator(t *testing.T, handler http.Handler) *Orchestrator {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	log := zerolog.New(io.Discard)
	ing := ingress.Ingress{}
	dialer := ingress.NewOriginDialer(ingress.OriginConfig{}, &log)
	o, err := NewOrchestrator(ctx, &Config{
		Ingress:             &ing,
		WarpRouting:         ingress.NewWarpRoutingConfig(&config.WarpRoutingConfig{}),
		OriginDialerService: dialer,
		ManagedHandler:      handler,
	}, nil, nil, &log)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func requestManaged(t *testing.T, o *Orchestrator, host, path string) *managedRecorder {
	t.Helper()
	p, err := o.GetOriginProxy()
	if err != nil {
		t.Fatal(err)
	}
	log := zerolog.New(io.Discard)
	req := httptest.NewRequest(http.MethodPost, "https://"+host+path, strings.NewReader("hello"))
	rec := &managedRecorder{httptest.NewRecorder()}
	if err := p.ProxyHTTP(rec, tracing.NewTracedHTTPRequest(req, 0, &log), false); err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestManagedHandlerRemoteRoutingAndUpdates(t *testing.T) {
	var count atomic.Int32
	o := newManagedTestOrchestrator(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		w.Header().Set("X-Route", r.Host)
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, r.URL.Path)
	}))
	// Missing dashboard config must not accidentally expose all hosts.
	if rec := requestManaged(t, o, "mcp.example.com", "/mcp"); rec.Code != http.StatusServiceUnavailable || count.Load() != 0 {
		t.Fatalf("unconfigured status=%d requests=%d", rec.Code, count.Load())
	}
	for i, svc := range []string{"http://127.0.0.1:49152", "https://no-server.invalid:443", "ws://localhost:443", "wss://localhost:443"} {
		data := []byte(fmt.Sprintf(`{"ingress":[{"hostname":"mcp.example.com","path":"^/mcp","service":%q},{"service":"http_status:404"}]}`, svc))
		if resp := o.UpdateConfig(int32(i+1), data); resp.Err != nil || resp.LastAppliedVersion != int32(i+1) {
			t.Fatalf("remote update %d: %+v", i, resp)
		}
		rec := requestManaged(t, o, "mcp.example.com", "/mcp")
		if rec.Code != http.StatusAccepted || rec.Body.String() != "/mcp" || rec.Header().Get("X-Route") != "mcp.example.com" {
			t.Fatalf("forwarded status=%d body=%q headers=%v", rec.Code, rec.Body.String(), rec.Header())
		}
		for _, route := range [][2]string{{"other.example.com", "/mcp"}, {"mcp.example.com", "/other"}} {
			if got := requestManaged(t, o, route[0], route[1]).Code; got != http.StatusNotFound {
				t.Fatalf("fallback for %v returned %d", route, got)
			}
		}
		if got := count.Load(); got != int32(i+1) {
			t.Fatalf("handler invoked for fallback or never called: %d", got)
		}
		// Remote HTTP origins are replaced, not listened to or dialed.
		if got := o.config.Ingress.Rules[0].Service.String(); got != "in-memory" {
			t.Fatalf("unexpected origin %q", got)
		}
	}
	if rec := requestManaged(t, o, "mcp.example.com", "/mcp"); rec.Code != http.StatusAccepted {
		t.Fatalf("route lost after update: %d", rec.Code)
	}
}

func TestManagedHandlerUnsafeRemoteUpdatePreservesLastGoodConfig(t *testing.T) {
	o := newManagedTestOrchestrator(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	first := []byte(`{"ingress":[{"service":"https://localhost:4000"}]}`)
	if resp := o.UpdateConfig(1, first); resp.Err != nil {
		t.Fatal(resp.Err)
	}
	for i, svc := range []string{"ssh://localhost:22", "unix:/tmp/other-origin.sock", "hello_world", "bastion"} {
		raw := []byte(fmt.Sprintf(`{"ingress":[{"service":%q}]}`, svc))
		if resp := o.UpdateConfig(int32(i+2), raw); resp.Err == nil || resp.LastAppliedVersion != 1 {
			t.Fatalf("service %q accepted remote update: %+v", svc, resp)
		}
		if rec := requestManaged(t, o, "mcp.example.com", "/mcp"); rec.Code != http.StatusCreated {
			t.Fatalf("rejected update destroyed existing route, status=%d", rec.Code)
		}
	}
	if p, err := o.GetOriginProxy(); err != nil {
		t.Fatal(err)
	} else if err := p.ProxyTCP(context.Background(), nil, nil); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("unexpected private TCP proxy result %v", err)
	}
}

func TestManagedHandlerNeverBypassesOriginAccess(t *testing.T) {
	var calls atomic.Int32
	o := newManagedTestOrchestrator(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	remote := []byte(`{"ingress":[{"service":"http://127.0.0.1:80","originRequest":{"access":{"required":true}}}]}`)
	if resp := o.UpdateConfig(1, remote); resp.Err != nil {
		t.Fatal(resp.Err)
	}
	if rec := requestManaged(t, o, "mcp.example.com", "/mcp"); rec.Code != http.StatusForbidden || calls.Load() != 0 {
		t.Fatalf("origin-side Access bypass: status=%d handler_calls=%d", rec.Code, calls.Load())
	}
}

func TestManagedHandlerIsIsolatedAcrossConcurrentInstances(t *testing.T) {
	var first, second atomic.Int64
	newHandler := func(label string, counter *atomic.Int64) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			counter.Add(1)
			w.Header().Set("X-Instance", label)
			w.WriteHeader(http.StatusOK)
		})
	}
	a := newManagedTestOrchestrator(t, newHandler("a", &first))
	b := newManagedTestOrchestrator(t, newHandler("b", &second))
	conf := []byte(`{"ingress":[{"hostname":"mcp.example.com","service":"http://127.0.0.1:1"},{"service":"http_status:404"}]}`)
	for _, o := range []*Orchestrator{a, b} {
		if resp := o.UpdateConfig(1, conf); resp.Err != nil {
			t.Fatal(resp.Err)
		}
	}
	const workers, iterations = 8, 25
	var wg sync.WaitGroup
	errs := make(chan error, workers*iterations)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			o, want := a, "a"
			if i%2 == 1 {
				o, want = b, "b"
			}
			for j := 0; j < iterations; j++ {
				rec := requestManaged(t, o, "mcp.example.com", "/mcp")
				if rec.Code != http.StatusOK || rec.Header().Get("X-Instance") != want {
					errs <- fmt.Errorf("instance %s served wrong origin: status=%d header=%q", want, rec.Code, rec.Header().Get("X-Instance"))
				}
				if j%5 == 0 {
					_ = o.UpdateConfig(int32(j+2), conf)
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if first.Load() != workers*iterations/2 || second.Load() != workers*iterations/2 {
		t.Fatalf("cross-instance handler calls: %d %d", first.Load(), second.Load())
	}
}

func TestManagedHandlerPreservesContextAndSSE(t *testing.T) {
	type requestKey struct{}
	ctx := context.WithValue(context.Background(), requestKey{}, "value")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Context().Value(requestKey{}) != "value" {
			t.Errorf("lost request context")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: hello\n\n")
		w.(http.Flusher).Flush()
	})
	o := newManagedTestOrchestrator(t, handler)
	if res := o.UpdateConfig(1, []byte(`{"ingress":[{"service":"http://127.0.0.1:7777"}]}`)); res.Err != nil {
		t.Fatal(res.Err)
	}
	p, err := o.GetOriginProxy()
	if err != nil {
		t.Fatal(err)
	}
	log := zerolog.New(io.Discard)
	req := httptest.NewRequest(http.MethodGet, "https://mcp.example.com/events", nil).WithContext(ctx)
	rec := &managedRecorder{httptest.NewRecorder()}
	if err := p.ProxyHTTP(rec, tracing.NewTracedHTTPRequest(req, 0, &log), false); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !rec.Flushed || rec.Code != 200 || rec.Body.String() != "data: hello\n\n" {
		t.Fatalf("SSE status=%d flush=%t body=%q", rec.Code, rec.Flushed, rec.Body.String())
	}
}
