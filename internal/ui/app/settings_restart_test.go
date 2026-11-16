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
