package cftunnel_test

import (
	"context"
	"strings"
	"testing"

	cftunnel "github.com/mewisme/cf-tunnel"
)

func TestPublicModuleRejectsMissingOrigin(t *testing.T) {
	_, err := cftunnel.Start(context.Background(), cftunnel.Config{})
	if err == nil || !strings.Contains(err.Error(), "origin URL is required") {
		t.Fatalf("error = %v", err)
	}
}

func TestPublicModuleRejectsNonHTTPOrigin(t *testing.T) {
	_, err := cftunnel.Start(context.Background(), cftunnel.Config{OriginURL: "tcp://127.0.0.1:1"})
	if err == nil || !strings.Contains(err.Error(), "http or https") {
		t.Fatalf("error = %v", err)
	}
}

func TestDefaultQuickServiceIsStable(t *testing.T) {
	if cftunnel.DefaultQuickService != "https://api.trycloudflare.com" {
		t.Fatalf("DefaultQuickService = %q", cftunnel.DefaultQuickService)
	}
}
