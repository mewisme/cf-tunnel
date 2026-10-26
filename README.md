# cf-tunnel

Cloudflare Edge connector identity: `cf-tunnel.v<version>` (for example,
`cf-tunnel.v0.0.1`). Release builds use the GoReleaser version; binaries built
with `go install` and applications embedding the library derive it from Go
module metadata. Unversioned development builds report `cf-tunnel.vdev`.
This is the connector's own version, not the base cloudflared source version.

Minimal Cloudflare Quick Tunnel runtime for exposing one local HTTP(S) origin
or an in-process `http.Handler`.
It can be used either as a standalone binary or as an importable Go module.
Both surfaces use the same tunnel implementation and intentionally keep the
runtime ephemeral: no account login, tunnel registry, updater, service manager,
or persistent provider authority is included.

## Binary

Install the latest release on Linux or macOS:

```bash
curl -fsSL https://get.mewis.me/cf-tunnel.sh | sh
```

Install the latest release on Windows PowerShell:

```powershell
irm https://get.mewis.me/cf-tunnel.ps1 | iex
```

The installers verify the downloaded release against `checksums.txt`. By
default, the shell installer writes to `~/.local/bin` and the PowerShell
installer writes to `%LOCALAPPDATA%\Programs\cf-tunnel\bin`. Override either
destination with `CF_TUNNEL_INSTALL_DIR`.

Uninstall on Linux or macOS:

```bash
curl -fsSL https://get.mewis.me/cf-tunnel.sh | sh -s -- --uninstall
```

Uninstall on Windows PowerShell:

```powershell
& ([scriptblock]::Create((irm https://get.mewis.me/cf-tunnel.ps1))) --uninstall
```

The uninstall mode removes the `cf-tunnel` binary from the selected install
directory. On Windows, the installer also removes its PATH entry when that
entry was originally added by the installer and removes the install directory
when it is empty.

Alternatively, install with Go:

```bash
go install go.mewis.me/cf-tunnel/cmd/cf-tunnel@latest
```

Run one tunnel:

```bash
cf-tunnel --origin http://127.0.0.1:37421
```

To connect an **existing Dashboard-managed tunnel**, copy its connector token
from Cloudflare Zero Trust. Prefer entering it without exposing it in shell
history or process arguments (Bash):

```bash
read -rsp 'Cloudflare tunnel token: ' CF_TUNNEL_TOKEN
echo
export CF_TUNNEL_TOKEN
cf-tunnel
unset CF_TUNNEL_TOKEN
```

For automation, inject `CF_TUNNEL_TOKEN` from the process manager or a secret
store. The `--token` flag is also supported, but a literal token argument can
be exposed via shell history and process listings:

```bash
cf-tunnel --token '<tunnel-token>'
```

Environment variables can also be inspected by sufficiently privileged local
processes; restrict access to the connector's runtime environment.

### Inspect or verify a Dashboard connector token

Use the same `CF_TUNNEL_TOKEN` environment variable with the token commands:

```bash
cf-tunnel token inspect
cf-tunnel token verify --timeout 30s
```

`token inspect` is **offline**: it validates the connector token format and
prints safe metadata only (account ID, tunnel ID, optional endpoint):

```json
{"account_id":"example-account","tunnel_id":"2bfc84a2-8393-46b7-b3f5-56f7c778a311"}
```

`token verify` temporarily registers one connector with Cloudflare Edge,
closes it, and returns one JSON object. A successful registration includes
`"verified":true` and the edge location when available:

```json
{"account_id":"example-account","tunnel_id":"2bfc84a2-8393-46b7-b3f5-56f7c778a311","verified":true,"edge_location":"SIN"}
```

An invalid token structure or failed verification produces JSON with a
machine-readable `error` and exit status 1. An unreachable Edge or a
timeout **does not establish that a token is revoked**; it means verification
could not be completed. Successful verification does not prove Dashboard routes
or local origin services are reachable. These commands use no Cloudflare
management API token and do not expose the connector secret.

Configure public hostnames and their local service URLs (for example,
`http://127.0.0.1:8080`) on the Cloudflare Dashboard. The connector
automatically receives the remote ingress rules and subsequent updates. You
do not need to install a system service, create a local ingress config file, or
specify a local port on the `cf-tunnel` command line. The origin server
configured on the dashboard must still be running and reachable.

Use **either** `--token` or `--origin`. The minimal runtime keeps one QUIC
edge connection. Token mode supports remotely managed HTTP(S)/WebSocket
origins; it does not add Cloudflare account login, service installation, or
private-network routing management. Remote ingress rules requiring
`originRequest.access.required` (origin-side Cloudflare Access JWT verification)
are **not supported** and fail closed with HTTP 403; Edge-enforced Access
policies are a separate mechanism. Do not configure origin-side JWT validation
for this minimal connector.

The binary writes newline-delimited JSON lifecycle events to stdout. The stable
fields are `state`, `origin`, `url`, `tunnel_id`, and `error`. Provisioning credentials,
account tags, tunnel secrets, and raw provider response bodies are never
emitted.
The `origin` field includes only the URL scheme and host, not embedded
credentials, URL paths or queries. Origin URLs containing userinfo are rejected.

Example lifecycle:

```json
{"state":"connecting","origin":"http://127.0.0.1:37421"}
{"state":"ready","origin":"http://127.0.0.1:37421","url":"https://example.trycloudflare.com"}
{"state":"stopped","origin":"http://127.0.0.1:37421"}
```

