package cloudflared

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	cfconfig "go.mewis.me/cf-tunnel/internal/cloudflared/config"
	"go.mewis.me/cf-tunnel/internal/cloudflared/ingress"
	"go.mewis.me/cf-tunnel/internal/cloudflared/orchestration"
)

func TestStubIsReadyAndPreservesTerminalError(t *testing.T) {
	tunnel := Stub("https://example.trycloudflare.com")
	if err := tunnel.WaitUntilReady(context.Background()); err != nil {
		t.Fatalf("WaitUntilReady: %v", err)
	}
	want := errors.New("terminal")
	tunnel.Complete(want)
	if err := tunnel.Wait(); !errors.Is(err, want) {
		t.Fatalf("Wait error = %v, want %v", err, want)
	}
}

func TestWaitUntilConfiguredSeparatesRemoteConfigFromEdgeReadiness(t *testing.T) {
	quick := Stub("https://quick.trycloudflare.com")
	if _, _, ok := quick.Routes(); ok {
		t.Fatal("quick tunnel has no Dashboard routes")
	}
	if err := quick.WaitUntilConfigured(context.Background()); err == nil {
		t.Fatal("quick tunnel should reject remote config wait")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := zerolog.New(io.Discard)
	initial := ingress.Ingress{}
	warp := ingress.NewWarpRoutingConfig(&cfconfig.WarpRoutingConfig{})
	dialer := ingress.NewOriginDialer(ingress.OriginConfig{}, &log)
	orchestrator, err := orchestration.NewOrchestrator(ctx, &orchestration.Config{
		Ingress: &initial, WarpRouting: warp, OriginDialerService: dialer,
	}, nil, nil, &log)
	if err != nil {
		t.Fatal(err)
	}
	managed := &Tunnel{managed: true, orchestrator: orchestrator, done: make(chan struct{})}
	if _, _, ok := managed.Routes(); ok {
		t.Fatal("remote routes visible before first update")
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if err := managed.WaitUntilConfigured(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled wait: %v", err)
	}
	if response := orchestrator.UpdateConfig(0, []byte(`{"ingress":[{"hostname":"one.example.com","service":"http://localhost:5555"},{"service":"http_status:404"}]}`)); response.Err != nil {
		t.Fatal(response.Err)
	}
	if err := managed.WaitUntilConfigured(ctx); err != nil {
		t.Fatalf("ready wait: %v", err)
	}
	if version, routes, ok := managed.Routes(); !ok || version != 0 || len(routes) != 2 || routes[0].Service != "http://localhost:5555" {
		t.Fatalf("version=%d routes=%+v ok=%t", version, routes, ok)
	}
}

func TestStartRequiresOrigin(t *testing.T) {
	_, err := Start(context.Background(), Config{})
	if err == nil || !strings.Contains(err.Error(), "origin URL is required") {
		t.Fatalf("error: %v", err)
	}
}

func TestStartRejectsNonHTTPOrigin(t *testing.T) {
	_, err := Start(context.Background(), Config{OriginURL: "tcp://127.0.0.1:1"})
	if err == nil || !strings.Contains(err.Error(), "http or https") {
		t.Fatalf("error: %v", err)
	}
}

func TestStartRejectsOriginURLCredentials(t *testing.T) {
	const secret = "password-that-must-not-leak"
	_, err := Start(context.Background(), Config{OriginURL: "http://user:" + secret + "@127.0.0.1:8080"})
	if err == nil || !strings.Contains(err.Error(), "must not contain credentials") {
		t.Fatalf("expected credential rejection: %v", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("origin credentials leaked in validation error")
	}
}

func TestNewIngressHandler(t *testing.T) {
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusAccepted)
	})
	ing, err := newIngress(Config{Handler: handler})
	if err != nil {
		t.Fatal(err)
	}
	if len(ing.Rules) != 1 || ing.Rules[0].Service.String() != "in-memory" {
		t.Fatalf("unexpected ingress: %#v", ing)
	}
	local, ok := ing.Rules[0].Service.(http.Handler)
	if !ok {
		t.Fatal("in-memory origin does not serve HTTP")
	}
	w := httptest.NewRecorder()
	local.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "https://example.trycloudflare.com/mcp", nil))
	if !called || w.Code != http.StatusAccepted {
		t.Fatalf("handler was not called: called=%t status=%d", called, w.Code)
	}
}

