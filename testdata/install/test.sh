#!/bin/sh
# Tests for install.sh: local fake release (file:// and a github.com
# stand-in that redirects releases/latest) plus checksum failures. Live
# anonymous installs from github.com are last and skipped if it is not
# reachable.
set -eu

CDPATH=
root=$(cd "$(dirname "$0")/../.." && pwd)
install="$root/install.sh"
fake_py="$root/testdata/install/fake_github.py"
failed=0

# Ambient GitHub tokens must not leak into local servers or test logs.
# Each install.sh invocation below starts from a cleaned environment.
run() {
	env -u GH_TOKEN -u GITHUB_TOKEN -u GH_ENTERPRISE_TOKEN -u MISE_GITHUB_TOKEN \
		"$@"
}

work=$(mktemp -d "${TMPDIR:-/tmp}/sqa-install-test.XXXXXX")
servers=""
cleanup() {
	if [ -n "$servers" ]; then
		# shellcheck disable=SC2086
		kill $servers >/dev/null 2>&1 || true
		# shellcheck disable=SC2086
		wait $servers >/dev/null 2>&1 || true
	fi
	rm -rf "$work"
}
trap cleanup EXIT INT HUP TERM

pass() { printf 'ok  %s\n' "$*"; }
fail() {
	printf 'FAIL  %s\n' "$*" >&2
	failed=1
}

contains_token() {
	# $1 = file, $2 = secret. Exit 0 if the secret appears.
	# Does not print the secret.
	haystack=$1
	needle=$2
	[ -n "$needle" ] || return 1
	grep -F -q -- "$needle" "$haystack"
}

assert_no_secret() {
	file=$1
	secret=$2
	label=$3
	if contains_token "$file" "$secret"; then
		fail "$label leaked a secret into the log"
	fi
}

make_release() {
	dest=$1
	mkdir -p "$dest/sqa_0.0.0_linux_amd64"
	cat >"$dest/sqa_0.0.0_linux_amd64/sqa" <<'EOF'
#!/bin/sh
if [ -n "${SQA_STUB_LOG:-}" ]; then
	printf '%s\n' "$*" >>"$SQA_STUB_LOG"
fi
if [ "${1:-}" = version ]; then
	printf 'sqa 0.0.0-test\n'
fi
if [ "${1:-}" = skill ] && [ "${2:-}" = install ]; then
	printf 'installed skill\n'
fi
exit 0
EOF
	chmod 755 "$dest/sqa_0.0.0_linux_amd64/sqa"
	tar -C "$dest" --sort=name -czf "$dest/sqa_0.0.0_linux_amd64.tar.gz" sqa_0.0.0_linux_amd64
	# checksums.txt must name the archive the same way the release workflow does.
	(cd "$dest" && sha256sum -- sqa_0.0.0_linux_amd64.tar.gz >checksums.txt)
}

free_port() {
	python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'
}

start_api_port=
start_api() {
	# $1 = release dir, $2 = tag ("" = no release), $3 = log file
	# Sets start_api_port. Must not run in a command substitution:
	# the server is backgrounded in this shell.
	releasedir=$1
	tag=$2
	log=$3
	start_api_port=
	port=$(free_port)
	FAKE_RELEASE_DIR="$releasedir" FAKE_HOST=127.0.0.1 FAKE_PORT="$port" \
		FAKE_TAG="$tag" \
		python3 "$fake_py" >"$log" 2>&1 &
	servers="$servers $!"
	n=0
	while [ "$n" -lt 50 ]; do
		if grep -q '^ready ' "$log" 2>/dev/null; then
			start_api_port=$port
			return 0
		fi
		n=$((n + 1))
		sleep 0.1
	done
	printf 'server log:\n' >&2
	cat "$log" >&2
	return 1
}

# --- fixtures ---
release="$work/release"
make_release "$release"
home="$work/home"
mkdir -p "$home"
bindir="$work/opt/sqa/bin"
log="$work/log.txt"

