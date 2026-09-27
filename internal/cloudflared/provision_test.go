package cloudflared

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

const (
	testSecret     = "quick-tunnel-secret-value"
	testAccountTag = "account-tag-must-not-leak"
	testHostname   = "random.trycloudflare.com"
	testTunnelID   = "11111111-1111-1111-1111-111111111111"
)

func TestParseProvisionSuccess(t *testing.T) {
	body, err := json.Marshal(QuickTunnelResponse{
		Success: true,
		Result: QuickTunnel{
			ID:         testTunnelID,
			Hostname:   testHostname,
			AccountTag: testAccountTag,
			Secret:     []byte(testSecret),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := parseProvisionResponse(200, body)
	if err != nil {
		t.Fatal(err)
	}
	if got.hostname != testHostname {
		t.Fatalf("hostname: got %q", got.hostname)
	}
	if got.url != "https://"+testHostname {
		t.Fatalf("url: got %q", got.url)
	}
	if got.credentials.AccountTag != testAccountTag {
		t.Fatalf("account tag mismatch")
	}
	if string(got.credentials.TunnelSecret) != testSecret {
		t.Fatalf("secret mismatch")
	}
	if got.credentials.TunnelID != uuid.MustParse(testTunnelID) {
		t.Fatalf("tunnel id mismatch")
	}
}

func TestParseProvisionPreservesHTTPSPrefix(t *testing.T) {
	body, err := json.Marshal(QuickTunnelResponse{
		Success: true,
		Result: QuickTunnel{
			ID:         testTunnelID,
			Hostname:   "https://" + testHostname,
			AccountTag: testAccountTag,
			Secret:     []byte(testSecret),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseProvisionResponse(200, body)
	if err != nil {
		t.Fatal(err)
	}
	if got.url != "https://"+testHostname {
		t.Fatalf("url: got %q", got.url)
	}
}

func TestParseProvisionNon2xxUsesErrorMessages(t *testing.T) {
	body, err := json.Marshal(QuickTunnelResponse{
		Success: false,
		Errors:  []QuickTunnelError{{Code: 1016, Message: "rate limited"}},
		Result: QuickTunnel{
			AccountTag: testAccountTag,
			Secret:     []byte(testSecret),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = parseProvisionResponse(429, body)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("error: %v", err)
	}
	assertNoSecrets(t, err.Error(), body)
}

func TestParseProvisionNon2xxWithoutJSONOmitsBody(t *testing.T) {
	body := []byte(`internal error containing ` + testSecret + ` and ` + testAccountTag)
	_, err := parseProvisionResponse(500, body)
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "quick tunnel provisioning failed with status 500" {
		t.Fatalf("error: %v", err)
	}
	assertNoSecrets(t, err.Error(), body)
}

func TestParseProvisionMalformedFailsClosed(t *testing.T) {
	_, err := parseProvisionResponse(200, []byte(`{"success": true,`))
	if err == nil || err.Error() != "failed to unmarshal quick Tunnel" {
		t.Fatalf("error: %v", err)
	}
}

func TestParseProvisionSuccessFalse(t *testing.T) {
	body, err := json.Marshal(QuickTunnelResponse{Success: false})
	if err != nil {
		t.Fatal(err)
	}
	_, err = parseProvisionResponse(200, body)
	if err == nil || err.Error() != "quick tunnel provisioning failed" {
		t.Fatalf("error: %v", err)
	}
}

func TestParseProvisionErrorsArray(t *testing.T) {
	body, err := json.Marshal(QuickTunnelResponse{
		Success: true,
		Errors: []QuickTunnelError{
			{Code: 1, Message: "bad request"},
			{Code: 2, Message: "try later"},
		},
		Result: QuickTunnel{Secret: []byte(testSecret), AccountTag: testAccountTag},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = parseProvisionResponse(200, body)
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "quick tunnel provisioning failed: [1] bad request; [2] try later" {
		t.Fatalf("error: %v", err)
	}
	assertNoSecrets(t, err.Error(), body)
}

func TestParseProvisionInvalidID(t *testing.T) {
	body, err := json.Marshal(QuickTunnelResponse{
		Success: true,
		Result: QuickTunnel{
			ID:         "not-a-uuid",
			Hostname:   testHostname,
			AccountTag: testAccountTag,
			Secret:     []byte(testSecret),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = parseProvisionResponse(200, body)
	if err == nil || err.Error() != "failed to parse quick Tunnel ID" {
		t.Fatalf("error: %v", err)
	}
	assertNoSecrets(t, err.Error(), body)
}

func TestParseProvisionMissingHostname(t *testing.T) {
	body, err := json.Marshal(QuickTunnelResponse{
		Success: true,
		Result: QuickTunnel{
			ID:         testTunnelID,
			AccountTag: testAccountTag,
			Secret:     []byte(testSecret),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = parseProvisionResponse(200, body)
	if err == nil || !strings.Contains(err.Error(), "missing hostname") {
		t.Fatalf("error: %v", err)
	}
	assertNoSecrets(t, err.Error(), body)
}

func TestParseProvisionMissingSecret(t *testing.T) {
	body, err := json.Marshal(QuickTunnelResponse{
		Success: true,
		Result: QuickTunnel{
			ID:         testTunnelID,
			Hostname:   testHostname,
			AccountTag: testAccountTag,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = parseProvisionResponse(200, body)
	if err == nil || !strings.Contains(err.Error(), "missing secret") {
		t.Fatalf("error: %v", err)
	}
	assertNoSecrets(t, err.Error(), body)
}

func assertNoSecrets(t *testing.T, errText string, body []byte) {
	t.Helper()
	for _, secret := range []string{testSecret, testAccountTag, string(body)} {
		if secret != "" && strings.Contains(errText, secret) {
			t.Fatalf("error leaked secret material %q: %s", secret, errText)
		}
	}
}
