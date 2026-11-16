#!/bin/sh
# Installs a pitwall release on a Linux host that pitwall --host reaches over
# ssh, and the shared libraries it links when the host lacks them. Install on
# Settings, Hosts runs it there; by hand it is:
#   ssh box 'sh -s' < scripts/get-host.sh
# PITWALL_VERSION picks the release, as v0.2.13; without it, the latest.
set -eu

# why: pitwall-win releases Windows builds only; a host runs upstream's.
repo=quanticstudios/pitwall

# asset names the release archive for `uname -s` and `uname -m`.
asset() {
    if [ "$1" != Linux ]; then
        printf 'pitwall: get-host.sh installs on Linux hosts, not on %s. On macOS run:\n  curl -fsSL https://raw.githubusercontent.com/%s/main/scripts/get.sh | sh\n' "$1" "$repo" >&2
        return 1
    fi
    case "$2" in
        x86_64 | amd64) arch=amd64 ;;
        aarch64 | arm64) arch=arm64 ;;
        *) printf 'pitwall: no release for %s on %s\n' "$2" "$1" >&2; return 1 ;;
    esac
    printf 'pitwall_linux_%s.tar.gz\n' "$arch"
}

sha256() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | cut -d ' ' -f 1
    else
        shasum -a 256 "$1" | cut -d ' ' -f 1
    fi
}

# missing_libs reads ldd's output and prints the shared libraries it did not
# find, one a line.
missing_libs() {
    awk '$2 == "=>" && $3 == "not" && $4 == "found" { print $1 }' | sort -u
}

# apt_package names the Debian or Ubuntu package of shared library $1. These
# are the libraries the release's pitwall links for its window: a host
# without a desktop often lacks some, and pitwall does not start without
# every one, even as a daemon.
apt_package() {
    case "$1" in
        libEGL.so.1) echo libegl1 ;;
        libwayland-egl.so.1) echo libwayland-egl1 ;;
        libwayland-client.so.0) echo libwayland-client0 ;;
        libwayland-cursor.so.0) echo libwayland-cursor0 ;;
        libxkbcommon-x11.so.0) echo libxkbcommon-x11-0 ;;
        libxkbcommon.so.0) echo libxkbcommon0 ;;
        libX11-xcb.so.1) echo libx11-xcb1 ;;
        libX11.so.6) echo libx11-6 ;;
        libXcursor.so.1) echo libxcursor1 ;;
        libXfixes.so.3) echo libxfixes3 ;;
        *) return 1 ;;
    esac
}

# apt_packages prints, on one line, the packages of the libraries $1 lists.
# A library it has no package for fails it.
apt_packages() {
    pkgs=
    for lib in $1; do
        pkg=$(apt_package "$lib") || {
            printf 'pitwall: the host lacks %s, and this script knows no package for it. Install it and run this again.\n' "$lib" >&2
            return 1
        }
        pkgs="$pkgs $pkg"
    done
    printf '%s\n' "${pkgs# }"
}

# install_libs installs the shared libraries the pitwall at $1 lacks on
# this host. It needs apt-get, and root or a sudo that asks no password:
# nobody is there to type one. Without them it says what to run.
install_libs() {
    if ! command -v ldd >/dev/null 2>&1; then
        printf '%s\n' 'pitwall: no ldd on this host, so the libraries pitwall needs cannot be checked' >&2
        exit 1
    fi
    libs=$(ldd "$1" | missing_libs | tr '\n' ' ')
    libs=${libs% }
    if [ -z "$libs" ]; then
        return 0
    fi
    printf 'pitwall links libraries this host lacks: %s\n' "$libs"
    pkgs=$(apt_packages "$libs") || exit 1
    if ! command -v apt-get >/dev/null 2>&1; then
        printf 'pitwall: this script installs libraries only with apt-get, which this host lacks. Install %s with its package manager and run this again.\n' "$libs" >&2
        exit 1
    fi
    if [ "$(id -u)" = 0 ]; then
        sudo=
    elif command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
        sudo='sudo -n'
    else
        printf 'pitwall: installing them needs root, and this user has no sudo without a password. Run on the host, then install again:\n  sudo apt-get install -y %s\n' "$pkgs" >&2
        exit 1
    fi
    printf 'Installing %s with apt-get\n' "$pkgs"
    # why: an unattended upgrade can hold dpkg's lock for minutes.
    # shellcheck disable=SC2086 # $sudo and $pkgs are word lists
    if ! $sudo env DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=120 install -y --no-install-recommends $pkgs >"$tmp/apt.log" 2>&1; then
        tail -n 5 "$tmp/apt.log" >&2
        printf 'pitwall: apt-get did not install %s. If it found no such package, run sudo apt-get update on the host, then install again.\n' "$pkgs" >&2
        exit 1
    fi
    left=$(ldd "$1" | missing_libs | tr '\n' ' ')
    if [ -n "$left" ]; then
        printf 'pitwall: apt-get installed %s, and the host still lacks %s\n' "$pkgs" "$left" >&2
        exit 1
    fi
}

