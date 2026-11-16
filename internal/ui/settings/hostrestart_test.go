package settings

import (
	"image"
	"path/filepath"
	"testing"
	"time"

	gl "gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/quanticstudios/pitwall/internal/config"
	"github.com/quanticstudios/pitwall/internal/ui/theme"
)

// TestHostsRestartButton lays the Hosts page out as a window does. With a
// host the window does not show, a click on Restart window is the page's
// result; a window that shows every host has no such button to click; and
// the reason a restart failed lays out under the row.
func TestHostsRestartButton(t *testing.T) {
	var p Page
	p.Show(filepath.Join(t.TempDir(), "config.toml"))
	s := config.Settings{Hosts: []config.Host{{Name: "box", Window: true}}}
	var ops op.Ops
	frame := func() Result {
		t.Helper()
		ops.Reset()
		p.cat = catHosts
		gtx := gl.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: gl.Exact(image.Pt(1000, 700)), Now: time.Now()}
		return p.Layout(gtx, theme.Dark(), s, nil)
	}
	if r := frame(); r != None {
		t.Fatalf("the page at rest returned %d", r)
	}
	p.btn("host-restart").Click()
	if r := frame(); r != RestartWindow {
		t.Fatalf("a click on Restart window returned %d, want RestartWindow", r)
	}
	p.RestartErr = "no display"
	if r := frame(); r != None {
		t.Fatalf("the page with a failed restart returned %d", r)
	}

	// The window shows box now: no row, so a click queued on its button
	// reaches nothing.
	p.WindowHosts, p.RestartErr = []WindowHost{{Name: "box", SSH: "box"}}, ""
	p.btn("host-restart").Click()
	if r := frame(); r != None {
		t.Fatalf("a window that shows every host returned %d", r)
	}
}
