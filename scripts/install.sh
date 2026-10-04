#!/bin/sh
# Install Toskar Core and, optionally, join a network (#40).
#
#   curl -fsSL https://github.com/yeixio/toskar-core/releases/latest/download/install.sh | sh
#   curl -fsSL https://github.com/yeixio/toskar-core/releases/latest/download/install.sh | \
#     sh -s -- join --server 192.168.1.10:7332 --token ygj_… --fingerprint sha256:…
#
# Linux: the release's .deb (apt) or .rpm (dnf, yum), which run Toskar as
# a systemd service. macOS: the release's headless archive in
# /usr/local/lib/toskar (or ~/.local/lib/toskar without sudo), run by
# launchd. Each download is checked against the release's SHA256SUMS.txt.
# An installed Toskar that is running is left as it is.
#
# Environment (the YGGDRASIL_ names from before the rename work too):
#   TOSKAR_VERSION      a release such as 1.5.0 (default: the latest)
#   TOSKAR_RELEASE_URL  where the release files are (overrides the version)
#   TOSKAR_URL          the daemon's API (default http://127.0.0.1:7331)
#   TOSKAR_PREFIX       macOS install directory
#   TOSKAR_NO_SERVICE=1 install files only; do not register or start a service
set -eu

TOSKAR_VERSION="${TOSKAR_VERSION:-${YGGDRASIL_VERSION:-}}"
TOSKAR_RELEASE_URL="${TOSKAR_RELEASE_URL:-${YGGDRASIL_RELEASE_URL:-}}"
TOSKAR_PREFIX="${TOSKAR_PREFIX:-${YGGDRASIL_PREFIX:-}}"
TOSKAR_BIN_DIR="${TOSKAR_BIN_DIR:-${YGGDRASIL_BIN_DIR:-}}"
TOSKAR_NO_SERVICE="${TOSKAR_NO_SERVICE:-${YGGDRASIL_NO_SERVICE:-}}"

REPO="yeixio/toskar-core"
API="${TOSKAR_URL:-${YGGDRASIL_URL:-http://127.0.0.1:7331}}"

say() { printf '%s\n' "$*"; }
ok() { printf '\342\234\223 %s\n' "$*"; }
die() {
	printf 'Install failed: %s\n' "$*" >&2
	exit 1
}

usage() {
	cat <<'EOF'
usage: install.sh [join --server <host:port> --token <ygj_…> --fingerprint <sha256:…>]
Installs Toskar Core, starts it as a service, and with join, joins this
computer to the network that made the join command.
EOF
}

joining=0
if [ "$#" -gt 0 ]; then
	case "$1" in
	join)
		joining=1
		shift
		;;
	-h | --help | help)
		usage
		exit 0
		;;
	*)
		usage >&2
		exit 2
		;;
	esac
fi

if [ -n "$TOSKAR_RELEASE_URL" ]; then
	BASE="${TOSKAR_RELEASE_URL%/}"
elif [ -n "$TOSKAR_VERSION" ]; then
	BASE="https://github.com/${REPO}/releases/download/v${TOSKAR_VERSION#v}"
else
	BASE="https://github.com/${REPO}/releases/latest/download"
fi

# Run as root when needed, through sudo when not root already.
as_root() {
	if [ "$(id -u)" -eq 0 ]; then
		"$@"
	elif command -v sudo >/dev/null 2>&1; then
		sudo "$@"
	else
		die "this step needs root, and sudo isn't available: run the command as root"
	fi
}

fetch() { # url dest
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL --retry 3 -o "$2" "$1"
	elif command -v wget >/dev/null 2>&1; then
		wget -q -O "$2" "$1"
	else
		die "neither curl nor wget is installed"
	fi
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{print $1}'
	else
		die "neither sha256sum nor shasum is installed, so the download can't be checked"
	fi
}

healthy() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsS -m 2 "${API}/api/v1/health" >/dev/null 2>&1
	else
		wget -q -T 2 -O /dev/null "${API}/api/v1/health" >/dev/null 2>&1
	fi
}

wait_healthy() {
	i=0
	while [ "$i" -lt 60 ]; do
		if healthy; then
			return 0
		fi
		i=$((i + 1))
		sleep 1
	done
	return 1
}

os="$(uname -s)"
case "$(uname -m)" in
x86_64 | amd64) arch="amd64" ;;
aarch64 | arm64) arch="arm64" ;;
*) die "this computer's processor ($(uname -m)) isn't one Toskar is built for" ;;
esac

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

