#!/bin/sh
# Install sqa from GitHub releases. POSIX sh, no sudo.
#
#   curl -fsSL https://raw.githubusercontent.com/dansailer/sqa/main/install.sh | sh
#   curl -fsSL ... | sh -s -- --version v0.2.0
#   curl -fsSL ... | sh -s -- --no-skill
#
# Environment:
#   SQA_VERSION, SQA_INSTALL_DIR (default ~/.local/bin), SQA_NO_SKILL=1
#   SQA_DOWNLOAD_BASE, SQA_GITHUB — test hooks (see testdata/install)

set -eu

REPO="dansailer/sqa"
GITHUB_WEB="${SQA_GITHUB:-https://github.com}"

usage() {
	cat <<'EOF'
Install sqa from GitHub releases (linux/amd64).

Usage:
  curl -fsSL https://raw.githubusercontent.com/dansailer/sqa/main/install.sh | sh
  curl -fsSL ... | sh -s -- --version v0.2.0 [--no-skill]

Options:
  --version TAG   Release tag (or SQA_VERSION). Default: latest release.
  --no-skill      Do not run `sqa skill install` (or SQA_NO_SKILL=1).
  -h, --help      Show this help.

Installs to $SQA_INSTALL_DIR (default ~/.local/bin) and verifies the
archive against checksums.txt. No GitHub login or token is needed: the
latest release is read from the github.com/dansailer/sqa/releases/latest
redirect.
EOF
}

die() {
	printf 'install.sh: %s\n' "$*" >&2
	exit 1
}

tmpdir=
cleanup() {
	if [ -n "${tmpdir:-}" ] && [ -d "$tmpdir" ]; then
		rm -rf "$tmpdir"
	fi
}

have_cmd() {
	command -v "$1" >/dev/null 2>&1
}

# download URL DEST
download() {
	if have_cmd curl; then
		curl -fsSL --connect-timeout 30 --max-time 300 -A "sqa-install" -o "$2" "$1"
	else
		wget -q --timeout=30 --tries=3 -U "sqa-install" -O "$2" "$1"
	fi
}

# latest_tag prints the tag that ${GITHUB_WEB}/${REPO}/releases/latest
# redirects to (.../releases/tag/<tag>), without the rate-limited GitHub API.
latest_tag() {
	url="${GITHUB_WEB}/${REPO}/releases/latest"
	if have_cmd curl; then
		final=$(curl -fsSL --connect-timeout 30 --max-time 60 -A "sqa-install" \
			-o /dev/null -w '%{url_effective}' "$url") || return 1
	else
		# wget exits non-zero when it stops at the redirect; read Location.
		final=$(wget -q -S --max-redirect=0 -U "sqa-install" -O /dev/null "$url" 2>&1 |
			sed -n 's/^ *[Ll]ocation: *//p' | tr -d '\r' | tail -n 1)
	fi
	case "$final" in
	*/releases/tag/?*) printf '%s' "${final##*/releases/tag/}" ;;
	*) return 1 ;;
	esac
}

sha256_of() {
	if have_cmd sha256sum; then
		sha256sum "$1" | awk '{print $1}'
	elif have_cmd shasum; then
		shasum -a 256 "$1" | awk '{print $1}'
	else
		die "need sha256sum or shasum to verify the archive"
	fi
}

expected_sha256() {
	# sha256sum format: "<hash>  filename" or "<hash> *filename"
	awk -v name="$2" '
		$2 == name || $2 == ("*" name) { print $1; exit }
	' "$1"
}

os_arch() {
	os=$(uname -s | tr '[:upper:]' '[:lower:]')
	arch=$(uname -m)
	case "$arch" in
	x86_64 | amd64) arch=amd64 ;;
	esac
	if [ "$os" != linux ] || [ "$arch" != amd64 ]; then
		die "only linux/amd64 is supported today (this machine is ${os}/${arch})"
	fi
}

normalize_tag() {
	# Accept v0.2.0 or 0.2.0; the GitHub tag has the v prefix.
	raw=$1
	case "$raw" in
	v*) printf '%s' "$raw" ;;
	*) printf 'v%s' "$raw" ;;
	esac
}

