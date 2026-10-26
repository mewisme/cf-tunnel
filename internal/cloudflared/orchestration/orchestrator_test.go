package orchestration

import (
	"context"
	"io"
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
