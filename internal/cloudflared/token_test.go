package cloudflared

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestParseTunnelToken(t *testing.T) {
	id := uuid.New()
	data, err := json.Marshal(map[string]any{
		"a": "account-tag",
		"s": base64.StdEncoding.EncodeToString([]byte("valid-tunnel-secret")),
		"t": id.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := base64.StdEncoding.EncodeToString(data)
	creds, err := parseTunnelToken(" \n" + raw + "\t")
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccountTag != "account-tag" || creds.TunnelID != id || string(creds.TunnelSecret) != "valid-tunnel-secret" {
		t.Fatalf("unexpected decoded credentials: account=%q id=%s secret_len=%d", creds.AccountTag, creds.TunnelID, len(creds.TunnelSecret))
	}
}

func TestParseTunnelTokenRejectsInvalidInputs(t *testing.T) {
	cases := []string{
		"secret-not-base64!",
		base64.StdEncoding.EncodeToString([]byte("not json")),
		base64.StdEncoding.EncodeToString([]byte("{}")),
		base64.StdEncoding.EncodeToString([]byte(`{"a":"tag","s":"AQID","t":"not-a-uuid"}`)),
		base64.StdEncoding.EncodeToString([]byte(`{"a":"tag","s":"AQID","t":"00000000-0000-0000-0000-000000000000"}`)),
	}
	for _, raw := range cases {
		_, err := parseTunnelToken(raw)
		if err == nil {
			t.Fatal("expected invalid token error")
		}
		if strings.Contains(err.Error(), raw) {
			t.Fatal("error contains raw token")
		}
	}
}
