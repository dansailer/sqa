#!/bin/sh
# Install sqa from GitHub releases. POSIX sh, no sudo.
#
#   curl -fsSL https://raw.githubusercontent.com/dansailer/sqa/main/install.sh | sh
#   curl -fsSL ... | sh -s -- --version v0.2.0
#   curl -fsSL ... | sh -s -- --no-skill
#
# Environment:
#   SQA_VERSION, SQA_INSTALL_DIR (default ~/.local/bin), SQA_NO_SKILL=1
#   GITHUB_TOKEN or GH_TOKEN — optional, avoids the anonymous GitHub API rate limit
#   SQA_DOWNLOAD_BASE, SQA_GITHUB_API — test hooks (see testdata/install)

set -eu

REPO="dansailer/sqa"
GITHUB_WEB="${SQA_GITHUB:-https://github.com}"
GITHUB_API="${SQA_GITHUB_API:-https://api.github.com}"

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
archive against checksums.txt. No GitHub login is needed. Optionally set
GITHUB_TOKEN or GH_TOKEN, or authenticate with gh(1), to avoid GitHub's
anonymous API rate limit (60 requests/hour per IP) when looking up the
latest release; pinning --version avoids that lookup.
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

github_token() {
	if [ -n "${GITHUB_TOKEN:-}" ]; then
		printf '%s' "$GITHUB_TOKEN"
	elif [ -n "${GH_TOKEN:-}" ]; then
		printf '%s' "$GH_TOKEN"
	fi
}

has_github_token() {
	[ -n "${GITHUB_TOKEN:-}" ] || [ -n "${GH_TOKEN:-}" ]
}

have_cmd() {
	command -v "$1" >/dev/null 2>&1
}

# download URL DEST [api]
# The optional third argument "api" adds a GitHub auth header and the
# Accept value in $4 (default application/octet-stream). The token is
# never printed.
download() {
	url=$1
	dest=$2
	mode=${3:-}
	accept=${4:-application/octet-stream}

	if have_cmd curl; then
		if [ "$mode" = api ]; then
			token=$(github_token)
			curl -fsSL --connect-timeout 30 --max-time 300 \
				-A "sqa-install" \
				-H "Authorization: Bearer ${token}" \
				-H "Accept: ${accept}" \
				-H "X-GitHub-Api-Version: 2022-11-28" \
				-o "$dest" \
				"$url"
		else
			curl -fsSL --connect-timeout 30 --max-time 300 \
				-A "sqa-install" \
				-o "$dest" \
				"$url"
		fi
	elif have_cmd wget; then
		if [ "$mode" = api ]; then
			token=$(github_token)
			wget -q --timeout=30 --tries=3 \
				-U "sqa-install" \
				--header="Authorization: Bearer ${token}" \
				--header="Accept: ${accept}" \
				--header="X-GitHub-Api-Version: 2022-11-28" \
				-O "$dest" \
				"$url"
		else
			wget -q --timeout=30 --tries=3 \
				-U "sqa-install" \
				-O "$dest" \
				"$url"
		fi
	else
		die "need curl or wget to download sqa"
	fi
}

# Asset API URL for NAME from a GitHub release JSON file.
asset_api_url() {
	json=$1
	want=$2
	awk -v want="$want" '
		BEGIN { RS = "{" }
		{
			name = ""
			url = ""
			n = split($0, a, "\"")
			for (i = 1; i <= n; i++) {
				if (a[i] != "name" && a[i] != "url") {
					continue
				}
				# a[i+1] is the text between this key and the next
				# quoted string, typically `: ` or `": "`.
				if (a[i] == "name") {
					name = a[i + 2]
				}
				if (a[i] == "url" && a[i + 2] ~ /\/releases\/assets\/[0-9]+$/) {
					url = a[i + 2]
				}
			}
			if (name == want && url != "") {
				print url
				exit 0
			}
		}
	' "$json"
}

tag_from_release_json() {
	sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$1" | head -n 1
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
	if has_github_token; then
		release_json="$tmpdir/release.json"
		download "${GITHUB_API}/repos/${REPO}/releases/latest" "$release_json" api "application/vnd.github+json"
		tag=$(tag_from_release_json "$release_json")
		[ -n "$tag" ] || die "could not read tag_name from the GitHub latest-release response"
		printf '%s' "$tag"
		return
	fi
	if have_cmd gh && gh auth status >/dev/null 2>&1; then
		tag=$(gh release view --repo "$REPO" --json tagName --jq .tagName)
		[ -n "$tag" ] || die "gh could not determine the latest sqa release"
		printf '%s' "$tag"
		return
	fi
	release_json="$tmpdir/release.json"
	download "${GITHUB_API}/repos/${REPO}/releases/latest" "$release_json"
	tag=$(tag_from_release_json "$release_json")
	[ -n "$tag" ] || die "could not determine the latest release (GitHub API rate limit? pass --version, or set GITHUB_TOKEN or authenticate with gh)"
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

	if has_github_token; then
		release_json="$tmpdir/release.json"
		if [ ! -s "$release_json" ]; then
			download "${GITHUB_API}/repos/${REPO}/releases/tags/${tag}" "$release_json" api "application/vnd.github+json"
		fi
		archive_url=$(asset_api_url "$release_json" "$archive")
		sums_url=$(asset_api_url "$release_json" "checksums.txt")
		[ -n "$archive_url" ] || die "release ${tag} has no asset ${archive}"
		[ -n "$sums_url" ] || die "release ${tag} has no asset checksums.txt"
		download "$archive_url" "$tmpdir/$archive" api
		download "$sums_url" "$tmpdir/checksums.txt" api
		printf '%s' "$archive"
		return
	fi

	if have_cmd gh && gh auth status >/dev/null 2>&1; then
		gh release download "$tag" --repo "$REPO" --dir "$tmpdir" \
			--pattern "$archive" --pattern "checksums.txt" >/dev/null
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
