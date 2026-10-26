package cftunnel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fixtureToken(t *testing.T) string {
	t.Helper()
	body, err := json.Marshal(map[string]string{
		"a": "account-fixture",
		"t": "2bfc84a2-8393-46b7-b3f5-56f7c778a311",
		"s": base64.StdEncoding.EncodeToString([]byte("secret-never-export")),
		"e": "custom-edge.example",
	})
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(body)
}

func TestParseTokenOnlyReturnsPublicMetadata(t *testing.T) {
	token := fixtureToken(t)
	info, err := ParseToken(" \n" + token + "\t")
	if err != nil {
		t.Fatal(err)
	}
	if info.AccountID != "account-fixture" || info.TunnelID != "2bfc84a2-8393-46b7-b3f5-56f7c778a311" || info.Endpoint != "custom-edge.example" {
		t.Fatalf("metadata=%+v", info)
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret-never-export") || strings.Contains(string(data), token) || strings.Contains(string(data), "AQID") {
		t.Fatal("token secret leaked to token metadata")
	}
}

func TestParseTokenRejectsMalformedWithoutLeaking(t *testing.T) {
	token := "very-secret-invalid-token"
	_, err := ParseToken(token)
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("error=%v", err)
	}
	_, err = ParseToken(strings.Repeat("x", 16*1024+1))
	if err == nil {
		t.Fatal("expected rejection of oversized token")
	}
}

type fakeVerificationTunnel struct {
	ready    func(context.Context) error
	location string
	canceled *atomic.Bool
}

func (t *fakeVerificationTunnel) WaitUntilReady(ctx context.Context) error {
	return t.ready(ctx)
}
func (t *fakeVerificationTunnel) Wait() error          { return nil }
func (t *fakeVerificationTunnel) EdgeLocation() string { return t.location }

func TestVerifyTokenSuccessAndCleanup(t *testing.T) {
	token := fixtureToken(t)
	closed := make(chan struct{})
	got, err := verifyToken(context.Background(), token, func(ctx context.Context, arg string) (verificationTunnel, error) {
		if arg != token {
			t.Fatal("token was changed")
		}
		go func() { <-ctx.Done(); close(closed) }()
		return &fakeVerificationTunnel{location: "SIN", ready: func(context.Context) error { return nil }}, nil
	})
	if err != nil || got == nil || !got.Verified || got.EdgeLocation != "SIN" || got.AccountID != "account-fixture" {
		t.Fatalf("result=%+v err=%v", got, err)
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("verification did not cancel temporary connector")
	}
}

func TestVerifyTokenFailureIsNotAnInvalidVerdict(t *testing.T) {
	token := fixtureToken(t)
	closed := make(chan struct{})
	got, err := verifyToken(context.Background(), token, func(ctx context.Context, arg string) (verificationTunnel, error) {
		go func() { <-ctx.Done(); close(closed) }()
		return &fakeVerificationTunnel{ready: func(context.Context) error { return errors.New("network not reachable") }}, nil
	})
	if err == nil || got == nil || got.Verified {
		t.Fatalf("result=%+v err=%v", got, err)
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("failed verification left a connector context open")
	}
}

func TestVerifyTokenHonorsCancellation(t *testing.T) {
	token := fixtureToken(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	got, err := verifyToken(ctx, token, func(ctx context.Context, _ string) (verificationTunnel, error) {
		called = true
		return &fakeVerificationTunnel{ready: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}}, nil
	})
	if got == nil || got.Verified || !errors.Is(err, context.Canceled) || called {
		t.Fatalf("result=%+v err=%v started=%t", got, err, called)
	}
}

func TestVerifyTokenDoesNotAcceptReadyAfterDeadline(t *testing.T) {
	token := fixtureToken(t)
	ctx, cancel := context.WithCancel(context.Background())
	got, err := verifyToken(ctx, token, func(context.Context, string) (verificationTunnel, error) {
		return &fakeVerificationTunnel{ready: func(context.Context) error {
			cancel()
			return nil
		}}, nil
	})
	if got == nil || got.Verified || !errors.Is(err, context.Canceled) {
		t.Fatalf("result=%+v err=%v", got, err)
	}
}

func TestVerifyTokenDoesNotAttemptNetworkForMalformedToken(t *testing.T) {
	called := false
	got, err := verifyToken(context.Background(), "invalid-token", func(context.Context, string) (verificationTunnel, error) {
		called = true
		return nil, nil
	})
	if err == nil || called || got != nil {
		t.Fatalf("result=%+v err=%v called=%t", got, err, called)
	}
}
