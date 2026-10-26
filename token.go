package cftunnel

import (
	"context"
	"fmt"
	"time"

	"go.mewis.me/cf-tunnel/internal/cloudflared"
)

// TokenInfo is public metadata extracted from a Cloudflare tunnel connector
// token. It never contains the tunnel secret or the original token string.
type TokenInfo struct {
	AccountID string `json:"account_id"`
	TunnelID  string `json:"tunnel_id"`
	Endpoint  string `json:"endpoint,omitempty"`
}

// ParseToken validates the connector token's structure without a network
// connection. A successfully parsed token may still be revoked or incorrect.
func ParseToken(token string) (TokenInfo, error) {
	info, err := cloudflared.ParseToken(token)
	if err != nil {
		return TokenInfo{}, err
	}
	return TokenInfo{AccountID: info.AccountID, TunnelID: info.TunnelID, Endpoint: info.Endpoint}, nil
}

// VerificationResult describes an Edge registration attempt. Verified is true
// only after Cloudflare accepts the connector registration, not after an
// offline token parse. Network failures and timeouts are reported as errors,
// NOT as proof the credentials are invalid.
type VerificationResult struct {
	TokenInfo
	Verified     bool   `json:"verified"`
	EdgeLocation string `json:"edge_location,omitempty"`
}

type verificationTunnel interface {
	WaitUntilReady(context.Context) error
	Wait() error
	EdgeLocation() string
}

type verificationStart func(context.Context, string) (verificationTunnel, error)

// VerifyToken temporarily registers one connector against Cloudflare Edge.
// It does not call the Cloudflare management REST API. A successful result
// proves the connector secret was accepted at registration time, not that the
// Dashboard ingress configuration or origins are reachable.
//
// A caller deadline is honored; without one, verification times out in 30s.
// The temporary connector is shut down before returning.
func VerifyToken(ctx context.Context, token string) (*VerificationResult, error) {
	return verifyToken(ctx, token, func(ctx context.Context, token string) (verificationTunnel, error) {
		return Start(ctx, Config{Token: token})
	})
}

func verifyToken(ctx context.Context, token string, start verificationStart) (*VerificationResult, error) {
	info, err := ParseToken(token)
	if err != nil {
		return nil, err
	}
	result := &VerificationResult{TokenInfo: info}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	tunnel, err := start(sessionCtx, token)
	if err != nil {
		cancel()
		return result, fmt.Errorf("token verification connection failed: %w", err)
	}
	if err := tunnel.WaitUntilReady(sessionCtx); err != nil {
		cancel()
		_ = tunnel.Wait()
		return result, fmt.Errorf("token verification failed: %w", err)
	}
	if err := sessionCtx.Err(); err != nil {
		cancel()
		_ = tunnel.Wait()
		return result, fmt.Errorf("token verification interrupted: %w", err)
	}
	result.Verified = true
	result.EdgeLocation = tunnel.EdgeLocation()
	cancel()
	_ = tunnel.Wait()
	return result, nil
}