# window_pitwall prints the pitwall a window's remote-start runs on this
# host: the first on the PATH, then the one in ~/.local/bin. None prints
# nothing.
window_pitwall() {
    (
        PATH="$PATH:$HOME/.local/bin"
        command -v pitwall
    ) || true
}

# check_window fails when a window would not run a pitwall that starts,
# though $1, the one just installed, does: another pitwall that does not
# start is ahead of it on the PATH, or $1 is in a folder a window does not
# look in. Installing changed nothing for the window then.
check_window() {
    runs=$(window_pitwall)
    if [ "$runs" = "$1" ]; then
        return 0
    fi
    if [ -z "$runs" ]; then
        printf 'pitwall: a window looks for pitwall on the PATH and in ~/.local/bin, and %s is in neither\n' "$1" >&2
        return 1
    fi
    if "$runs" --version >/dev/null 2>&1; then
        printf 'A window runs %s, which is ahead of %s on the PATH of this host.\n' "$runs" "$1"
        return 0
    fi
    printf 'pitwall: a window runs %s, which is ahead of %s on the PATH of this host and does not start. Remove or repair it, then check again.\n' "$runs" "$1" >&2
    return 1
}

main() {
    for tool in curl tar awk mktemp; do
        if ! command -v "$tool" >/dev/null 2>&1; then
            printf 'pitwall: %s is not installed on this host\n' "$tool" >&2
            exit 1
        fi
    done
    if ! command -v sha256sum >/dev/null 2>&1 && ! command -v shasum >/dev/null 2>&1; then
        printf '%s\n' 'pitwall: neither sha256sum nor shasum is installed on this host' >&2
        exit 1
    fi
    name=$(asset "$(uname -s)" "$(uname -m)")
    version=${PITWALL_VERSION:-}
    if [ -n "$version" ]; then
        base="https://github.com/$repo/releases/download/$version"
    else
        base="https://github.com/$repo/releases/latest/download"
    fi
    dir=${PITWALL_INSTALL_DIR:-"$HOME/.local/bin"}

    tmp=$(mktemp -d)
    trap 'rm -rf "$tmp"' EXIT
    printf 'Downloading %s (%s)\n' "$name" "${version:-latest}"
    for file in "$name" checksums.txt; do
        if ! curl -fsSL -o "$tmp/$file" "$base/$file"; then
            printf 'pitwall: could not download %s\n' "$base/$file" >&2
            exit 1
        fi
    done
    want=$(awk -v n="$name" '$2 == n || $2 == "*" n { print $1 }' "$tmp/checksums.txt")
    got=$(sha256 "$tmp/$name")
    if [ -z "$want" ] || [ "$want" != "$got" ]; then
        printf 'pitwall: %s does not match checksums.txt; nothing installed\n' "$name" >&2
        exit 1
    fi
    tar -xzf "$tmp/$name" -C "$tmp"
    install_libs "$tmp/pitwall"
    mkdir -p "$dir"
    # why: a rename leaves a running pitwall on its old binary.
    cp "$tmp/pitwall" "$dir/.pitwall.new"
    chmod 755 "$dir/.pitwall.new"
    mv -f "$dir/.pitwall.new" "$dir/pitwall"
    # why: a pitwall that does not start is no install; get.sh reports one.
    if ! said=$("$dir/pitwall" --version 2>&1); then
        printf 'pitwall: installed %s, and it does not start: %s\n' "$dir/pitwall" "$said" >&2
        exit 1
    fi
    printf 'Installed %s in %s.\n' "$said" "$dir"
    check_window "$dir/pitwall" || exit 1
    printf 'For agent status in the sidebar, run on the host: %s hooks install\n' "$dir/pitwall"
}

# why: main runs last, so a script cut short on its way here runs nothing.
if [ "${PITWALL_GET_TEST:-}" != 1 ]; then
    main
fi
