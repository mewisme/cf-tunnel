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
