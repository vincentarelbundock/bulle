#!/bin/sh
set -eu

OWNER="vincentarelbundock"
REPO="bulle"
BIN="bulle"
INSTALL_DIR="/usr/local/bin"
VERSION="latest"

usage() {
  cat <<EOF
Usage: install.sh [options]

Options:
  -b, --bin-dir DIR   Install directory (default: /usr/local/bin)
  -v, --version VER   Version to install, e.g. v0.1.0 (default: latest)
  -h, --help          Show this help
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    -b|--bin-dir)
      INSTALL_DIR="$2"
      shift 2
      ;;
    -v|--version)
      VERSION="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "required command not found: $1" >&2
    exit 1
  fi
}

need curl
need tar
need grep
need cut

OS=$(uname -s)
ARCH=$(uname -m)

case "$OS" in
  Darwin) OS="Darwin" ;;
  Linux) OS="Linux" ;;
  *)
    echo "unsupported OS: $OS" >&2
    exit 1
    ;;
esac

case "$ARCH" in
  x86_64|amd64) ARCH="x86_64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *)
    echo "unsupported architecture: $ARCH" >&2
    exit 1
    ;;
esac

if [ "$VERSION" = "latest" ]; then
  BASE_URL="https://github.com/$OWNER/$REPO/releases/latest/download"
else
  BASE_URL="https://github.com/$OWNER/$REPO/releases/download/$VERSION"
fi

ASSET="${BIN}_${OS}_${ARCH}.tar.gz"
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT INT TERM

ARCHIVE="$TMPDIR/$ASSET"
CHECKSUMS="$TMPDIR/checksums.txt"

curl -fsSL "$BASE_URL/$ASSET" -o "$ARCHIVE"
curl -fsSL "$BASE_URL/checksums.txt" -o "$CHECKSUMS"

EXPECTED=$(grep "  $ASSET\$" "$CHECKSUMS" | cut -d ' ' -f 1)
if [ -z "$EXPECTED" ]; then
  echo "could not find checksum for $ASSET" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL=$(sha256sum "$ARCHIVE" | cut -d ' ' -f 1)
elif command -v shasum >/dev/null 2>&1; then
  ACTUAL=$(shasum -a 256 "$ARCHIVE" | cut -d ' ' -f 1)
else
  echo "required command not found: sha256sum or shasum" >&2
  exit 1
fi

if [ "$ACTUAL" != "$EXPECTED" ]; then
  echo "checksum mismatch for $ASSET" >&2
  echo "expected: $EXPECTED" >&2
  echo "actual:   $ACTUAL" >&2
  exit 1
fi

tar -xzf "$ARCHIVE" -C "$TMPDIR"

if [ ! -f "$TMPDIR/$BIN" ]; then
  echo "archive did not contain $BIN" >&2
  exit 1
fi

mkdir -p "$INSTALL_DIR"
if [ -w "$INSTALL_DIR" ]; then
  install -m 755 "$TMPDIR/$BIN" "$INSTALL_DIR/$BIN"
else
  echo "installing to $INSTALL_DIR requires sudo" >&2
  sudo install -m 755 "$TMPDIR/$BIN" "$INSTALL_DIR/$BIN"
fi

echo "$BIN installed to $INSTALL_DIR/$BIN"