# 1. file:// install, --no-skill, directory not on PATH
: >"$log"
if run HOME="$home" SQA_STUB_LOG="$work/stub.log" \
	SQA_DOWNLOAD_BASE="file://${release}" \
	SQA_INSTALL_DIR="$bindir" \
	"$install" --version v0.0.0 --no-skill >"$log" 2>&1; then
	if [ -x "$bindir/sqa" ] && [ "$("$bindir/sqa" version)" = "sqa 0.0.0-test" ]; then
		pass "file:// install --no-skill"
	else
		fail "file:// install did not write a working binary"
	fi
else
	fail "file:// install exited non-zero"
	cat "$log" >&2
fi
if grep -q 'skill install' "$work/stub.log" 2>/dev/null; then
	fail "--no-skill still ran skill install"
else
	pass "--no-skill skipped skill install"
fi
if grep -q "warning: ${bindir} is not on PATH" "$log"; then
	pass "warns when install dir is not on PATH"
else
	fail "missing PATH warning"
	cat "$log" >&2
fi

# 2. SQA_VERSION + SQA_NO_SKILL + local path (no file:// prefix) and skill off
bindir2="$work/opt/sqa2/bin"
: >"$work/stub2.log"
if run HOME="$home" SQA_STUB_LOG="$work/stub2.log" \
	SQA_DOWNLOAD_BASE="$release" \
	SQA_INSTALL_DIR="$bindir2" \
	SQA_VERSION=0.0.0 \
	SQA_NO_SKILL=1 \
	"$install" >"$log" 2>&1; then
	if [ -x "$bindir2/sqa" ]; then
		pass "SQA_VERSION and path SQA_DOWNLOAD_BASE"
	else
		fail "path SQA_DOWNLOAD_BASE did not install a binary"
	fi
else
	fail "SQA_VERSION install exited non-zero"
	cat "$log" >&2
fi

# 3. skill install is run by default
bindir3="$work/opt/sqa3/bin"
: >"$work/stub3.log"
if run HOME="$home" SQA_STUB_LOG="$work/stub3.log" \
	SQA_DOWNLOAD_BASE="$release" \
	SQA_INSTALL_DIR="$bindir3" \
	PATH="${bindir3}:$PATH" \
	"$install" --version v0.0.0 >"$log" 2>&1; then
	if grep -qx 'skill install' "$work/stub3.log"; then
		pass "runs sqa skill install by default"
	else
		fail "skill install was not called"
		cat "$work/stub3.log" "$log" >&2
	fi
else
	fail "default skill install run exited non-zero"
	cat "$log" >&2
fi

# 4. checksum mismatch fails hard and does not install
bad="$work/bad-release"
mkdir -p "$bad"
cp "$release/sqa_0.0.0_linux_amd64.tar.gz" "$bad/"
printf '%s  %s\n' "$(printf '0%.0s' $(seq 1 64))" "sqa_0.0.0_linux_amd64.tar.gz" >"$bad/checksums.txt"
bindir4="$work/opt/bad/bin"
if run HOME="$home" SQA_DOWNLOAD_BASE="$bad" SQA_INSTALL_DIR="$bindir4" \
	"$install" --version v0.0.0 --no-skill >"$log" 2>&1; then
	fail "checksum mismatch exited 0"
	cat "$log" >&2
else
	if grep -q 'checksum mismatch' "$log" && [ ! -e "$bindir4/sqa" ]; then
		pass "checksum mismatch fails and does not install"
	else
		fail "checksum mismatch did not report a mismatch or left a binary"
		cat "$log" >&2
	fi
fi

