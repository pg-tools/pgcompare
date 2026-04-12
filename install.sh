#!/bin/sh
set -eu

PROJECT_NAME="pgcompare"
GITHUB_REPO="pg-tools/pgcompare"

BINDIR="${HOME}/.local/bin"
VERSION=""

usage() {
	cat <<'EOF'
Install pgcompare from GitHub Releases.

Usage:
  install.sh [-b <bindir>] [-v <version>]

Options:
  -b, --bindir   Installation directory (default: ~/.local/bin)
  -v, --version  Release tag to install (default: latest, e.g. v0.1.0)
  -h, --help     Show this help
EOF
}

err() {
	echo "error: $*" >&2
	exit 1
}

need_cmd() {
	command -v "$1" >/dev/null 2>&1 || err "required command not found: $1"
}

checksum_file() {
	file="$1"
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$file" | awk '{print $1}'
		return 0
	fi
	if command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$file" | awk '{print $1}'
		return 0
	fi
	if command -v openssl >/dev/null 2>&1; then
		openssl dgst -sha256 "$file" | awk '{print $NF}'
		return 0
	fi
	err "no SHA256 tool found (sha256sum/shasum/openssl)"
}

detect_os() {
	os="$(uname -s | tr '[:upper:]' '[:lower:]')"
	case "$os" in
	linux | darwin) echo "$os" ;;
	*) err "unsupported OS: $os (supported: linux, darwin)" ;;
	esac
}

detect_arch() {
	arch="$(uname -m)"
	case "$arch" in
	x86_64 | amd64) echo "amd64" ;;
	aarch64 | arm64) echo "arm64" ;;
	*) err "unsupported architecture: $arch (supported: amd64, arm64)" ;;
	esac
}

fetch_latest_version() {
	api_url="https://api.github.com/repos/${GITHUB_REPO}/releases/latest"

	if [ -n "${GITHUB_TOKEN:-}" ]; then
		curl -fsSL -H "Authorization: Bearer ${GITHUB_TOKEN}" "$api_url" \
			| sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' \
			| head -n 1
		return 0
	fi

	curl -fsSL "$api_url" \
		| sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' \
		| head -n 1
}

while [ "$#" -gt 0 ]; do
	case "$1" in
	-b | --bindir)
		[ "$#" -ge 2 ] || err "missing value for $1"
		BINDIR="$2"
		shift 2
		;;
	-v | --version)
		[ "$#" -ge 2 ] || err "missing value for $1"
		VERSION="$2"
		shift 2
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		err "unknown argument: $1 (use --help)"
		;;
	esac
done

need_cmd curl
need_cmd tar
need_cmd awk
need_cmd sed
need_cmd find

OS="$(detect_os)"
ARCH="$(detect_arch)"

if [ -z "$VERSION" ]; then
	VERSION="$(fetch_latest_version)"
	[ -n "$VERSION" ] || err "failed to resolve latest release tag"
fi

ASSET="${PROJECT_NAME}_${OS}_${ARCH}.tar.gz"
BASE_URL="https://github.com/${GITHUB_REPO}/releases/download/${VERSION}"

TMP_DIR="$(mktemp -d)"
cleanup() {
	rm -rf "$TMP_DIR"
}
trap cleanup EXIT INT TERM

ARCHIVE_PATH="${TMP_DIR}/${ASSET}"
CHECKSUMS_PATH="${TMP_DIR}/checksums.txt"

echo "Installing ${PROJECT_NAME} ${VERSION} for ${OS}/${ARCH}..."

curl -fsSL "${BASE_URL}/checksums.txt" -o "$CHECKSUMS_PATH" \
	|| err "failed to download checksums: ${BASE_URL}/checksums.txt"
curl -fsSL "${BASE_URL}/${ASSET}" -o "$ARCHIVE_PATH" \
	|| err "failed to download archive: ${BASE_URL}/${ASSET}"

EXPECTED_SHA="$(awk -v file="$ASSET" '$2 == file { print $1 }' "$CHECKSUMS_PATH" | head -n 1)"
if [ -z "$EXPECTED_SHA" ]; then
	EXPECTED_SHA="$(awk -v file="$ASSET" '$2 == "*" file { print $1 }' "$CHECKSUMS_PATH" | head -n 1)"
fi
[ -n "$EXPECTED_SHA" ] || err "checksum for ${ASSET} not found in checksums.txt"

ACTUAL_SHA="$(checksum_file "$ARCHIVE_PATH")"
[ "$ACTUAL_SHA" = "$EXPECTED_SHA" ] || err "checksum mismatch for ${ASSET}"

tar -xzf "$ARCHIVE_PATH" -C "$TMP_DIR" || err "failed to extract archive"

BINARY_PATH="$(find "$TMP_DIR" -type f -name "$PROJECT_NAME" | head -n 1)"
[ -n "$BINARY_PATH" ] || err "binary ${PROJECT_NAME} not found in archive"

mkdir -p "$BINDIR"
[ -w "$BINDIR" ] || err "no write permission to ${BINDIR}; choose another -b or run with sudo"

if command -v install >/dev/null 2>&1; then
	install -m 0755 "$BINARY_PATH" "${BINDIR}/${PROJECT_NAME}"
else
	cp "$BINARY_PATH" "${BINDIR}/${PROJECT_NAME}"
	chmod 0755 "${BINDIR}/${PROJECT_NAME}"
fi

echo "Installed ${PROJECT_NAME} to ${BINDIR}/${PROJECT_NAME}"

case ":$PATH:" in
*":${BINDIR}:"*) ;;
*)
	echo "Add ${BINDIR} to PATH, for example:"
	echo "  export PATH=\"${BINDIR}:\$PATH\""
	;;
esac
