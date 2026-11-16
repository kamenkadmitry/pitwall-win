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
exit "$fail"
