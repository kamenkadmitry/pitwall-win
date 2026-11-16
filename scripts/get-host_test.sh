#!/bin/sh
# Checks get-host.sh's platform, library and package mapping:
#   sh scripts/get-host_test.sh
set -eu

export PITWALL_GET_TEST=1
# shellcheck source=scripts/get-host.sh
. "$(dirname -- "$0")/get-host.sh"

fail=0
check() {
    what=$1 want=$2
    shift 2
    got=$("$@" 2>/dev/null) || got=error
    if [ "$got" != "$want" ]; then
        printf '%s = %s, want %s\n' "$what" "$got" "$want" >&2
        fail=1
    fi
}
check 'asset Linux x86_64' pitwall_linux_amd64.tar.gz asset Linux x86_64
check 'asset Linux aarch64' pitwall_linux_arm64.tar.gz asset Linux aarch64
check 'asset Linux armv7l' error asset Linux armv7l
check 'asset Darwin arm64' error asset Darwin arm64
check 'asset MINGW64_NT-10.0 x86_64' error asset MINGW64_NT-10.0 x86_64

# ldd's lines, as on a host without libxkbcommon-x11 and libEGL.
ldd_out() {
    printf '\t%s\n' \
        'linux-vdso.so.1 (0x00007ecbcf749000)' \
        'libEGL.so.1 => not found' \
        'libwayland-client.so.0 => /lib/x86_64-linux-gnu/libwayland-client.so.0 (0x00007ecbcf715000)' \
        'libxkbcommon-x11.so.0 => not found' \
        'libEGL.so.1 => not found' \
        '/lib64/ld-linux-x86-64.so.2 (0x00007ecbcf74b000)'
}
missing() { ldd_out | missing_libs | tr '\n' ' '; }
check 'missing_libs' 'libEGL.so.1 libxkbcommon-x11.so.0 ' missing
none() { printf '\tlibc.so.6 => /lib/x86_64-linux-gnu/libc.so.6 (0x1)\n' | missing_libs; }
check 'missing_libs of a complete host' '' none

check 'apt_packages' 'libegl1 libxkbcommon-x11-0' apt_packages 'libEGL.so.1 libxkbcommon-x11.so.0'
check 'apt_packages of every library' \
    'libegl1 libwayland-egl1 libwayland-client0 libwayland-cursor0 libxkbcommon-x11-0 libxkbcommon0 libx11-xcb1 libx11-6 libxcursor1 libxfixes3' \
    apt_packages 'libEGL.so.1 libwayland-egl.so.1 libwayland-client.so.0 libwayland-cursor.so.0 libxkbcommon-x11.so.0 libxkbcommon.so.0 libX11-xcb.so.1 libX11.so.6 libXcursor.so.1 libXfixes.so.3'
check 'apt_packages of an unknown library' error apt_packages 'libEGL.so.1 libnope.so.9'

# install_libs and check_window run against stand-ins for the host's ldd, id,
# sudo, apt-get and pitwall, which log their calls to $work/calls.
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
bin=$work/bin
mkdir -p "$bin"
stub() {
    printf '#!/bin/sh\n%s\n' "$2" >"$1"
    chmod +x "$1"
}
# ldd misses $FAKE_MISSING until apt-get has run, or for good with FAKE_APT_NOOP.
# shellcheck disable=SC2016 # the stubs expand their variables, not this script
stub "$bin/ldd" 'if [ ! -e "$FAKE_WORK/installed" ] || [ -n "${FAKE_APT_NOOP:-}" ]; then
    for l in $FAKE_MISSING; do printf "\t%s => not found\n" "$l"; done
fi
printf "\tlibc.so.6 => /lib/x86_64-linux-gnu/libc.so.6 (0x1)\n"'
# shellcheck disable=SC2016
stub "$bin/id" 'echo "$FAKE_UID"'
# sudo -n runs its command, unless FAKE_SUDO_ASKS: then it would ask a password.
# shellcheck disable=SC2016
stub "$bin/sudo" 'echo "sudo $*" >>"$FAKE_WORK/calls"
[ -z "${FAKE_SUDO_ASKS:-}" ] || { echo "sudo: a password is required" >&2; exit 1; }
shift
exec "$@"'
# shellcheck disable=SC2016
stub "$bin/apt-get" 'echo "apt-get $*" >>"$FAKE_WORK/calls"
[ -z "${FAKE_APT_FAILS:-}" ] || { echo "E: Unable to locate package"; exit 100; }
: >"$FAKE_WORK/installed"'

