package settings

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/quanticstudios/pitwall/internal/config"
	"github.com/quanticstudios/pitwall/scripts"
)

func TestCleanSSHDest(t *testing.T) {
	for in, want := range map[string]string{
		"box":                "box",
		"  me@10.0.0.2  ":    "me@10.0.0.2",
		"ssh box":            "box",
		"ssh ssh box":        "box", // paste of a pasted command
		"ssh -p 22 box":      "-p 22 box",
		"":                   "",
		"  ssh  alter-core ": "alter-core",
	} {
		if got := cleanSSHDest(in); got != want {
			t.Errorf("cleanSSHDest(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSSHAliases(t *testing.T) {
	dir := t.TempDir()
	conf := `Host alter-core
    HostName 192.168.0.10

Host nocobasev3 bridge
    HostName 192.168.0.122

Host *
    ServerAliveInterval 30

Host !blocked skipme
    HostName 10.0.0.9

Match all
    HostName ignored.example
`
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
	want := []string{"alter-core", "nocobasev3", "bridge", "skipme"}
	got := sshAliases(path)
	if !slices.Equal(got, want) {
		t.Fatalf("sshAliases = %v, want %v", got, want)
	}
	if got := sshAliases(filepath.Join(dir, "missing")); got != nil {
		t.Fatalf("sshAliases of a missing file = %v, want nil", got)
	}
}

// The probe's answer is its marker line, past whatever the host's shell
// prints first; only ssh's own failure or a host without sh fails it.
func TestProbeResult(t *testing.T) {
	exit := func(code int) error { return exec.Command("sh", "-c", "exit "+strconv.Itoa(code)).Run() }
	for i, c := range []struct {
		out, stderr string
		err         error
		timedOut    bool
		want        int
		wantOut     string
	}{
		{out: "pitwall-probe\t/home/me/.local/bin/pitwall\tok\tyes\tpitwall v0.2.13\n", want: probeDaemon, wantOut: "pitwall v0.2.13 at /home/me/.local/bin/pitwall"},
		{out: "Welcome to box\r\npitwall-probe\t/usr/bin/pitwall\tok\tno\tpitwall v0.2.13\r\n", want: probeDaemon, wantOut: "pitwall v0.2.13 at /usr/bin/pitwall"},
		{out: "motd\npitwall-probe\t\t\tno\t\n", want: probeNoDaemon},
		// A pitwall that is there and does not start is no green check.
		{out: "pitwall-probe\t/home/me/.local/bin/pitwall\tfail\tyes\t/home/me/.local/bin/pitwall: error while loading shared libraries: libxkbcommon-x11.so.0: cannot open shared object file: No such file or directory\n",
			want: probeBroken, wantOut: "/home/me/.local/bin/pitwall does not start: error while loading shared libraries: libxkbcommon-x11.so.0: cannot open shared object file: No such file or directory" +
				". This pitwall release links the window's libraries: Install adds them on a host with apt-get; elsewhere install the one named with the host's package manager, then check again"},
		{out: "pitwall-probe\t/home/me/.local/bin/pitwall\tfail\tyes\tExec format error\n", want: probeBroken, wantOut: "/home/me/.local/bin/pitwall does not start: Exec format error"},
		// One ahead of ~/.local/bin on the PATH is not Install's to replace.
		{out: "pitwall-probe\t/usr/local/bin/pitwall\tfail\tno\tExec format error\n", want: probeShadowed,
			wantOut: "/usr/local/bin/pitwall does not start: Exec format error. A window runs this one, not ~/.local/bin/pitwall, which Install writes, so Install cannot replace it: remove or repair it on the host, then check again"},
		{out: "pitwall-probe\t/usr/bin/pitwall\tfail\tno\t/usr/bin/pitwall: error while loading shared libraries: libEGL.so.1: cannot open shared object file\n", want: probeShadowed,
			wantOut: "/usr/bin/pitwall does not start: error while loading shared libraries: libEGL.so.1: cannot open shared object file. A window runs this one, not ~/.local/bin/pitwall, which Install writes, so Install cannot replace it: install the library named with the host's package manager, or remove this pitwall there, then check again"},
		{out: "pitwall-probe\t/usr/bin/pitwall\tok\tpitwall v0.2.13\n", want: probeFail, wantOut: `the host's answer has 3 fields, not 4: "/usr/bin/pitwall\tok\tpitwall v0.2.13"`},
		{out: "motd\n", want: probeFail, wantOut: `the host ran no sh: it printed "motd"`},
		{stderr: "kex_exchange_identification: read: Connection reset by peer\nssh: connect to host box port 22: Connection refused\n", err: exit(255),
			want: probeFail, wantOut: "ssh: connect to host box port 22: Connection refused"},
		{stderr: "Host key verification failed.\n", err: exit(255),
			want: probeFail, wantOut: "Host key verification failed. Run ssh me@box in a terminal once to accept the host's key, then check again"},
		{stderr: "me@box: Permission denied (publickey,password).\n", err: exit(255),
			want: probeFail, wantOut: "me@box: Permission denied (publickey,password). The check cannot type a password or passphrase: set up key login, or load the key into ssh-agent, then check again"},
		// A host whose shell has no sh, as cmd.exe on Windows: not "no pitwall".
		{stderr: "'sh' is not recognized as an internal or external command,\noperable program or batch file.\n", err: exit(1),
			want: probeFail, wantOut: "operable program or batch file"},
		{err: exit(3), want: probeFail, wantOut: "exit status 3"},
		{out: "pitwall-probe\t/usr/bin/pitwall\tok\tpitwall v0.2.13\n", err: exit(255), timedOut: true, want: probeFail, wantOut: "no answer in 15s"},
	} {
		if got, out := probeResult(c.out, c.stderr, c.err, c.timedOut, "me@box"); got != c.want || out != c.wantOut {
			t.Errorf("%d: probeResult = %d, %q, want %d, %q", i, got, out, c.want, c.wantOut)
		}
	}
}

// hostShell is how the tests stand in for a host: sh's folder alone as the
// PATH, so no pitwall, curl or tar of this machine answers for the host's.
func hostShell(t *testing.T) (dir string) {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Fatalf("the test runs the remote commands with sh: %v", err)
	}
	dir = filepath.Dir(sh)
	for _, name := range []string{"pitwall", "pitwall.exe"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Skipf("%s holds a pitwall, which would answer for the host's", dir)
		}
	}
	return dir
}

// asHost runs command as a host's login shell does once ssh hands it over,
// with stdin as ssh passes it on, home as $HOME and path as its PATH.
func asHost(command, stdin, home string, path []string, env ...string) (string, error) {
	cmd := exec.Command("sh", "-c", command)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = append(append(os.Environ(), "HOME="+home, "PATH="+strings.Join(path, string(os.PathListSeparator))), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// script writes an executable sh script.
func script(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// The probe finds a pitwall in ~/.local/bin, where get.sh installs and
// which a non-interactive ssh login's PATH lacks, runs it, and reports a
// host without one by an empty path, never by its exit status.
func TestProbeCommand(t *testing.T) {
	shDir := hostShell(t)
	home := t.TempDir()
	out, err := asHost(probeCommand, "", home, []string{shDir})
	if st, _ := probeResult(out, "", err, false, "box"); st != probeNoDaemon {
		t.Fatalf("no pitwall: probe printed %q, %v, read as %d, want probeNoDaemon", out, err, st)
	}
	bin := filepath.Join(home, ".local", "bin", "pitwall")
	script(t, bin, `[ "$1" = --version ] && echo "pitwall v0.2.13"`)
	out, err = asHost("echo motd; "+probeCommand, "", home, []string{shDir})
	st, msg := probeResult(out, "", err, false, "box")
	if st != probeDaemon || !strings.HasPrefix(msg, "pitwall v0.2.13 at ") || !strings.HasSuffix(msg, "/.local/bin/pitwall") {
		t.Fatalf("pitwall in ~/.local/bin: probe printed %q, %v, read as %d, %q", out, err, st, msg)
	}
	// A pitwall that cannot start, as one whose libraries the host lacks.
	script(t, bin, "printf 'pitwall: error while loading shared libraries: libxkbcommon-x11.so.0:\\tno such file\\nsecond line\\n' >&2; exit 127")
	out, err = asHost(probeCommand, "", home, []string{shDir})
	st, msg = probeResult(out, "", err, false, "box")
	if st != probeBroken || !strings.Contains(msg, "does not start: pitwall: error while loading shared libraries: libxkbcommon-x11.so.0: no such file second line") {
		t.Fatalf("pitwall that does not start: probe printed %q, %v, read as %d, %q", out, err, st, msg)
	}
	// The reviewer's case: a pitwall that does not start sits on the PATH
	// ahead of a good one in ~/.local/bin. A window runs the first, so the
	// probe reports that one, and as not Install's to replace.
	script(t, bin, `[ "$1" = --version ] && echo "pitwall v0.2.13"`)
	ahead := t.TempDir()
	script(t, filepath.Join(ahead, "pitwall"), "echo 'Exec format error' >&2; exit 126")
	out, err = asHost(probeCommand, "", home, []string{ahead, shDir})
	st, msg = probeResult(out, "", err, false, "box")
	if st != probeShadowed || !strings.Contains(msg, "/pitwall does not start: Exec format error. A window runs this one, not ~/.local/bin/pitwall") {
		t.Fatalf("a broken pitwall ahead of ~/.local/bin: probe printed %q, %v, read as %d, %q", out, err, st, msg)
	}
	if v := viewOf(hostCheckState{probe: st, probeOut: msg}); v.canInstall || !v.canCheck {
		t.Fatalf("a broken pitwall ahead of ~/.local/bin offers Install: %+v", v)
	}
	// A window runs a pitwall-core in preference to any pitwall, so the
	// probe reports that one: a good one beats a broken pitwall on the
	// PATH, and a broken one is not Install's to replace either.
	core := filepath.Join(home, ".local", "bin", "pitwall-core")
	script(t, core, `[ "$1" = --version ] && echo "pitwall-core v0.2.14"`)
	out, err = asHost(probeCommand, "", home, []string{ahead, shDir})
	st, msg = probeResult(out, "", err, false, "box")
	if st != probeDaemon || !strings.HasPrefix(msg, "pitwall-core v0.2.14 at ") || !strings.HasSuffix(msg, "/.local/bin/pitwall-core") {
		t.Fatalf("a pitwall-core next to a pitwall: probe printed %q, %v, read as %d, %q", out, err, st, msg)
	}
	script(t, core, "echo 'Exec format error' >&2; exit 126")
	out, err = asHost(probeCommand, "", home, []string{shDir})
	st, msg = probeResult(out, "", err, false, "box")
	if st != probeShadowed || !strings.Contains(msg, "/pitwall-core does not start: Exec format error. A window runs this one") {
		t.Fatalf("a broken pitwall-core next to a good pitwall: probe printed %q, %v, read as %d, %q", out, err, st, msg)
	}
}

// Install feeds this build's get-host.sh to sh on the host, pinned to the
// upstream release the build came from; the script's exit status is the
// install's.
func TestInstallCommand(t *testing.T) {
	if got, want := installCommand("v0.2.13-win.1"), `sh -c 'PITWALL_VERSION=v0.2.13 exec sh -s'`; got != want {
		t.Fatalf("installCommand of a -win release = %s, want %s", got, want)
	}
	if got, want := installCommand("dev-0123456789ab"), `sh -c 'exec sh -s'`; got != want {
		t.Fatalf("installCommand of a dev build = %s, want %s", got, want)
	}

	shDir := hostShell(t)
	home := t.TempDir()
	says := `echo "get-host.sh installs ${PITWALL_VERSION:-latest}"`
	for ver, want := range map[string]string{"v0.2.13-win.1": "get-host.sh installs v0.2.13\n", "v0.2.13": "get-host.sh installs v0.2.13\n", "dev": "get-host.sh installs latest\n"} {
		if out, err := asHost(installCommand(ver), says, home, []string{shDir}); err != nil || out != want {
			t.Errorf("install from %s printed %q, %v, want %q", ver, out, err, want)
		}
	}
	out, err := asHost(installCommand("v0.2.13"), "echo 'pitwall: could not download it' >&2; exit 1", home, []string{shDir})
	if err == nil || out != "pitwall: could not download it\n" {
		t.Errorf("a failing script: %q, %v, want its message and its failure", out, err)
	}
}

// The script the binary carries is the repository's, with the line ends sh
// reads, and sh takes all of it from stdin: PITWALL_GET_TEST stops it
// before main, so nothing installs.
func TestGetHostScript(t *testing.T) {
	if !strings.HasPrefix(scripts.GetHost, "#!/bin/sh\n") || strings.Contains(scripts.GetHost, "\r") {
		t.Fatalf("get-host.sh is embedded with CRLF line ends or without its first line: %q", scripts.GetHost[:min(40, len(scripts.GetHost))])
	}
	shDir := hostShell(t)
	if out, err := asHost(installCommand("v0.2.13"), scripts.GetHost, t.TempDir(), []string{shDir}, "PITWALL_GET_TEST=1"); err != nil || out != "" {
		t.Fatalf("sh on get-host.sh from stdin: %q, %v", out, err)
	}
}

// A result belongs to the destination it was made for: after an edit of
// the fields the form is back to "check first".
func TestHostCheckState(t *testing.T) {
	e := &hostsPage{probeDest: "box", probe: probeDaemon, probeOut: "/usr/bin/pitwall"}
	if got := e.check("box"); got.probe != probeDaemon || got.probeOut != "/usr/bin/pitwall" {
		t.Fatalf("check of the probed destination = %+v", got)
	}
	if got := e.check("box2"); got != (hostCheckState{}) {
		t.Fatalf("check of another destination = %+v, want none", got)
	}
	e.old, e.oldDest = "box", "box"
	if !e.check("box").saved || e.check("box2").saved {
		t.Fatalf("saved: %+v for the saved destination, %+v for another", e.check("box"), e.check("box2"))
	}
}

// The row offers Check after every result and Install only on a host
// without pitwall, once; while something runs it offers neither.
func TestHostCheckView(t *testing.T) {
	for i, c := range []struct {
		s                         hostCheckState
		check, install, busy      bool
		tone                      int
		wantInstall, wantInStatus string
	}{
		{s: hostCheckState{}, check: true, wantInStatus: "Check first"},
		// A saved host with its destination kept: Save is on, and the row says why.
		{s: hostCheckState{saved: true}, check: true, wantInStatus: "Save needs no check"},
		{s: hostCheckState{probe: probeRunning}, busy: true, wantInStatus: "Checking"},
		{s: hostCheckState{probe: probeDaemon, probeOut: "/usr/bin/pitwall"}, check: true, tone: toneGreen, wantInStatus: "/usr/bin/pitwall"},
		{s: hostCheckState{probe: probeNoDaemon}, check: true, install: true, tone: toneYellow, wantInStatus: "no pitwall"},
		{s: hostCheckState{probe: probeFail, probeOut: "Connection refused"}, check: true, tone: toneRed, wantInStatus: "Check failed: Connection refused"},
		{s: hostCheckState{probe: probeShadowed, probeOut: "/usr/bin/pitwall does not start: Exec format error. It is ahead"}, check: true, tone: toneRed,
			wantInStatus: "ssh works, but /usr/bin/pitwall does not start: Exec format error. It is ahead."},
		{s: hostCheckState{probe: probeBroken, probeOut: "/usr/bin/pitwall does not start: Exec format error"}, check: true, install: true, tone: toneRed,
			wantInStatus: "ssh works, but /usr/bin/pitwall does not start: Exec format error."},
		{s: hostCheckState{probe: probeNoDaemon, installing: true}, busy: true, wantInStatus: "Installing"},
		// A failed install keeps the yellow result and its one Install button.
		{s: hostCheckState{probe: probeNoDaemon, installOut: "exit status 22\ncurl: (22) 404", installErr: true}, check: true, install: true, tone: toneYellow,
			wantInstall: "exit status 22\ncurl: (22) 404", wantInStatus: "no pitwall"},
		{s: hostCheckState{probe: probeDaemon, probeOut: "/home/me/.local/bin/pitwall", installOut: "Installed pitwall v0.2.13"}, check: true, tone: toneGreen,
			wantInstall: "Installed pitwall v0.2.13", wantInStatus: "Save is active"},
	} {
		v := viewOf(c.s)
		if v.canCheck != c.check || v.canInstall != c.install || v.busy != c.busy || v.tone != c.tone ||
			v.install != c.wantInstall || v.installFail != c.s.installErr || !strings.Contains(v.status, c.wantInStatus) {
			t.Errorf("%d: viewOf(%+v) = %+v", i, c.s, v)
		}
	}
}

// Save needs a green check of the destination the fields name; a saved
// host whose destination the edit left alone needs none. Enter in a field
// holds the same gate.
func TestHostSaveGate(t *testing.T) {
	p := &Page{}
	e := &p.ho
	e.name.SetText("box")
	if p.canSave() {
		t.Fatal("a new host saves without a check")
	}
	p.saveHost()
	if !strings.Contains(e.err, "check the connection first") {
		t.Fatalf("saveHost without a check: err = %q", e.err)
	}
	e.probeDest, e.probe = "box", probeDaemon
	if !p.canSave() {
		t.Fatal("a green check does not open Save")
	}
	e.ssh.SetText("me@other")
	if p.canSave() {
		t.Fatal("a check of box opens Save for me@other")
	}

	e.old, e.oldDest = "box", "me@box"
	e.ssh.SetText("me@box")
	if !p.canSave() {
		t.Fatal("a saved host with its destination kept needs a check")
	}
	e.ssh.SetText("me@moved")
	if p.canSave() {
		t.Fatal("a saved host with a new destination saves without a check")
	}
}

func TestLastLines(t *testing.T) {
	if got := lastLines("a\nb\nc\nd\n", 2); got != "c\nd" {
		t.Fatalf("lastLines = %q", got)
	}
	if got := lastLines("one", 4); got != "one" {
		t.Fatalf("lastLines = %q", got)
	}
}

// A window reads its hosts when it starts. Hosts says how they differ from
// [[hosts]] now and offers a restart, except in a pitwall --host window.
func TestWindowHostsChange(t *testing.T) {
	cfg := []config.Host{
		{Name: "alter-core", SSH: "alter-core", Window: true},
		{Name: "nocobase", Window: true},
		{Name: "quiet", SSH: "me@quiet"}, // no window = true: in no window
	}
	for i, c := range []struct {
		shown []WindowHost
		want  string
	}{
		{[]WindowHost{{"alter-core", "alter-core"}, {"nocobase", "nocobase"}}, ""},
		{[]WindowHost{{"alter-core", "alter-core"}}, "Not in this window yet: nocobase."},
		{nil, "Not in this window yet: alter-core, nocobase."},
		{[]WindowHost{{"alter-core", "alter-core"}, {"nocobase", "nocobase"}, {"gone", "gone"}}, "Still in this window: gone."},
		{[]WindowHost{{"alter-core", "me@old"}, {"nocobase", "nocobase"}}, "On its old ssh destination in this window: alter-core."},
		{[]WindowHost{{"alter-core", "me@old"}, {"quiet", "me@quiet"}},
			"Not in this window yet: nocobase. Still in this window: quiet. On its old ssh destination in this window: alter-core."},
	} {
		if got := windowHostsChange(cfg, c.shown); got != c.want {
			t.Errorf("%d: windowHostsChange = %q, want %q", i, got, c.want)
		}
	}
	if got := windowHostsChange(nil, nil); got != "" {
		t.Errorf("no hosts at all: %q", got)
	}

	restartRow := func(p *Page) bool {
		for _, sec := range p.hosts() {
			for _, r := range sec.rows {
				if r.label == "Restart this window" {
					return strings.Contains(r.desc, "Not in this window yet: nocobase.")
				}
			}
		}
		return false
	}
	p := &Page{WindowHosts: []WindowHost{{"alter-core", "alter-core"}}}
	p.s.Hosts = cfg
	if !restartRow(p) {
		t.Error("Hosts offers no restart for a host this window does not show")
	}
	p.WindowHosts = append(p.WindowHosts, WindowHost{"nocobase", "nocobase"})
	if restartRow(p) {
		t.Error("Hosts offers a restart to a window that shows every host")
	}
	p.WindowHosts, p.OneHost = nil, true
	if restartRow(p) {
		t.Error("Hosts offers a restart in a pitwall --host window")
	}
}