# 5. missing checksum entry
missing="$work/missing-sum"
mkdir -p "$missing"
cp "$release/sqa_0.0.0_linux_amd64.tar.gz" "$missing/"
printf '%s  %s\n' "$(sha256sum "$missing/sqa_0.0.0_linux_amd64.tar.gz" | awk '{print $1}')" "other.tar.gz" >"$missing/checksums.txt"
bindir5="$work/opt/missing/bin"
if run HOME="$home" SQA_DOWNLOAD_BASE="$missing" SQA_INSTALL_DIR="$bindir5" \
	"$install" --version v0.0.0 --no-skill >"$log" 2>&1; then
	fail "missing checksum entry exited 0"
else
	if grep -q 'checksums.txt has no entry' "$log" && [ ! -e "$bindir5/sqa" ]; then
		pass "missing checksum entry fails hard"
	else
		fail "missing checksum entry: unexpected output"
		cat "$log" >&2
	fi
fi

# 6. unsupported OS/arch
shim="$work/shim"
mkdir -p "$shim"
cat >"$shim/uname" <<'EOF'
#!/bin/sh
[ "${1:-}" = -s ] && { printf 'Darwin\n'; exit 0; }
[ "${1:-}" = -m ] && { printf 'arm64\n'; exit 0; }
exec /usr/bin/uname "$@"
EOF
chmod 755 "$shim/uname"
if run HOME="$home" PATH="${shim}:$PATH" SQA_DOWNLOAD_BASE="$release" \
	SQA_INSTALL_DIR="$work/opt/darwin/bin" \
	"$install" --version v0.0.0 --no-skill >"$log" 2>&1; then
	fail "unsupported arch exited 0"
else
	if grep -q 'only linux/amd64 is supported today (this machine is darwin/arm64)' "$log"; then
		pass "unsupported OS/arch error"
	else
		fail "unsupported arch: unexpected output"
		cat "$log" >&2
	fi
fi

# 7. latest release from the releases/latest redirect, with curl and with
# wget only. A token in the environment and a logged-in gh are ignored:
# the server refuses any Authorization header and gh must not run.
test_token="sqa-test-token-do-not-leak"
api_log="$work/web-server.log"
if ! start_api "$release" v0.0.0 "$api_log"; then
	fail "fake github.com server failed to start"
fi
port=$start_api_port
ghstub="$work/ghstub"
mkdir -p "$ghstub"
printf '#!/bin/sh\nprintf "%%s\\n" "$*" >>"%s/gh.log"\nexit 0\n' "$work" >"$ghstub/gh"
chmod 755 "$ghstub/gh"
nocurl="$work/nocurl"
mkdir -p "$nocurl"
for d in /usr/local/bin /usr/bin /bin; do
	for f in "$d"/*; do
		n=${f##*/}
		[ "$n" = curl ] || [ -e "$nocurl/$n" ] || ln -s "$f" "$nocurl/$n"
	done
done
for client in curl wget; do
	path="${ghstub}:$PATH"
	if [ "$client" = wget ]; then
		if [ ! -e "$nocurl/wget" ]; then
			printf 'skipped: wget is not installed\n'
			continue
		fi
		path="${ghstub}:${nocurl}"
	fi
	bindir7="$work/opt/web-$client/bin"
	if [ -n "$port" ] && run HOME="$home" PATH="$path" \
		GITHUB_TOKEN="$test_token" GH_TOKEN="$test_token" \
		SQA_GITHUB="http://127.0.0.1:${port}" \
		SQA_INSTALL_DIR="$bindir7" \
		"$install" --no-skill >"$log" 2>&1; then
		if [ -x "$bindir7/sqa" ] && grep -q 'installed sqa v0.0.0' "$log"; then
			pass "latest release via the releases/latest redirect ($client)"
		else
			fail "redirect install ($client) did not install v0.0.0"
			cat "$log" >&2
		fi
	elif [ -n "$port" ]; then
		fail "redirect install ($client) exited non-zero"
		cat "$log" >&2
	fi
	assert_no_secret "$log" "$test_token" "install.sh ($client)"
