package app

import (
	"image"
	"testing"
	"time"

	gl "gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/quanticstudios/pitwall/internal/model"
	"github.com/quanticstudios/pitwall/internal/vt"
)

// screenBackend is a FakeBackend whose panes all show screen.
type screenBackend struct {
	*FakeBackend
	screen string
}

func (b *screenBackend) Frame(pane string) (vt.Grid, vt.Modes, bool) {
	_, m, ok := b.FakeBackend.Frame(pane)
	e := vt.New(100, 30, nil)
	e.Write([]byte(b.screen))
	return e.Snapshot(), m, ok
}

// A host's older daemon marks a Claude Code pane that shows its list of
// sessions to resume as sending no hooks. The window offers no install
// there, and offers it once the pane shows something else.
func TestHooksNoticeWaitsForSessionPick(t *testing.T) {
	defer func(f func(bool, bool) (string, error)) { InstallHooks = f }(InstallHooks)
	InstallHooks = func(bool, bool) (string, error) { return "", nil }
	u, _ := keyWindow(t, conventional)
	fake := u.b.(*FakeBackend)
	fake.mu.Lock()
	for i := range fake.st.Panes { // the fake marks a pane of its own
		fake.st.Panes[i].HooksMissing = i == 0
	}
	fake.st.Panes[0].Provider = model.ProviderClaude
	fake.mu.Unlock()
	b := &screenBackend{FakeBackend: fake, screen: "  Resume session\r\n  ╭────────╮\r\n  │ ⌕ Search… │\r\n  ╰────────╯\r\n  ❯ Fix the login test\r\n    Type to search · Esc to cancel"}
	u.b = b
	frame := func() {
		var ops op.Ops
		u.layout(gl.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: gl.Exact(image.Pt(1280, 800)), Now: time.Now()})
	}
	u.hooks.shownAt = time.Time{}
	frame()
	if !u.hooks.shownAt.IsZero() {
		t.Fatal("the notice showed over the list of sessions to resume")
	}
	b.screen = "❯ "
	frame()
	if u.hooks.shownAt.IsZero() {
		t.Fatal("no notice once a session was picked and still no hook came")
	}
}
