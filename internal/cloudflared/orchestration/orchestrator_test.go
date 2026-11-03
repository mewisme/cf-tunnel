package orchestration

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"go.mewis.me/cf-tunnel/internal/cloudflared/config"
	"go.mewis.me/cf-tunnel/internal/cloudflared/ingress"
)

func TestDashboardRemotelyUpdatesIngress(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := zerolog.New(io.Discard)
	initial := ingress.Ingress{}
	warp := ingress.NewWarpRoutingConfig(&config.WarpRoutingConfig{})
	dialer := ingress.NewOriginDialer(ingress.OriginConfig{}, &log)
	o, err := NewOrchestrator(ctx, &Config{
		Ingress:             &initial,
		WarpRouting:         warp,
		OriginDialerService: dialer,
	}, nil, nil, &log)
	if err != nil {
		t.Fatal(err)
	}
	first, err := o.GetConfigJSON()
	if err != nil || !strings.Contains(string(first), "http_status:503") {
		t.Fatalf("expected fail-closed ingress before config: %s %v", first, err)
	}
	for version, port := range []string{"8080", "9090"} {
		data := []byte(`{"ingress":[{"hostname":"mcp.example.com","service":"http://127.0.0.1:` + port + `"},{"service":"http_status:404"}]}`)
		resp := o.UpdateConfig(int32(version+1), data)
		if resp.Err != nil || resp.LastAppliedVersion != int32(version+1) {
			t.Fatalf("config update %d: %+v", version+1, resp)
		}
		current, err := o.GetConfigJSON()
		if err != nil || !strings.Contains(string(current), "http://127.0.0.1:"+port) {
			t.Fatalf("dashboard routing after update: %s %v", current, err)
		}
	}
	if resp := o.UpdateConfig(1, []byte("bad data")); resp.Err != nil || resp.LastAppliedVersion != 2 {
		t.Fatalf("stale dashboard update should be ignored: %+v", resp)
	}
}

func TestDashboardRoutesSnapshotPreservesOriginalService(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := zerolog.New(io.Discard)
	initial := ingress.Ingress{}
	warp := ingress.NewWarpRoutingConfig(&config.WarpRoutingConfig{})
	dialer := ingress.NewOriginDialer(ingress.OriginConfig{}, &log)
	o, err := NewOrchestrator(ctx, &Config{
		Ingress: &initial, WarpRouting: warp, OriginDialerService: dialer,
		ManagedHandler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	}, nil, nil, &log)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := o.Routes(); ok {
		t.Fatal("pre-config default route must not appear as remote configuration")
	}
	select {
	case <-o.RoutesReady():
		t.Fatal("ready before remote config")
	default:
	}

	first := []byte(`{"ingress":[{"hostname":"mcp.example.com","path":"^/mcp","service":"http://user:private@127.0.0.1:5555?key=hidden"},{"service":"http_status:404"}]}`)
	if response := o.UpdateConfig(3, first); response.Err != nil {
		t.Fatalf("update failed: %v", response.Err)
	}
	select {
	case <-o.RoutesReady():
	default:
		t.Fatal("config notification missing")
	}
	version, routes, ok := o.Routes()
	if !ok || version != 3 || len(routes) != 2 || routes[0].Hostname != "mcp.example.com" || routes[0].Path != "^/mcp" || routes[0].Service != "http://127.0.0.1:5555" || routes[1].Service != "http_status:404" {
		t.Fatalf("incorrect redacted routes v=%d routes=%+v ok=%t", version, routes, ok)
	}
	routes[0].Service = "modified"
	if _, unchanged, _ := o.Routes(); unchanged[0].Service != "http://127.0.0.1:5555" {
		t.Fatal("mutating routes snapshot changed stored config")
	}
	if response := o.UpdateConfig(4, []byte(`{"ingress":[{"service":"tcp://127.0.0.1:2222"}]}`)); response.Err == nil {
		t.Fatal("incompatible handler configuration should fail")
	}
	if v, retained, _ := o.Routes(); v != 3 || retained[0].Service != "http://127.0.0.1:5555" {
		t.Fatal("rejected remote config replaced applied snapshot")
	}
	if response := o.UpdateConfig(2, []byte("invalid")); response.Err != nil {
		t.Fatal("stale config should be ignored")
	}
	if response := o.UpdateConfig(5, []byte(`{"ingress":[{"hostname":"new.example.com","service":"https://localhost:8443"},{"service":"http_status:404"}]}`)); response.Err != nil {
		t.Fatalf("update failed: %v", response.Err)
	}
	if v, updated, _ := o.Routes(); v != 5 || updated[0].Hostname != "new.example.com" || updated[0].Service != "https://localhost:8443" {
		t.Fatalf("updated routes v=%d routes=%+v", v, updated)
	}
}

func TestSafeRouteServiceFailsClosed(t *testing.T) {
	for input, want := range map[string]string{
		"http://user:pass@[::1]:5555/private?token=secret#fragment": "http://[::1]:5555",
		"ssh://127.0.0.1:22?key=secret":                             "ssh://127.0.0.1:22",
		"unix:/tmp/private.sock":                                    "unix:<redacted>",
		"unix+tls:/tmp/private.sock":                                "unix+tls:<redacted>",
		"http_status:404":                                           "http_status:404",
		"hello_world":                                               "hello_world",
		"opaque-secret-token":                                       "<redacted>",
		"http_status:404?password=private":                          "<redacted>",
		"http://%broken-url":                                        "<redacted>",
	} {
		if got := safeRouteService(input); got != want {
			t.Errorf("safeRouteService(%q)=%q want %q", input, got, want)
		}
	}
}