download_base_url() {
	# Join SQA_DOWNLOAD_BASE and a filename. Accepts http(s)://, file://, or a local path.
	base=$1
	name=$2
	case "$base" in
	/*) base="file://${base}" ;;
	esac
	base=${base%/}
	printf '%s/%s' "$base" "$name"
}

resolve_tag() {
	if [ -n "$version" ]; then
		normalize_tag "$version"
		return
	fi
	if [ -n "${SQA_DOWNLOAD_BASE:-}" ]; then
		die "pass --version or set SQA_VERSION when using SQA_DOWNLOAD_BASE"
	fi
	tag=$(latest_tag) || die "could not determine the latest release from ${GITHUB_WEB}/${REPO}/releases/latest (pass --version)"
	printf '%s' "$tag"
}

fetch_assets() {
	tag=$1
	ver=${tag#v}
	archive="sqa_${ver}_linux_amd64.tar.gz"

	if [ -n "${SQA_DOWNLOAD_BASE:-}" ]; then
		download "$(download_base_url "$SQA_DOWNLOAD_BASE" "$archive")" "$tmpdir/$archive"
		download "$(download_base_url "$SQA_DOWNLOAD_BASE" "checksums.txt")" "$tmpdir/checksums.txt"
		printf '%s' "$archive"
		return
	fi

	download "${GITHUB_WEB}/${REPO}/releases/download/${tag}/${archive}" "$tmpdir/$archive"
	download "${GITHUB_WEB}/${REPO}/releases/download/${tag}/checksums.txt" "$tmpdir/checksums.txt"
	printf '%s' "$archive"
}

verify_archive() {
	archive=$1
	sums=$2
	expected=$(expected_sha256 "$sums" "$archive")
	[ -n "$expected" ] || die "checksums.txt has no entry for ${archive}"
	expected=$(printf '%s' "$expected" | tr '[:upper:]' '[:lower:]')
	got=$(sha256_of "$tmpdir/$archive" | tr '[:upper:]' '[:lower:]')
	if [ "$expected" != "$got" ]; then
		die "checksum mismatch for ${archive} (expected ${expected}, got ${got})"
	fi
}

install_binary() {
	archive=$1
	ver=$2
	mkdir -p "$tmpdir/extract"
	tar -xzf "$tmpdir/$archive" -C "$tmpdir/extract"
	bin=""
	# Preferred layout from the release workflow, then any single `sqa` file.
	if [ -f "$tmpdir/extract/sqa_${ver}_linux_amd64/sqa" ]; then
		bin="$tmpdir/extract/sqa_${ver}_linux_amd64/sqa"
	else
		# POSIX: do not use find -print0 / hidden bash arrays.
		count=0
		for f in "$tmpdir"/extract/*/sqa "$tmpdir"/extract/sqa; do
			if [ -f "$f" ]; then
				count=$((count + 1))
				bin=$f
			fi
		done
		[ "$count" -eq 1 ] || die "archive did not contain exactly one sqa binary"
	fi
	mkdir -p "$install_dir"
	cp "$bin" "$install_dir/sqa"
	chmod 755 "$install_dir/sqa"
}

on_path() {
	dir=$1
	# Portable PATH membership: wrap with colons and look for :dir:.
	case ":${PATH}:" in
	*":${dir}:"*) return 0 ;;
	*) return 1 ;;
	esac
}

# --- main ---

version=${SQA_VERSION:-}
no_skill=0
case "${SQA_NO_SKILL:-}" in
1 | true | yes) no_skill=1 ;;
esac

while [ $# -gt 0 ]; do
	case "$1" in
	--version)
		[ $# -ge 2 ] || die "--version needs a value"
		version=$2
		shift 2
		;;
	--version=*)
		version=${1#--version=}
		shift
		;;
	--no-skill)
		no_skill=1
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		die "unknown option: $1"
		;;
	esac
done

os_arch

if ! have_cmd curl && ! have_cmd wget; then
	die "need curl or wget to download sqa"
fi

install_dir=${SQA_INSTALL_DIR:-"${HOME}/.local/bin"}

trap cleanup EXIT INT HUP TERM
tmpdir=$(mktemp -d "${TMPDIR:-/tmp}/sqa-install.XXXXXX")

tag=$(resolve_tag)
ver=${tag#v}
archive=$(fetch_assets "$tag")
verify_archive "$archive" "$tmpdir/checksums.txt"
install_binary "$archive" "$ver"

printf 'installed sqa %s to %s/sqa\n' "$tag" "$install_dir"

if ! on_path "$install_dir"; then
	# $PATH must appear literally in the hint, not expanded.
	# shellcheck disable=SC2016
	printf 'warning: %s is not on PATH. Add it, for example:\n  export PATH="%s:$PATH"\n' \
		"$install_dir" "$install_dir" >&2
fi

if [ "$no_skill" -eq 0 ]; then
	"$install_dir/sqa" skill install
fi
