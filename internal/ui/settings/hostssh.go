package settings

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/quanticstudios/pitwall/internal/nowindow"
	"github.com/quanticstudios/pitwall/internal/update"
	"github.com/quanticstudios/pitwall/scripts"
)

// What a connection check on a [[hosts]] entry finds.
const (
	probeIdle = iota
	probeRunning
	probeDaemon   // reachable, and the pitwall a window would run starts
	probeNoDaemon // reachable, but no pitwall there
	probeBroken   // reachable, and its pitwall, the one Install replaces, does not start
	probeShadowed // reachable, and a pitwall ahead of ~/.local/bin on the PATH does not start
	probeFail     // ssh failed, or the host ran no sh
)

// probeMark starts the line the probe prints, so whatever the host's shell
// startup files print is not taken for its answer.
const probeMark = "pitwall-probe"

// probeCommand prints probeMark and four tab-separated fields: pitwall's
// path on the host, empty when it has none; ok or fail, whether pitwall
// --version ran; yes or no, whether that path is ~/.local/bin/pitwall, the
// file Install writes; and what --version printed, on one line. It looks
// where a window's remote-start does (cmd/pitwall's startCommand): the PATH
// and then ~/.local/bin, where get-host.sh installs and which the PATH of
// a non-interactive ssh login often lacks. It runs the binary because a
// file there proves little: a release built with the window does not start
// on a host without the window's libraries. It always exits 0, so another
// exit status is the host's or ssh's failure, never "no pitwall": sh's
// command -v exits 1 in bash and 127 in dash.
const probeCommand = `sh -c 'PATH="$PATH:$HOME/.local/bin"; p=$(command -v pitwall); r=; v=; o=no; ` +
	`if [ "$p" = "$HOME/.local/bin/pitwall" ]; then o=yes; fi; ` +
	`if [ -n "$p" ]; then v=$("$p" --version 2>&1) && r=ok || r=fail; fi; ` +
	`printf "` + probeMark + `\t%s\t%s\t%s\t%s\n" "$p" "$r" "$o" "$(printf "%s" "$v" | tr "\n\t" "  ")"'`

const (
	probeTimeout   = 15 * time.Second
	installTimeout = 3 * time.Minute
)

// cleanSSHDest is the ssh field's destination: a bare alias or user@host,
// tolerating a pasted "ssh " command prefix.
func cleanSSHDest(s string) string {
	s = strings.TrimSpace(s)
	for strings.HasPrefix(s, "ssh ") {
		s = strings.TrimSpace(strings.TrimPrefix(s, "ssh "))
	}
	return s
}

// sshAliases are the Host names of the user's ssh config at path,
// skipping wildcard and negated patterns.
func sshAliases(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []string
	for line := range strings.Lines(string(data)) {
		f := strings.Fields(line)
		if len(f) < 2 || !strings.EqualFold(f[0], "host") {
			continue
		}
		for _, name := range f[1:] {
			if strings.ContainsAny(name, "*?") || strings.HasPrefix(name, "!") {
				continue
			}
			out = append(out, name)
		}
	}
	return out
}

// sshAliasesFile is the user's ssh config's Host names.
func sshAliasesFile() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return sshAliases(filepath.Join(home, ".ssh", "config"))
}

// probeHost checks dest over ssh: is it reachable, and does it have
// pitwall where a window would run it. BatchMode keeps ssh from asking for
// anything: a settings page cannot answer.
func probeHost(dest string) (int, string) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=8",
		"--", dest, probeCommand)
	nowindow.Set(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	st, msg := probeResult(string(out), stderr.String(), err, ctx.Err() != nil, dest)
	if st == probeFail {
		log.Printf("host check of %s failed: %s (ssh: %v, stderr %q)", dest, msg, err, strings.TrimSpace(stderr.String()))
	}
	return st, msg
}

