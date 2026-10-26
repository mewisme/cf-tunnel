package cftunnel_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	cftunnel "go.mewis.me/cf-tunnel"
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

func TestPublicModuleRejectsBothOriginModes(t *testing.T) {
	_, err := cftunnel.Start(context.Background(), cftunnel.Config{
		OriginURL: "http://127.0.0.1:8080",
		Handler:   http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	})
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("error = %v", err)
	}
}

func TestPublicModuleRejectsTokenWithOrigin(t *testing.T) {
	_, err := cftunnel.Start(context.Background(), cftunnel.Config{
		Token:     "secret-must-not-appear",
		OriginURL: "http://127.0.0.1:8080",
	})
	if err == nil || !strings.Contains(err.Error(), "cannot be combined") || strings.Contains(err.Error(), "secret-must-not-appear") {
		t.Fatalf("error = %v", err)
	}
}

func TestPublicModuleAllowsTokenWithHandlerBeforeTokenValidation(t *testing.T) {
	_, err := cftunnel.Start(context.Background(), cftunnel.Config{
		Token:   "secret-must-not-appear",
		Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	})
	if err == nil || !strings.Contains(err.Error(), "invalid tunnel token") || strings.Contains(err.Error(), "secret-must-not-appear") {
		t.Fatalf("error = %v", err)
	}
}

func TestPublicModuleRejectsInvalidTokenWithoutNetwork(t *testing.T) {
	_, err := cftunnel.Start(context.Background(), cftunnel.Config{Token: "private-invalid-token"})
	if err == nil || !strings.Contains(err.Error(), "invalid tunnel token") || strings.Contains(err.Error(), "private-invalid-token") {
		t.Fatalf("error = %v", err)
	}
}

func TestDefaultQuickServiceIsStable(t *testing.T) {
	if cftunnel.DefaultQuickService != "https://api.trycloudflare.com" {
		t.Fatalf("DefaultQuickService = %q", cftunnel.DefaultQuickService)
	}
}
