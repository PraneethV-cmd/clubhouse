#!/usr/bin/env sh
set -eu

base="${CLUBHOUSE_BASE_URL:-http://localhost:1313}"
install_dir="${CLUBHOUSE_INSTALL_DIR:-$HOME/.local/bin}"

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "clubhouse installer: missing required command: $1" >&2
    exit 1
  }
}

need tar
need uname

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$os" in
  darwin|linux) ;;
  *) echo "clubhouse installer: unsupported OS: $os" >&2; exit 1 ;;
esac
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "clubhouse installer: unsupported arch: $arch" >&2; exit 1 ;;
esac

asset="clubhouse_${os}_${arch}.tar.gz"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

download() {
  from="$1"
  to="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$from" -o "$to"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$to" "$from"
  else
    echo "clubhouse installer: install curl or wget first" >&2
    exit 1
  fi
}

echo "Downloading clubhouse local simulation build for ${os}/${arch}..."
download "${base}/releases/${asset}" "$tmp/$asset"
download "${base}/releases/checksums.txt" "$tmp/checksums.txt"

if command -v shasum >/dev/null 2>&1; then
  (cd "$tmp" && grep "  ${asset}\$" checksums.txt | shasum -a 256 -c -)
elif command -v sha256sum >/dev/null 2>&1; then
  (cd "$tmp" && grep "  ${asset}\$" checksums.txt | sha256sum -c -)
else
  echo "clubhouse installer: warning: no checksum tool found; skipping verification" >&2
fi

mkdir -p "$install_dir"
tar -xzf "$tmp/$asset" -C "$tmp"
install "$tmp/clubhouse" "$install_dir/clubhouse"

echo "Installed clubhouse to $install_dir/clubhouse"
case ":$PATH:" in
  *":$install_dir:"*) ;;
  *) echo "Add $install_dir to PATH, then run: clubhouse host" ;;
esac
echo "Next: clubhouse host"
