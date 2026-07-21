#!/usr/bin/env bash
set -euo pipefail

version="${HUGO_VERSION:-0.164.0}"
hugo_bin="${HUGO_BIN:-}"

if [[ -z "$hugo_bin" ]]; then
  if command -v hugo >/dev/null 2>&1; then
    hugo_bin="$(command -v hugo)"
  else
    os="$(uname -s | tr '[:upper:]' '[:lower:]')"
    arch="$(uname -m)"
    case "$arch" in
      x86_64|amd64) arch="amd64" ;;
      arm64|aarch64) arch="arm64" ;;
      *) echo "unsupported architecture for Hugo: $arch" >&2; exit 1 ;;
    esac
    case "$os" in
      linux|darwin) ;;
      *) echo "unsupported OS for Hugo: $os" >&2; exit 1 ;;
    esac

    tmp="$(mktemp -d)"
    trap 'rm -rf "$tmp"' EXIT INT TERM
    url="https://github.com/gohugoio/hugo/releases/download/v${version}/hugo_extended_${version}_${os}-${arch}.tar.gz"
    curl -fsSL "$url" -o "$tmp/hugo.tar.gz"
    tar -xzf "$tmp/hugo.tar.gz" -C "$tmp" hugo
    hugo_bin="$tmp/hugo"
  fi
fi

"$hugo_bin" --source site --destination "$PWD/public" --gc --minify --noBuildLock
