# cf-tunnel

Minimal Cloudflare Quick Tunnel runtime for exposing one local HTTP(S) origin.
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
go install github.com/mewisme/cf-tunnel/cmd/cf-tunnel@latest
```

Run one tunnel:

```bash
cf-tunnel --origin http://127.0.0.1:37421
```

The binary writes newline-delimited JSON lifecycle events to stdout. The stable
fields are `state`, `origin`, `url`, and `error`. Provisioning credentials,
account tags, tunnel secrets, and raw provider response bodies are never
emitted.

Example lifecycle:

```json
{"state":"connecting","origin":"http://127.0.0.1:37421"}
{"state":"ready","origin":"http://127.0.0.1:37421","url":"https://example.trycloudflare.com"}
{"state":"stopped","origin":"http://127.0.0.1:37421"}
```

Cancel the process with `SIGINT` or `SIGTERM` for a clean shutdown.

## Go module

```bash
go get github.com/mewisme/cf-tunnel
```

```go
package main

import (
	"context"
	"log"

	cftunnel "github.com/mewisme/cf-tunnel"
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
Cloudflare's `cloudflared` Quick Tunnel implementation, pinned to upstream tag
`2026.9.1`. See `third_party/cloudflared/UPSTREAM.md` for the exact commit,
transport constraints, and extraction notes.

Project-specific code is MIT licensed and Copyright (c) 2026 Mew. The retained
and adapted Cloudflare implementation remains Apache-2.0 licensed and
attributed to Cloudflare and its contributors. See `LICENSE`, `LICENSING.md`,
`NOTICE`, and `third_party/cloudflared/`.