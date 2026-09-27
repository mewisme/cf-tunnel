# Licensing

This repository is a combined work with two license scopes.

## Project-specific code — MIT

Files authored specifically for `cf-tunnel`, including the public Go
package, standalone command, build/release configuration, tests, and project
documentation, are licensed under the MIT License in the repository root
`LICENSE` file and are Copyright (c) 2026 Mew.

## Cloudflare-derived implementation — Apache-2.0

`internal/cloudflared/**` is a reduced and modified derivative of Cloudflare's
`cloudflared` project and remains licensed under the Apache License, Version
2.0. The retained license, attribution, exact upstream pin, and extraction
notes are in `third_party/cloudflared/`.

When redistributing the combined binary or source, retain both the root MIT
license and the Cloudflare Apache-2.0 license/provenance files.