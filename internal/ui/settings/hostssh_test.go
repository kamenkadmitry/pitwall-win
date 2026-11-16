package settings

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestCleanSSHDest(t *testing.T) {
	for in, want := range map[string]string{
		"box":              "box",
		"  me@10.0.0.2  ":  "me@10.0.0.2",
		"ssh box":          "box",
		"ssh ssh box":      "box", // paste of a pasted command
		"ssh -p 22 box":    "-p 22 box",
		"":                 "",
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

func TestLastLines(t *testing.T) {
	if got := lastLines("a\nb\nc\nd\n", 2); got != "c\nd" {
		t.Fatalf("lastLines = %q", got)
	}
	if got := lastLines("one", 4); got != "one" {
		t.Fatalf("lastLines = %q", got)
	}
}
