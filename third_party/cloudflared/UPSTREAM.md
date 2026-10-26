# Upstream pin

Quick Tunnel source is adapted from `github.com/cloudflare/cloudflared`.

| Field | Value |
| --- | --- |
| Tag | `2026.9.1` |
| Commit | `f11dea9cb7079e90a982c1a2d5548ab40847fdcf` |
| Annotated tag object | `be5825661dca56de70b5493ece66237f290427e3` |
| License | Apache License 2.0 (copied as `LICENSE`) |
| Official linux-amd64 checksum | `03f1f25d1cc93b9ad6c60569d44060bc4f17ed97075760ed8cfca4b12dcd68cc` |

## Selective follow-up (2026-10-26)

Reviewed [upstream 2026.10.3](https://github.com/cloudflare/cloudflared/compare/2026.9.1...2026.10.3)
without replacing the original 2026.9.1 extraction. The minimal project
incorporates these relevant compatibility/security fixes:

- QUIC request-body close cancels the receive side without closing the response
  stream (`connection/quic_connection.go`, `quic/safe_stream.go`; TUN-10808).
- Ingress route matching canonicalizes paths when Access rules are present to
  prevent path traversal from selecting a different route (VULN-141859).
- Quick Tunnel provisioning responses are capped at 1 MiB.
- TCP/SOCKS-over-WebSocket preserves the unread tail of oversized messages
  (`websocket/connection.go`; TUN-10994).
- Build minimum is Go 1.27.3 to inherit patched upstream Go security fixes.

The connector also supports **Dashboard-managed tunnel tokens** using the
existing supervisor, remote-config update RPC, and origin ingress parser.
Tokens are decoded in-memory. No `cloudflared tunnel login`, config-file
authority, OS service management, or persistent token storage was imported.
The local base remains 2026.9.1; these are isolated backports rather than a
wholesale rebase of Cloudflare sources. Protected Quick Tunnel OTP/login and
its accompanying dependencies were intentionally not imported.

The sidecar exposes the Quick Tunnel entry as a small library `Start(ctx, Config)`
that provisions via `POST {quick-service}/tunnel` (default
`https://api.trycloudflare.com`) and then runs the connection supervisor against
one HTTP origin. The implementation does not depend on `cmd/cloudflared`.

## Protocol constraint (do not regress)

Runtime MCP HTTP through official Quick Tunnel works as Streamable HTTP JSON
(`Content-Type: application/json`). Required initialize / `tools/list` /
`tools/call` / session continuity / auth do not need SSE. `cm serve` `/mcp/sse`
is 405 locally. Do not add protocol hacks to bypass Cloudflare's documented SSE
limit. Direct MCP HTTP and Admin authentication remain mandatory on any public
URL.

## Call path to keep

```text
POST https://api.trycloudflare.com/tunnel
  -> tunnel id, account tag, secret, hostname
  -> force protocol=quic, ha-connections=1
  -> connection.TunnelProperties{Credentials, QuickTunnelUrl}
  -> supervisor.Supervisor (single edge connection)
  -> origin HTTP proxy to Config.OriginURL
```

Upstream files that encode provisioning (must be rewritten out of urfave/cli):

- `cmd/cloudflared/tunnel/quick_tunnel.go` (`RunQuickTunnel`, response types)
- `connection` credentials / QuickTunnelUrl observer
- `supervisor` reconnect loop
- `edgediscovery` region DNS
- `orchestration` origin config
- `ingress` / `proxy` HTTP origin
- `quic` + `quic/v3` edge transport
- `tunnelrpc` + generated capnp
- `tunnelstate`, `retry`, `signal`, `stream`

## Keep vs reject (coarse)

Keep, then strip CLI/globals:

- `connection/`, `supervisor/`, `orchestration/`, `edgediscovery/`
- `quic/`, `quic/v3/`, `tunnelrpc/`, `tunnelstate/`
- `ingress/` origin HTTP only (drop ICMP, hello-world, JWT middleware if unused)
- `proxy/`, `retry/`, `signal/`, `stream/`, `tlsconfig/`, `ipaccess/`, `packet/`

Reject:

- entire `cmd/cloudflared` (urfave/cli, service install, updater, `tunnel login/run/create`, DNS routes)
- `updater/`, `token/` (Access login / browser), `management/`, `sshgen/`, `socks/`
- `watcher/`, `overwatch/`, `prechecks/` (optional; stock binary runs them)
- Sentry init, systemd notify, pidfile, tracing/otel exporters
- Cloudflare's vendored third-party tree (none should be copied)

## Third-party modules that may remain

Expected after strip:

- `github.com/quic-go/quic-go`
- `github.com/google/uuid`
- `github.com/pkg/errors` (replace with stdlib if cheap)
- `golang.org/x/crypto`, `golang.org/x/net`, `golang.org/x/sync`

Remove from the extracted path if the corresponding cloudflared code is dropped:

- `github.com/urfave/cli/v2`
- `github.com/getsentry/sentry-go`
- `github.com/coreos/go-oidc`, `github.com/go-jose/go-jose` (Access/JWT ingress)
- `go.opentelemetry.io/*`
- `github.com/go-chi/chi`, `github.com/shirou/gopsutil`
- Prometheus if metrics can be no-ops

## Size estimate (tag `2026.9.1`, tests excluded)

- Whole repo Go: ~44k LOC
- Coarse keep-candidate dirs above: ~21k LOC
- CLI `cmd/`: ~9k LOC (do not copy)
- Official `cloudflared-linux-amd64` binary: 38 MB; extracted library must be smaller and must not import `github.com/cloudflare/cloudflared/...`

## Keep packages (tag `2026.9.1`)

Rewrite imports into `go.mewis.me/cf-tunnel/internal/cloudflared/...`:

- `connection`, `connection/dialopts`
- `supervisor`
- `orchestration`
- `edgediscovery`, `edgediscovery/allregions`
- `quic`, `quic/v3`
- `tunnelrpc`, `tunnelrpc/metrics`, `tunnelrpc/pogs`, `tunnelrpc/proto`, `tunnelrpc/quic`
- `tunnelstate`, `retry`, `signal`, `stream`, `tlsconfig`, `ipaccess`, `packet`, `proxy`

Keep only HTTP origin pieces from `ingress`. Drop `ingress/middleware` (JWT/Access) and ICMP/hello-world origin types during copy. Do not copy `*_test.go` until the stripped package still compiles.

## Extracted (2026-09-16)

Copied from `/tmp/cloudflared-src` at commit `f11dea9cb7079e90a982c1a2d5548ab40847fdcf` (~23k LOC, tests excluded):

- keep packages above, plus compile deps: `config` (types only), `ingress` (+ `origins`), `carrier` (bastion dest helper only), `cfio`, `client`, `crypto`, `datagramsession`, `features`, `fips`, `flow`, `tracing`, `websocket`, `socks`
- rewritten `github.com/cloudflare/cloudflared` → `go.mewis.me/cf-tunnel/internal/cloudflared`
- no `cmd/cloudflared`, no `github.com/cloudflare/cloudflared/...` imports, no `urfave/cli`, no Sentry

Local stubs/patches:

- `flags`: `MaxActiveFlows` constant only
- `management`: log event constants + `ManagementService` HTTP stub (no websocket/chi management server)
- `hello`: no-op listener (hello-world origin is not a product path)
- `ingress/middleware/jwtvalidator.go`: Access JWT always forbidden; no oidc
- `config/configuration.go`: structs/`CustomDuration` only (no config-file discovery)
- stripped CLI ingress parsers and Sentry calls from `supervisor`/`stream`

`RunQuickTunnel` is rewritten as an internal `Start(ctx, Config)` plus
`parseProvisionResponse`, with a narrow public wrapper at the repository root.
Protocol is forced to QUIC with `HAConnections=1`. Datagram metrics use a
per-supervisor Prometheus registry so embedding the library does not rely on a
process-global registry.

quic-go replace (same as upstream): `github.com/chungthuang/quic-go v0.45.1-0.20260529212404-a9fddf436fc4`

## Fetch / diff the next upstream release

Do not bump the pin automatically. Review security and protocol changes first.

```bash
git clone --filter=blob:none https://github.com/cloudflare/cloudflared.git /tmp/cloudflared-src
git -C /tmp/cloudflared-src fetch --tags origin
git -C /tmp/cloudflared-src checkout <new-tag>
git -C /tmp/cloudflared-src log --oneline f11dea9cb7079e90a982c1a2d5548ab40847fdcf..<new-tag> -- \
  connection supervisor orchestration edgediscovery quic tunnelrpc ingress proxy retry
```

Diff keep-set packages against `internal/cloudflared/` after rewriting
`github.com/cloudflare/cloudflared` → `go.mewis.me/cf-tunnel/internal/cloudflared`.
Re-apply every local stub/patch listed above. Re-run MCP/Admin Quick Tunnel
smoke against the official binary for that tag before extracting.

The upstream source tag stays in this file as provenance; the connector
reports `cf-tunnel.v<version>` to Cloudflare Edge. The version comes from
release build metadata when available, otherwise from the Go module build
information. This intentionally differs from the original cloudflared tag.

## Release checklist (before bumping the pin)

- [ ] Official linux-amd64 checksum recorded for the new tag
- [ ] Apache-2.0 LICENSE unchanged or reviewed
- [ ] Quick Tunnel provision path (`POST /tunnel`) still returns id/hostname/secret
- [ ] MCP Streamable HTTP JSON still works; do not add SSE hacks
- [ ] quic-go replace still matches upstream when required
- [ ] Local stubs/patches re-applied and the root package remains the only public tunnel API
- [ ] No `cmd/cloudflared` and no `github.com/cloudflare/cloudflared/...` imports
- [ ] Provision secret/account tag never appear in errors or logs
