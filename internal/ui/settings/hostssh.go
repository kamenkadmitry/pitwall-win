package settings

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/quanticstudios/pitwall/internal/nowindow"
)

// What a connection check on a [[hosts]] entry finds.
const (
	probeIdle = iota
	probeRunning
	probeDaemon   // reachable, and pitwall or pitwall-core is on its PATH
	probeNoDaemon // reachable, but no pitwall there
	probeFail     // ssh itself failed
)

// getScript installs pitwall-core, the windowless build a host needs, from
// the same script remote-start's install hint uses.
const getScript = "https://raw.githubusercontent.com/quanticstudios/pitwall/main/scripts/get.sh"

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
// pitwall or pitwall-core on its login PATH. BatchMode keeps ssh from
// asking for anything: a settings page cannot answer.
func probeHost(dest string) (int, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=8",
		"--", dest, `command -v pitwall-core || command -v pitwall`)
	nowindow.Set(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	switch {
	case ctx.Err() != nil:
		return probeFail, fmt.Sprintf("no answer in %s", 15*time.Second)
	case err == nil:
		return probeDaemon, strings.TrimSpace(string(out))
	}
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
		return probeNoDaemon, ""
	}
	msg := strings.TrimSpace(stderr.String())
	if i := strings.LastIndexByte(msg, '\n'); i >= 0 {
		msg = msg[i+1:]
	}
	if msg == "" {
		msg = err.Error()
	}
	return probeFail, msg
}

// installCore runs get.sh --core on dest and returns its output.
func installCore(dest string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10",
		"--", dest, "curl -fsSL "+getScript+" | sh -s -- --core")
	nowindow.Set(cmd)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return string(out), fmt.Errorf("no answer in %s", 3*time.Minute)
	}
	return string(out), err
}
