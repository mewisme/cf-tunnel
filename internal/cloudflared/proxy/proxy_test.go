package proxy

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"go.mewis.me/cf-tunnel/internal/cloudflared/ingress"
	"go.mewis.me/cf-tunnel/internal/cloudflared/tracing"
)

type inMemoryResponseWriter struct {
	*httptest.ResponseRecorder
}

func (w *inMemoryResponseWriter) WriteRespHeaders(status int, headers http.Header) error {
	for k, values := range headers {
		for _, value := range values {
			w.Header().Add(k, value)
		}
	}
	w.WriteHeader(status)
	return nil
}

func (w *inMemoryResponseWriter) AddTrailer(name, value string) {
	w.Header().Add(name, value)
}

func (w *inMemoryResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, http.ErrNotSupported
}

func TestInMemoryOriginProxyHTTP(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "request-context")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Context().Value(contextKey{}) != "request-context" {
			t.Error("request context was not forwarded")
		}
		if r.Method != http.MethodPost || r.URL.Path != "/mcp" || r.Host != "demo.trycloudflare.com" {
			t.Errorf("unexpected request: %s %s %s", r.Method, r.URL.Path, r.Host)
		}
		if got := r.Header.Get("Mcp-Session-Id"); got != "session-1" {
			t.Errorf("session header = %q", got)
		}
		w.Header().Set("Mcp-Session-Id", "session-2")
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","result":{}}`)
	})
	ing := ingress.Ingress{Rules: []ingress.Rule{{Service: ingress.NewHandlerService(handler)}}}
	log := zerolog.New(io.Discard)
	p := NewOriginProxy(ing, nil, nil, nil, &log)
	req := httptest.NewRequest(http.MethodPost, "https://demo.trycloudflare.com/mcp", strings.NewReader("{}")).WithContext(ctx)
	req.Header.Set("Mcp-Session-Id", "session-1")
	rec := &inMemoryResponseWriter{httptest.NewRecorder()}
	if err := p.ProxyHTTP(rec, tracing.NewTracedHTTPRequest(req, 0, &log), false); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusAccepted || rec.Header().Get("Mcp-Session-Id") != "session-2" || rec.Body.String() != `{"jsonrpc":"2.0","result":{}}` {
		t.Fatalf("response = %d %#v %q", rec.Code, rec.Header(), rec.Body.String())
	}
}

func TestInMemoryOriginSSE(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "event: message\ndata: one\n\n")
		w.(http.Flusher).Flush()
		_, _ = io.WriteString(w, "event: message\ndata: two\n\n")
	})
	ing := ingress.Ingress{Rules: []ingress.Rule{{Service: ingress.NewHandlerService(handler)}}}
	log := zerolog.New(io.Discard)
	p := NewOriginProxy(ing, nil, nil, nil, &log)
	rec := &inMemoryResponseWriter{httptest.NewRecorder()}
	req := httptest.NewRequest(http.MethodGet, "https://demo.trycloudflare.com/mcp", nil)
	if err := p.ProxyHTTP(rec, tracing.NewTracedHTTPRequest(req, 0, &log), false); err != nil {
		t.Fatal(err)
	}
	if !rec.Flushed || rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream metadata: flushed=%t status=%d headers=%#v", rec.Flushed, rec.Code, rec.Header())
	}
	if got := rec.Body.String(); got != "event: message\ndata: one\n\nevent: message\ndata: two\n\n" {
		t.Fatalf("stream = %q", got)
	}
}

func TestInMemoryOriginRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	})
	ing := ingress.Ingress{Rules: []ingress.Rule{{Service: ingress.NewHandlerService(handler)}}}
	log := zerolog.New(io.Discard)
	p := NewOriginProxy(ing, nil, nil, nil, &log)
	req := httptest.NewRequest(http.MethodGet, "https://demo.trycloudflare.com/mcp", nil).WithContext(ctx)
	done := make(chan error, 1)
	go func() {
		done <- p.ProxyHTTP(&inMemoryResponseWriter{httptest.NewRecorder()}, tracing.NewTracedHTTPRequest(req, 0, &log), false)
	}()
	<-started
	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("ProxyHTTP cancellation: %v", err)
	}
}