done
if grep -q 'AUTHORIZATION' "$api_log"; then
	fail "install.sh sent an Authorization header"
else
	pass "no Authorization header sent"
fi
if [ -e "$work/gh.log" ]; then
	fail "install.sh ran gh"
else
	pass "gh is not used"
fi

# 8. web path, wrong checksum still fails
bad2="$work/bad-web"
mkdir -p "$bad2"
cp "$release/sqa_0.0.0_linux_amd64.tar.gz" "$bad2/"
printf '%s  %s\n' "$(printf 'a%.0s' $(seq 1 64))" "sqa_0.0.0_linux_amd64.tar.gz" >"$bad2/checksums.txt"
if ! start_api "$bad2" v0.0.0 "$work/web-server2.log"; then
	fail "fake github.com server (mismatch) failed to start"
fi
port2=$start_api_port
bindir8="$work/opt/web-bad/bin"
if [ -n "$port2" ] && run HOME="$home" SQA_GITHUB="http://127.0.0.1:${port2}" \
	SQA_INSTALL_DIR="$bindir8" \
	"$install" --version v0.0.0 --no-skill >"$log" 2>&1; then
	fail "web checksum mismatch exited 0"
elif [ -n "$port2" ]; then
	if grep -q 'checksum mismatch' "$log" && [ ! -e "$bindir8/sqa" ]; then
		pass "web checksum mismatch fails and does not install"
	else
		fail "web checksum mismatch: unexpected output"
		cat "$log" >&2
	fi
fi

# 8b. no release yet: releases/latest redirects to /releases
if ! start_api "$release" "" "$work/web-server3.log"; then
	fail "fake github.com server (no release) failed to start"
fi
port3=$start_api_port
if [ -n "$port3" ] && run HOME="$home" SQA_GITHUB="http://127.0.0.1:${port3}" \
	SQA_INSTALL_DIR="$work/opt/none/bin" \
	"$install" --no-skill >"$log" 2>&1; then
	fail "no release exited 0"
elif [ -n "$port3" ]; then
	if grep -q 'could not determine the latest release' "$log"; then
		pass "no release: clear error"
	else
		fail "no release: unexpected output"
		cat "$log" >&2
	fi
fi

# 8c. install.ps1 against the same stand-in, when pwsh is installed
# (GitHub's Ubuntu runners have it).
if ! command -v pwsh >/dev/null 2>&1; then
	printf 'skipped: pwsh is not installed (install.ps1 tests)\n'
else
	winrel="$work/win-release"
	mkdir -p "$winrel"
	python3 -c 'import sys, zipfile