# download_release <pattern>: download the one release file matching an
# extended regular expression, checked against SHA256SUMS.txt, and print its
# path.
# download_release <pattern> [<pattern from before the rename>]: the first
# file in the release that matches, checked against SHA256SUMS.txt. An
# older release (TOSKAR_VERSION) names its packages yggdrasil (#237).
download_release() {
	fetch "${BASE}/SHA256SUMS.txt" "$tmp/SHA256SUMS.txt" || die "couldn't download the release list from ${BASE}"
	line="$(grep -E "  ($1)\$" "$tmp/SHA256SUMS.txt" | head -n 1 || true)"
	if [ -z "$line" ] && [ -n "${2:-}" ]; then
		line="$(grep -E "  ($2)\$" "$tmp/SHA256SUMS.txt" | head -n 1 || true)"
	fi
	[ -n "$line" ] || die "the release has no file for this computer (${os} ${arch})"
	want="${line%% *}"
	name="${line##* }"
	say "Downloading ${name}..." >&2
	fetch "${BASE}/${name}" "$tmp/${name}" || die "couldn't download ${BASE}/${name}"
	got="$(sha256 "$tmp/${name}")"
	[ "$got" = "$want" ] || die "${name} doesn't match its checksum (expected ${want}, got ${got}); nothing was installed"
	printf '%s\n' "$tmp/${name}"
}

# quietly <what> <command…>: run a package manager, and show the end of its
# output if it fails.
quietly() {
	what="$1"
	shift
	if ! "$@" >"$tmp/install.log" 2>&1; then
		tail -n 25 "$tmp/install.log" >&2
		die "$what"
	fi
}

start_service_linux() {
	[ "$TOSKAR_NO_SERVICE" = "1" ] && return 0
	if command -v systemctl >/dev/null 2>&1; then
		as_root systemctl daemon-reload || true
		# toskar.service, or yggdrasil.service from an older release.
		unit="toskar.service"
		systemctl cat "$unit" >/dev/null 2>&1 || unit="yggdrasil.service"
		as_root systemctl enable --now "$unit" >/dev/null 2>&1 || as_root systemctl restart "$unit"
	fi
}

install_linux() {
	if command -v apt-get >/dev/null 2>&1 && command -v dpkg >/dev/null 2>&1; then
		pkg="$(download_release "toskar_[0-9][^ ]*_${arch}\.deb" "yggdrasil_[0-9][^ ]*_${arch}\.deb")"
		quietly "apt-get couldn't install ${pkg##*/}" as_root apt-get install -y "$pkg"
	elif command -v rpm >/dev/null 2>&1; then
		rarch="x86_64"
		[ "$arch" = "arm64" ] && rarch="aarch64"
		pkg="$(download_release "toskar-[0-9][^ ]*\.${rarch}\.rpm" "yggdrasil-[0-9][^ ]*\.${rarch}\.rpm")"
		if command -v dnf >/dev/null 2>&1; then
			quietly "dnf couldn't install ${pkg##*/}" as_root dnf install -y "$pkg"
		elif command -v yum >/dev/null 2>&1; then
			quietly "yum couldn't install ${pkg##*/}" as_root yum install -y "$pkg"
		else
			quietly "rpm couldn't install ${pkg##*/}" as_root rpm -U --replacepkgs "$pkg"
		fi
	else
		die "this Linux has neither apt nor rpm; see https://github.com/${REPO}#install to build from source"
	fi
	start_service_linux
}