Cancel the process with `SIGINT` or `SIGTERM` for a clean shutdown.
In token mode the `ready` event contains `tunnel_id`, not `url`: managed
public hostnames belong to Dashboard configuration, not the connector.

## Go module

```bash
go get go.mewis.me/cf-tunnel
```

```go
package main

import (
	"context"
	"log"

	cftunnel "go.mewis.me/cf-tunnel"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tunnel, err := cftunnel.Start(ctx, cftunnel.Config{
		OriginURL: "http://127.0.0.1:37421",
	})
	if err != nil {
		log.Fatal(err)
	}

	if err := tunnel.WaitUntilReady(ctx); err != nil {
		log.Fatal(err)
	}
	log.Printf("public URL: %s", tunnel.URL)
	if err := tunnel.Wait(); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}
```

One `Start` call owns one ephemeral tunnel. Use `WaitUntilReady` when the caller
needs confirmation that the first Cloudflare edge connection is established,
and cancel the context to stop it. `Config.QuickService` is optional and defaults to
`https://api.trycloudflare.com`.

### Dashboard-managed tunnel (Go SDK)

Use the same API with `Config.Token`. The library does not provision an
anonymous Quick Tunnel or require `OriginURL` in this mode:

```go
tunnel, err := cftunnel.Start(ctx, cftunnel.Config{
	Token: os.Getenv("CF_TUNNEL_TOKEN"),
})
if err != nil {
	return err
}
if err := tunnel.WaitUntilReady(ctx); err != nil {
	return err
}
log.Printf("connected managed tunnel %s", tunnel.TunnelID())
return tunnel.Wait()
```

Import `os` for the environment variable. The caller owns the context and
shutdown. The registered hostname(s), local origin URLs, and their updates are
managed by Cloudflare. `PublicURL()` is empty in managed mode; retrieve
the URL(s) from your Dashboard. Do not combine `Token` with `OriginURL`,
`Handler`, or `QuickService`.

You can inspect a token without connecting or verify it with a short-lived
Cloudflare Edge registration:

```go
token := os.Getenv("CF_TUNNEL_TOKEN")
info, err := cftunnel.ParseToken(token)
if err != nil {
    return err
}
log.Printf("tunnel=%s account=%s", info.TunnelID, info.AccountID)

ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
defer cancel()
verification, err := cftunnel.VerifyToken(ctx, token)
if err != nil {
    return err // offline parse, network, authorization, or timeout failure
}
log.Printf("verified=%t edge=%s", verification.Verified, verification.EdgeLocation)
```

Import `time` to use a deadline. `VerifyToken` defaults to a 30-second
timeout when the caller supplies no deadline; it closes its temporary connector
before returning and does not require a separately running tunnel.

### In-memory HTTP origin

Embed the tunnel in a Go service without binding a localhost TCP port:

```go
handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
})

tunnel, err := cftunnel.Start(ctx, cftunnel.Config{Handler: handler})
if err != nil {
	return err
}
if err := tunnel.WaitUntilReady(ctx); err != nil {
	return err
}
log.Printf("public URL: %s", tunnel.PublicURL())
```

Import `net/http` to define the handler. Use either `Config.Handler` or
`Config.OriginURL`, never both. The handler runs in the caller's process;
HTTP headers, request contexts, and streaming/SSE responses pass through the
existing ingress HTTP handling path. The binary keeps its existing `--origin`
behavior.

In-memory means **no origin listener**. The tunnel still makes outbound QUIC
connections to Cloudflare, and its public URL remains accessible from the
internet. Secure sensitive endpoints such as MCP servers with suitable
authentication; in-memory routing does not add access control.

## CodeMCP compatibility

The public contract is deliberately compatible with CodeMCP's current
single-tunnel architecture:

- one invocation / `Start` call exposes one origin;
- the public URL is ephemeral and is never a durable identity;
- the caller owns lifecycle, restart policy, and persistence decisions;
- no multi-instance registry or competing persistent tunnel authority is
  introduced by this module;
- errors and the binary lifecycle stream do not expose Quick Tunnel
  credentials;
- the transport is Cloudflare Quick Tunnel over QUIC with one edge connection;
- MCP Streamable HTTP JSON works through the origin normally; this project does
  not add an SSE workaround.

The standalone binary is suitable when CodeMCP should keep the Cloudflared/QUIC
dependency tree out of its primary executable. The Go module is available when
an embedding application explicitly prefers in-process ownership.

## Development

```bash
make check
make build
```

Release tags matching `v*` are built by GoReleaser for Linux, macOS, and
Windows on amd64 and arm64. Release archives contain both the project MIT
license and the retained Cloudflare Apache-2.0 license/provenance files. Linux
amd64/arm64 and Windows amd64 release binaries are compressed with UPX
(`--best --lzma`). macOS and Windows arm64 binaries are left uncompressed;
current UPX does not support PE/ARM64 packing.

## Upstream and licensing

This project contains a deliberately reduced and modified subset of
Cloudflare's `cloudflared` implementation, based on upstream tag `2026.9.1`
with selected fixes backported from `2026.10.3`. See
`third_party/cloudflared/UPSTREAM.md` for provenance, transport constraints,
and the backport list.

Project-specific code is MIT licensed and Copyright (c) 2026 Mew. The retained
and adapted Cloudflare implementation remains Apache-2.0 licensed and
attributed to Cloudflare and its contributors. See `LICENSE`, `LICENSING.md`,
`NOTICE`, and `third_party/cloudflared/`.