with zipfile.ZipFile(sys.argv[1], "w") as z:
    z.writestr("sqa_0.0.0_windows_amd64/sqa.exe", "fake")' "$winrel/sqa_0.0.0_windows_amd64.zip"
	(cd "$winrel" && sha256sum -- sqa_0.0.0_windows_amd64.zip >checksums.txt)
	start_api "$winrel" v0.0.0 "$work/ps-server.log" || fail "fake github.com server (ps1) failed to start"
	psport=$start_api_port
	psdir="$work/opt/ps/bin"
	if run HOME="$home" GITHUB_TOKEN="$test_token" SQA_GITHUB="http://127.0.0.1:${psport}" \
		SQA_INSTALL_DIR="$psdir" SQA_NO_SKILL=1 \
		pwsh -NoProfile -NonInteractive -File "$root/install.ps1" >"$log" 2>&1 &&
		[ -f "$psdir/sqa.exe" ] && grep -q 'installed sqa v0.0.0' "$log"; then
		pass "install.ps1: latest release via the redirect"
	else
		fail "install.ps1 redirect install"
		cat "$log" >&2
	fi
	if grep -q 'AUTHORIZATION' "$work/ps-server.log"; then
		fail "install.ps1 sent an Authorization header"
	fi
	printf '%s  %s\n' "$(printf 'b%.0s' $(seq 1 64))" sqa_0.0.0_windows_amd64.zip >"$winrel/checksums.txt"
	if run HOME="$home" SQA_GITHUB="http://127.0.0.1:${psport}" SQA_VERSION=v0.0.0 \
		SQA_INSTALL_DIR="$work/opt/ps-bad/bin" SQA_NO_SKILL=1 \
		pwsh -NoProfile -NonInteractive -File "$root/install.ps1" >"$log" 2>&1; then
		fail "install.ps1 checksum mismatch exited 0"
	elif grep -q 'checksum mismatch' "$log" && [ ! -e "$work/opt/ps-bad/bin/sqa.exe" ]; then
		pass "install.ps1: checksum mismatch fails"
	else
		fail "install.ps1 checksum mismatch: unexpected output"
		cat "$log" >&2
	fi
	if run HOME="$home" SQA_GITHUB="http://127.0.0.1:${port3}" \
		SQA_INSTALL_DIR="$work/opt/ps-none/bin" SQA_NO_SKILL=1 \
		pwsh -NoProfile -NonInteractive -File "$root/install.ps1" >"$log" 2>&1; then
		fail "install.ps1 without a release exited 0"
	elif grep -q 'could not determine the latest release' "$log"; then
		pass "install.ps1: no release, clear error"
	else
		fail "install.ps1 no release: unexpected output"
		cat "$log" >&2
	fi
fi

# 9. --help works on any OS and does not download
if run "$install" --help >"$log" 2>&1; then
	if grep -q 'Install sqa from GitHub releases' "$log"; then
		pass "--help"
	else
		fail "--help output missing usage"
	fi
else
	fail "--help exited non-zero"
fi

# --- optional live v0.2.0: anonymous download from the public repository
# (skipped, not failed, when github.com is unreachable)
printf '\nLive v0.2.0 release:\n'
if ! curl -fsSI --max-time 10 -o /dev/null "https://github.com/dansailer/sqa/releases/tag/v0.2.0" 2>/dev/null; then
	printf 'skipped: github.com is unreachable\n'
else
	live="$work/live"
	mkdir -p "$live" "$work/nogh"
	# No token and no usable gh (a stub that is never logged in): the public
	# release must install anonymously.
	printf '#!/bin/sh\nexit 1\n' >"$work/nogh/gh"
	chmod 755 "$work/nogh/gh"
	nogh_path="$work/nogh:$PATH"
	if run SQA_INSTALL_DIR="$live" HOME="$live/home" PATH="$nogh_path" \
		"$install" --version v0.2.0 --no-skill >"$work/live.log" 2>&1; then
		if [ -x "$live/sqa" ]; then
			# version may be a short string; just require it to run
			if HOME="$live/home" "$live/sqa" version >"$work/live-ver.txt" 2>&1; then
				pass "live v0.2.0 install"
				cat "$work/live-ver.txt"
			else
				fail "live v0.2.0 binary failed to run version"
				cat "$work/live-ver.txt" >&2
			fi
		else
			fail "live v0.2.0 did not install a binary"
		fi
	else
		fail "live v0.2.0 install exited non-zero"
		cat "$work/live.log" >&2
	fi
	latest="$work/live-latest"
	mkdir -p "$latest"
	if run SQA_INSTALL_DIR="$latest" HOME="$latest/home" PATH="$nogh_path" \
		"$install" --no-skill >"$work/latest.log" 2>&1 && [ -x "$latest/sqa" ]; then
		pass "live anonymous install of the latest release: $(grep -o 'installed sqa v[^ ]*' "$work/latest.log")"
	else
		fail "live latest install failed"
		cat "$work/latest.log" >&2
	fi
fi

if [ "$failed" -ne 0 ]; then
	printf '\ninstall.sh tests failed\n' >&2
	exit 1
fi
printf '\nall install.sh tests passed\n'