install_macos() {
	old_prefix=""
	if [ -n "$TOSKAR_PREFIX" ]; then
		prefix="$TOSKAR_PREFIX"
		bindir="${TOSKAR_BIN_DIR:-$prefix/bin}"
	elif [ "$(id -u)" -eq 0 ]; then
		prefix="/usr/local/lib/toskar"
		old_prefix="/usr/local/lib/yggdrasil"
		bindir="/usr/local/bin"
	else
		prefix="$HOME/.local/lib/toskar"
		old_prefix="$HOME/.local/lib/yggdrasil"
		bindir="$HOME/.local/bin"
	fi
	# Archives from before the rename (TOSKAR_VERSION) are named yggdrasil.
	archive="$(download_release "toskar-[0-9][^ ]*-darwin-${arch}-headless\.tar\.gz" "yggdrasil-[0-9][^ ]*-darwin-${arch}-headless\.tar\.gz")"
	tar -xzf "$archive" -C "$tmp"
	src="$(find "$tmp" -maxdepth 1 -type d \( -name 'toskar-*-headless' -o -name 'yggdrasil-*-headless' \) | head -n 1)"
	[ -n "$src" ] || die "the archive didn't have the expected folder"
	mkdir -p "$prefix" "$bindir"
	rm -rf "$prefix.new"
	cp -R "$src" "$prefix.new"
	rm -rf "$prefix"
	mv "$prefix.new" "$prefix"
	# Downloaded with curl, so not quarantined; clear it anyway for archives
	# fetched by a browser.
	xattr -dr com.apple.quarantine "$prefix" 2>/dev/null || true
	# The install folder from before the rename becomes a link to this one, so
	# MCP settings and scripts that run programs inside it keep working
	# (#237). Only a folder that holds an install of ours is replaced.
	if [ -n "$old_prefix" ] && [ ! -L "$old_prefix" ]; then
		if [ ! -e "$old_prefix" ] || [ -e "$old_prefix/yggdrasil-daemon" ]; then
			rm -rf "$old_prefix"
			ln -s "$prefix" "$old_prefix"
		fi
	fi
	# toskar and toskarctl, and the names from before the rename, which an
	# older release (TOSKAR_VERSION) has alone (#237).
	for name in toskar toskarctl yggdrasil-daemon yggctl; do
		if [ -e "$prefix/$name" ]; then ln -sf "$prefix/$name" "$bindir/$name"; fi
	done
	daemon="$prefix/toskar"
	[ -x "$daemon" ] || daemon="$prefix/yggdrasil-daemon"
	YGGCTL="$prefix/toskarctl"
	[ -x "$YGGCTL" ] || YGGCTL="$prefix/yggctl"
	[ "$TOSKAR_NO_SERVICE" = "1" ] && return 0

	label="ai.toskar.toskar"
	old_label="io.yeix.yggdrasil"
	if [ "$(id -u)" -eq 0 ]; then
		# A server with nobody logged in: a system daemon that runs as the
		# person who ran sudo, so its data stays in their home folder.
		plist="/Library/LaunchDaemons/${label}.plist"
		user="${SUDO_USER:-root}"
		home="$(dscl . -read "/Users/${user}" NFSHomeDirectory 2>/dev/null | awk '{print $2}')"
		[ -n "$home" ] || home="/var/root"
		user_key="<key>UserName</key><string>${user}</string>"
		env_home="<key>HOME</key><string>${home}</string>"
		domain="system"
	else
		plist="$HOME/Library/LaunchAgents/${label}.plist"
		user_key=""
		env_home=""
		domain="gui/$(id -u)"
		mkdir -p "$HOME/Library/LaunchAgents"
	fi
	# YGGDRASIL_WEB_UI_DIR, not TOSKAR_: older releases read only the old name.
	cat >"$plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>${label}</string>
  ${user_key}
  <key>ProgramArguments</key><array><string>${daemon}</string></array>
  <key>EnvironmentVariables</key><dict><key>YGGDRASIL_WEB_UI_DIR</key><string>${prefix}/web</string>${env_home}</dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
</dict>
</plist>
EOF
	launchctl bootout "$domain/$label" >/dev/null 2>&1 || true
	# The service from before the rename, so the two don't both run (#237).
	for d in "$domain" "user/$(id -u)"; do
		launchctl bootout "$d/$old_label" >/dev/null 2>&1 || true
	done
	rm -f "${plist%/*}/${old_label}.plist"
	if ! launchctl bootstrap "$domain" "$plist" 2>/dev/null; then
		# Over SSH there is no login window to start an agent in.
		[ "$domain" = "system" ] && die "launchd couldn't start Toskar"
		launchctl bootstrap "user/$(id -u)" "$plist" || die "launchd couldn't start Toskar; run this with sudo on a computer nobody is logged in to"
	fi
}

YGGCTL="toskarctl"
command -v toskarctl >/dev/null 2>&1 || YGGCTL="yggctl"
if command -v "$YGGCTL" >/dev/null 2>&1 && healthy; then
	ok "Toskar Core is already installed and running"
else
	case "$os" in
	Linux) install_linux ;;
	Darwin) install_macos ;;
	*) die "this script installs on Linux and macOS; on Windows use install.ps1" ;;
	esac
	ok "Toskar Core installed"
	# A package from before the rename has only yggctl.
	if [ "$os" = "Linux" ] && ! command -v toskarctl >/dev/null 2>&1; then
		YGGCTL="yggctl"
	elif [ "$os" = "Linux" ]; then
		YGGCTL="toskarctl"
	fi
	if [ "$TOSKAR_NO_SERVICE" != "1" ] || [ "$joining" -eq 1 ]; then
		say "Waiting for Toskar to start..."
		wait_healthy || die "Toskar didn't start within a minute; see its log (journalctl -u toskar on Linux, the logs folder in ~/Library/Application Support/Toskar, or Yggdrasil for an install from before the rename, on macOS) and try again"
		ok "Toskar Core is running"
	fi
fi

if [ "$joining" -eq 1 ]; then
	TOSKAR_URL="$API" YGGDRASIL_URL="$API" exec "$YGGCTL" join "$@"
fi
say "Open ${API} or run ${YGGCTL##*/} to use it."