func TestNewIngressURL(t *testing.T) {
	ing, err := newIngress(Config{OriginURL: "http://127.0.0.1:8080"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ing.Rules) != 1 || ing.Rules[0].Service.String() != "http://127.0.0.1:8080" {
		t.Fatalf("unexpected ingress: %#v", ing)
	}
}

func TestNewIngressManagedMode(t *testing.T) {
	ing, err := newIngress(Config{Token: "dummy"})
	if err != nil {
		t.Fatal(err)
	}
	if !ing.IsEmpty() {
		t.Fatalf("expected no local ingress rules: %#v", ing)
	}
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	ing, err = newIngress(Config{Token: "dummy", Handler: handler})
	if err != nil || !ing.IsEmpty() {
		t.Fatalf("managed handler must use Dashboard ingress: %+v %v", ing, err)
	}
	if managedHandler(Config{Token: "dummy", Handler: handler}) == nil || managedHandler(Config{Handler: handler}) != nil {
		t.Fatal("managed handler must only be attached to token tunnel")
	}
	for _, tc := range []Config{
		{Token: "dummy", OriginURL: "http://127.0.0.1:8080"},
		{Token: "dummy", QuickService: "https://api.trycloudflare.com"},
		{Token: "dummy", Handler: handler, QuickService: "https://api.trycloudflare.com"},
	} {
		if _, err := newIngress(tc); err == nil {
			t.Fatalf("expected conflict: %#v", tc)
		}
	}
}

func TestManagedDashboardIngressConfiguration(t *testing.T) {
	var remote ingress.RemoteConfig
	err := json.Unmarshal([]byte(`{
		"ingress": [
			{"hostname":"mcp.example.com","service":"http://127.0.0.1:5678"},
			{"service":"http_status:404"}
		]
	}`), &remote)
	if err != nil {
		t.Fatal(err)
	}
	if len(remote.Ingress.Rules) != 2 {
		t.Fatalf("unexpected rule count: %d", len(remote.Ingress.Rules))
	}
	rule, _ := remote.Ingress.FindMatchingRule("mcp.example.com", "/mcp")
	if rule.Service.String() != "http://127.0.0.1:5678" {
		t.Fatalf("wrong dashboard origin: %s", rule.Service)
	}
	rule, _ = remote.Ingress.FindMatchingRule("other.example.com", "/")
	if rule.Service.String() != "http_status:404" {
		t.Fatalf("wrong dashboard fallback: %s", rule.Service)
	}
}

func TestStartProvisionNon2xx(t *testing.T) {
	secret := "start-secret-must-not-leak"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tunnel" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("User-Agent") != userAgent {
			t.Errorf("user-agent: %s", r.Header.Get("User-Agent"))
		}
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, secret)
	}))
	defer srv.Close()

	_, err := Start(context.Background(), Config{
		OriginURL:    "http://127.0.0.1:1",
		QuickService: srv.URL,
	})
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("error: %v", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("leaked secret: %v", err)
	}
}

func TestStartProvisionMalformed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"success":true,`)
	}))
	defer srv.Close()

	_, err := Start(context.Background(), Config{
		OriginURL:    "http://127.0.0.1:1",
		QuickService: srv.URL,
	})
	if err == nil || err.Error() != "failed to unmarshal quick Tunnel" {
		t.Fatalf("error: %v", err)
	}
}

func TestStartRejectsOversizedProvisioningResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, strings.Repeat("x", 1<<20+1))
	}))
	defer srv.Close()
	_, err := Start(context.Background(), Config{
		OriginURL:    "http://127.0.0.1:1",
		QuickService: srv.URL,
	})
	if err == nil || !strings.Contains(err.Error(), "response too large") {
		t.Fatalf("error: %v", err)
	}
}

func TestStartProvisionCancelled(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()

	_, err := Start(ctx, Config{
		OriginURL:    "http://127.0.0.1:1",
		QuickService: srv.URL,
	})
	if err == nil || !strings.Contains(err.Error(), "failed to request quick Tunnel") {
		t.Fatalf("error: %v", err)
	}
}

func TestStartDoesNotLogSecretOnSuccessParseFail(t *testing.T) {
	body, err := json.Marshal(QuickTunnelResponse{
		Success: true,
		Result: QuickTunnel{
			ID:         "not-a-uuid",
			Hostname:   "x.trycloudflare.com",
			AccountTag: testAccountTag,
			Secret:     []byte(testSecret),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	_, err = Start(context.Background(), Config{
		OriginURL:    "http://127.0.0.1:1",
		QuickService: srv.URL,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	assertNoSecrets(t, err.Error(), body)
}