# libs runs install_libs as uid $1 on a host that lacks the libraries $2;
# further arguments set the stubs. It leaves the output in $out, the exit
# status in $rc and the stubs' calls, joined by semicolons, in $calls.
libs() {
    uid=$1 missing=$2
    shift 2
    rm -f "$work/installed" "$work/calls"
    rc=0
    out=$(
        export FAKE_WORK="$work" FAKE_UID="$uid" FAKE_MISSING="$missing" "$@"
        PATH="$bin:$PATH"
        tmp=$work
        install_libs "$work/pitwall" 2>&1
    ) || rc=$?
    calls=
    if [ -e "$work/calls" ]; then
        calls=$(tr '\n' ';' <"$work/calls")
    fi
}
# expect checks the last libs or window run: its name, exit status, a piece
# of its output and, for libs, the stubs' calls.
expect() {
    if [ "$rc" != "$2" ]; then
        printf '%s: exit status %s, want %s; it printed: %s\n' "$1" "$rc" "$2" "$out" >&2
        fail=1
    fi
    case "$out" in
        *"$3"*) ;;
        *)
            printf '%s: it printed %s, want %s in it\n' "$1" "$out" "$3" >&2
            fail=1
            ;;
    esac
    if [ "$#" -ge 4 ] && [ "$calls" != "$4" ]; then
        printf '%s: calls %s, want %s\n' "$1" "$calls" "$4" >&2
        fail=1
    fi
}
apt='apt-get -o DPkg::Lock::Timeout=120 install -y --no-install-recommends'
lib=libxkbcommon-x11.so.0 pkg=libxkbcommon-x11-0

libs 1000 ''
expect 'a host with every library' 0 '' ''
if [ -n "$out" ]; then
    printf 'a host with every library: it printed %s, want nothing\n' "$out" >&2
    fail=1
fi
libs 0 "$lib libEGL.so.1"
expect 'root installs with apt-get itself' 0 'Installing libegl1 libxkbcommon-x11-0 with apt-get' "$apt libegl1 $pkg;"
libs 1000 "$lib"
expect 'a user installs through sudo -n' 0 "Installing $pkg with apt-get" "sudo -n true;sudo -n env DEBIAN_FRONTEND=noninteractive $apt $pkg;$apt $pkg;"
libs 1000 "$lib" FAKE_SUDO_ASKS=1
expect 'a sudo that asks a password stops the install' 1 "sudo apt-get install -y $pkg" 'sudo -n true;'
libs 0 "$lib" FAKE_APT_FAILS=1
expect 'a failing apt-get stops the install' 1 "apt-get did not install $pkg" "$apt $pkg;"
expect 'a failing apt-get shows its last lines' 1 'E: Unable to locate package'
libs 0 "$lib" FAKE_APT_NOOP=1
expect 'a library still missing after apt-get stops the install' 1 "the host still lacks $lib" "$apt $pkg;"
libs 0 'libnope.so.9'
expect 'a library without a package stops the install' 1 'libnope.so.9, and this script knows no package for it' ''

# put runs place on the release binary $1 into $work/dest.
put() {
    rc=0
    out=$(place "$1" "$work/dest" 2>&1) || rc=$?
}
stub "$work/good" 'echo "pitwall v0.2.13"'
# A release that cannot run here, as a glibc binary on a host with musl.
stub "$work/bad" 'echo "not found" >&2; exit 127'
put "$work/good"
expect 'a release that starts is put in place' 0 'pitwall v0.2.13'
if [ "$("$work/dest/pitwall" --version)" != 'pitwall v0.2.13' ] || [ -e "$work/dest/.pitwall.new" ]; then
    printf 'a release that starts: %s\n' "$(ls -A "$work/dest")" >&2
    fail=1
fi
stub "$work/dest/pitwall" 'echo "pitwall v0.2.12"'
put "$work/bad"
expect 'a release that does not start is not installed' 1 'does not start on this host, so nothing was installed: not found'
if [ "$("$work/dest/pitwall" --version)" != 'pitwall v0.2.12' ] || [ -e "$work/dest/.pitwall.new" ]; then
    printf 'a release that does not start replaced the old pitwall or left its file: %s\n' "$(ls -A "$work/dest")" >&2
    fail=1
fi

# window runs check_window on the pitwall at $1 with the folder $2, when
# given, first on the PATH and $work/home as the home folder.
window() {
    rc=0
    out=$(
        HOME=$work/home
        PATH="${2:+$2:}$base"
        check_window "$1" 2>&1
    ) || rc=$?
}
# why: only sh's own folder, so no pitwall of this machine is on the PATH.
base=$(dirname -- "$(command -v sh)")
if [ -e "$base/pitwall" ] || [ -e "$base/pitwall.exe" ]; then
    printf 'skipped check_window: %s holds a pitwall, which would answer for the host\n' "$base" >&2
else
    mine=$work/home/.local/bin/pitwall
    mkdir -p "$work/home/.local/bin" "$work/ahead" "$work/elsewhere"
    stub "$mine" 'echo "pitwall v0.2.13"'
    window "$mine"
    expect 'the installed pitwall is the one a window runs' 0 ''
    # The pitwall of another architecture, ahead of ~/.local/bin on the PATH.
    stub "$work/ahead/pitwall" 'echo "Exec format error" >&2; exit 126'
    window "$mine" "$work/ahead"
    expect 'a pitwall that does not start ahead of the installed one' 1 "a window runs $work/ahead/pitwall, which is ahead of $mine"
    stub "$work/ahead/pitwall" 'echo "pitwall v0.2.12"'
    window "$mine" "$work/ahead"
    expect 'a pitwall that starts ahead of the installed one' 0 "A window runs $work/ahead/pitwall"
    rm "$mine"
    stub "$work/elsewhere/pitwall" 'echo "pitwall v0.2.13"'
    window "$work/elsewhere/pitwall"
    expect 'a pitwall installed where no window looks' 1 "$work/elsewhere/pitwall is in neither"
fi
exit "$fail"
