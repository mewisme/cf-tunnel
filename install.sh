#!/bin/sh
set -eu

repo="mewisme/cf-tunnel"
binary="cf-tunnel"
install_dir="${CF_TUNNEL_INSTALL_DIR:-$HOME/.local/bin}"
mode="install"

fail() {
  printf 'cf-tunnel installer: %s\n' "$*" >&2
  exit 1
}

case "${1:-}" in
  "") ;;
  --uninstall) mode="uninstall" ;;
  *) fail "unknown option: $1" ;;
esac

if [ "$mode" = "uninstall" ]; then
  target="$install_dir/$binary"
  if [ ! -e "$target" ]; then
    printf 'cf-tunnel is not installed at %s\n' "$target"
    exit 0
  fi
  rm -f "$target"
  printf 'Uninstalled cf-tunnel from %s\n' "$target"
  exit 0
fi

command -v curl >/dev/null 2>&1 || fail "curl is required"
command -v tar >/dev/null 2>&1 || fail "tar is required"
command -v install >/dev/null 2>&1 || fail "install is required"

case "$(uname -s)" in
  Linux) os="linux" ;;
  Darwin) os="darwin" ;;
  *) fail "unsupported operating system: $(uname -s)" ;;
esac

case "$(uname -m)" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) fail "unsupported architecture: $(uname -m)" ;;
esac

latest_url="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest")" || \
  fail "could not resolve the latest release"
tag="${latest_url##*/}"
if [ -z "$tag" ] || [ "$tag" = "latest" ]; then
  fail "could not determine the latest release tag"
fi
version="${tag#v}"

asset="${binary}_${version}_${os}_${arch}.tar.gz"
base_url="https://github.com/$repo/releases/download/$tag"
tmp_dir="$(mktemp -d 2>/dev/null || mktemp -d -t cf-tunnel)"
trap 'rm -rf "$tmp_dir"' 0 HUP INT TERM

printf 'Installing cf-tunnel %s (%s/%s)\n' "$version" "$os" "$arch"
curl -fL --retry 3 --retry-delay 1 -o "$tmp_dir/$asset" "$base_url/$asset" || \
  fail "failed to download $asset"
curl -fL --retry 3 --retry-delay 1 -o "$tmp_dir/checksums.txt" "$base_url/checksums.txt" || \
  fail "failed to download checksums.txt"

expected="$(awk -v file="$asset" '$2 == file { print $1; exit }' "$tmp_dir/checksums.txt")"
[ -n "$expected" ] || fail "checksum for $asset was not found"

if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmp_dir/$asset" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  actual="$(shasum -a 256 "$tmp_dir/$asset" | awk '{print $1}')"
else
  fail "sha256sum or shasum is required to verify the release"
fi

[ "$actual" = "$expected" ] || fail "checksum verification failed for $asset"

tar -xzf "$tmp_dir/$asset" -C "$tmp_dir" "$binary" || fail "failed to extract $asset"
mkdir -p "$install_dir"
install -m 0755 "$tmp_dir/$binary" "$install_dir/$binary"

printf 'Installed %s to %s\n' "$binary" "$install_dir/$binary"
case ":${PATH:-}:" in
  *":$install_dir:"*) ;;
  *)
    printf 'Add %s to PATH to run %s from any shell.\n' "$install_dir" "$binary"
    ;;
esac