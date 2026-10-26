package cloudflared

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"go.mewis.me/cf-tunnel/internal/cloudflared/connection"
)

// parseTunnelToken decodes Cloudflare's base64-encoded connector credentials.
// Avoid exposing the original token or its decoded contents in errors.
func parseTunnelToken(raw string) (connection.Credentials, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) > 16*1024 {
		return connection.Credentials{}, fmt.Errorf("invalid tunnel token encoding")
	}
	content, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return connection.Credentials{}, fmt.Errorf("invalid tunnel token encoding")
	}
	var token connection.TunnelToken
	if err := json.Unmarshal(content, &token); err != nil {
		return connection.Credentials{}, fmt.Errorf("invalid tunnel token format")
	}
	if token.AccountTag == "" || token.TunnelID == uuid.Nil || len(token.TunnelSecret) == 0 {
		return connection.Credentials{}, fmt.Errorf("incomplete tunnel token credentials")
	}
	return token.Credentials(), nil
}

// TokenInfo exposes safe metadata, never the embedded tunnel secret.
type TokenInfo struct {
	AccountID string `json:"account_id"`
	TunnelID  string `json:"tunnel_id"`
	Endpoint  string `json:"endpoint,omitempty"`
}

// ParseToken checks the token structure locally. It does not prove Cloudflare
// currently accepts these credentials.
func ParseToken(raw string) (TokenInfo, error) {
	credentials, err := parseTunnelToken(raw)
	if err != nil {
		return TokenInfo{}, err
	}
	return TokenInfo{
		AccountID: credentials.AccountTag,
		TunnelID:  credentials.TunnelID.String(),
		Endpoint:  credentials.Endpoint,
	}, nil
}
