// Package scripts holds the installer a pitwall binary carries with it.
package scripts

import _ "embed"

// GetHost is get-host.sh, which installs pitwall on a Linux host with the
// libraries it links. Install on Settings, Hosts feeds it to sh on the host
// over ssh, so the host needs no route to this repository and runs the
// script of this build.
//
//go:embed get-host.sh
var GetHost string