// probeResult reads a finished probe: ssh's stdout, stderr and error, and
// whether it ran out of time. It returns a probe* const with the host's
// pitwall, its version and path, or with why the check failed.
func probeResult(out, stderr string, err error, timedOut bool, dest string) (int, string) {
	switch {
	case timedOut:
		return probeFail, fmt.Sprintf("no answer in %s", probeTimeout)
	case err != nil:
		return probeFail, sshFailure(stderr, err, dest)
	}
	for line := range strings.Lines(out) {
		rest, ok := strings.CutPrefix(strings.TrimRight(line, "\r\n"), probeMark+"\t")
		if !ok {
			continue
		}
		f := strings.Split(rest, "\t")
		if len(f) != 4 {
			return probeFail, fmt.Sprintf("the host's answer has %d fields, not 4: %q", len(f), rest)
		}
		path, ran, installs, said := strings.TrimSpace(f[0]), f[1], f[2] == "yes", strings.TrimSpace(f[3])
		switch {
		case path == "":
			return probeNoDaemon, ""
		case ran == "ok":
			return probeDaemon, said + " at " + path
		}
		// The loader starts its message with the path too.
		msg := fmt.Sprintf("%s does not start: %s", path, strings.TrimPrefix(said, path+": "))
		libs := strings.Contains(said, "error while loading shared libraries")
		if !installs {
			// why: a window runs the first pitwall on the PATH, so a new one
			// in ~/.local/bin would stay behind this one and change nothing.
			msg += ". It is ahead of ~/.local/bin on the host's PATH, so Install cannot replace it: "
			if libs {
				msg += "install the library named with the host's package manager, or remove this pitwall there, then check again"
			} else {
				msg += "remove or repair it on the host, then check again"
			}
			return probeShadowed, msg
		}
		if libs {
			// why: the release with the window links EGL, Wayland, X11 and
			// xkbcommon, which a host without a desktop often lacks.
			msg += ". This pitwall release links the window's libraries: Install adds them on a host with apt-get; elsewhere install the one named with the host's package manager, then check again"
		}
		return probeBroken, msg
	}
	return probeFail, fmt.Sprintf("the host ran no sh: it printed %q", strings.TrimSpace(out))
}

// sshFailure is why ssh to dest failed: its last stderr line, else its
// error, and for the two failures only the user can clear, how.
func sshFailure(stderr string, err error, dest string) string {
	msg := lastLines(stderr, 1)
	if msg == "" {
		msg = err.Error()
	}
	// ssh ends some of its lines with a full stop; the hints add their own.
	msg = strings.TrimSuffix(msg, ".")
	switch {
	case strings.Contains(stderr, "Host key verification failed"):
		// why: BatchMode refuses a host whose key ssh has not stored.
		msg += fmt.Sprintf(". Run ssh %s in a terminal once to accept the host's key, then check again", dest)
	case strings.Contains(stderr, "Permission denied"):
		msg += ". The check cannot type a password or passphrase: set up key login, or load the key into ssh-agent, then check again"
	}
	return msg
}

// installCommand is what Install runs on a host: sh, reading get-host.sh
// from ssh's stdin, pinned to the upstream release this pitwall of version
// ver was made from, or the latest for a build that is no release. The
// outer sh -c keeps it the same under any login shell. The script installs
// pitwall, not pitwall-core: remote-start runs pitwall, and v0.2.13, the
// release a v0.2.13-win build pins, has no pitwall-core.
func installCommand(ver string) string {
	pin := ""
	if v := update.HostRelease(ver); v != "" {
		pin = "PITWALL_VERSION=" + v + " "
	}
	return `sh -c '` + pin + `exec sh -s'`
}

// installPitwall runs this build's get-host.sh on dest and returns its
// output. The script goes over ssh's stdin: the host fetches the release
// from GitHub, and nothing else.
func installPitwall(dest, ver string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10",
		"--", dest, installCommand(ver))
	nowindow.Set(cmd)
	cmd.Stdin = strings.NewReader(scripts.GetHost)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		err = fmt.Errorf("no answer in %s", installTimeout)
	}
	if err != nil {
		log.Printf("install of pitwall on %s failed: %v; it printed %q", dest, err, lastLines(string(out), 8))
	}
	return string(out), err
}
