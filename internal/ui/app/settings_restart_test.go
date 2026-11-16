package app

import (
	"errors"
	"slices"
	"testing"

	"github.com/quanticstudios/pitwall/internal/model"
	"github.com/quanticstudios/pitwall/internal/ui/settings"
	"github.com/quanticstudios/pitwall/internal/ui/sidebar"
)

// Restart window on Settings, Hosts reopens the window on its session and
// tab and closes this one; when it cannot, the window stays and says why.
func TestRestartWindow(t *testing.T) {
	st := &model.State{Sessions: []model.Session{{ID: "s1", Name: "work"}}}
	u := &ui{}
	u.nav.session, u.nav.workspace = "s1", "tab7"
	t.Cleanup(func() { Relaunch = nil })

	Relaunch = nil
	if err := u.restartWindow(st); err == nil || u.relaunched {
		t.Fatalf("without Relaunch: %v, relaunched %v", err, u.relaunched)
	}
	var to []string
	Relaunch = func(session, ws string) error {
		to = append(to, session+"/"+ws)
		return errors.New("no display")
	}
	if err := u.restartWindow(st); err == nil || err.Error() != "no display" || u.relaunched {
		t.Fatalf("a relaunch that fails: %v, relaunched %v", err, u.relaunched)
	}
	Relaunch = func(session, ws string) error { to = append(to, session+"/"+ws); return nil }
	if err := u.restartWindow(st); err != nil || !u.relaunched {
		t.Fatalf("a relaunch that works: %v, relaunched %v", err, u.relaunched)
	}
	if want := []string{"work/tab7", "work/tab7"}; !slices.Equal(to, want) {
		t.Fatalf("relaunched on %v, want %v", to, want)
	}

	u = &ui{}
	u.nav.session = "gone"
	if err := u.restartWindow(st); err == nil || u.relaunched {
		t.Fatalf("a session the state lacks: %v, relaunched %v", err, u.relaunched)
	}
}

// The settings page gets the window's hosts without this machine.
func TestWindowHosts(t *testing.T) {
	got := windowHosts([]sidebar.HostInfo{{Name: ""}, {Name: "alter-core", SSH: "alter-core"}, {Name: "box", SSH: "me@box"}})
	want := []settings.WindowHost{{Name: "alter-core", SSH: "alter-core"}, {Name: "box", SSH: "me@box"}}
	if !slices.Equal(got, want) {
		t.Fatalf("windowHosts = %v, want %v", got, want)
	}
	if got := windowHosts([]sidebar.HostInfo{{Name: ""}}); got != nil {
		t.Fatalf("a window with this machine alone: %v", got)
	}
}

// From a session that is on another host alone, the window restarts on a
// session of this machine: this machine's daemon would make a session of
// the host's name, with a shell in it, were it asked for one.
func TestRestartWindowFromHostSession(t *testing.T) {
	b := hostFake{NewFakeBackend()} // calm-heron (s2) is on box
	st := b.State()
	u := &ui{b: b}
	var to []string
	Relaunch = func(session, ws string) error { to = append(to, session+"/"+ws); return nil }
	t.Cleanup(func() { Relaunch = nil })

	// Shown: box's session. This machine has swift-otter (s1), used most
	// recently, and brave-lynx (s3).
	u.nav.session, u.nav.workspace = "s2", "box-tab"
	var s1tab string
	for _, w := range st.Workspaces {
		if w.SessionID == "s1" {
			s1tab = w.ID
			break
		}
	}
	if s1tab == "" {
		t.Fatal("the fake has no tab in s1")
	}
	if err := u.restartWindow(&st); err != nil || !u.relaunched {
		t.Fatalf("restart from a host's session: %v, relaunched %v", err, u.relaunched)
	}
	if want := "swift-otter/" + s1tab; len(to) != 1 || to[0] != want {
		t.Fatalf("reopened on %v, want %s: never on calm-heron, which is box's", to, want)
	}

	// Shown: a session of this machine. It reopens on that one and its tab.
	u, to = &ui{b: b}, nil
	u.nav.session, u.nav.workspace = "s1", s1tab
	if err := u.restartWindow(&st); err != nil || len(to) != 1 || to[0] != "swift-otter/"+s1tab {
		t.Fatalf("restart from this machine's session: %v, reopened on %v", err, to)
	}

	// No session of this machine has a tab: nothing to reopen on, and the
	// window stays.
	only := st
	only.Sessions = []model.Session{{ID: "s2", Name: "calm-heron"}}
	u, to = &ui{b: b}, nil
	u.nav.session = "s2"
	if err := u.restartWindow(&only); err == nil || u.relaunched || len(to) != 0 {
		t.Fatalf("no session of this machine: %v, relaunched %v, reopened on %v", err, u.relaunched, to)
	}
}
