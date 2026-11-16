package settings

import (
	"cmp"
	"fmt"
	"image"
	"slices"
	"strings"
	"sync"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	gl "gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/quanticstudios/pitwall/internal/config"
	"github.com/quanticstudios/pitwall/internal/ui/kit"
	"github.com/quanticstudios/pitwall/internal/ui/theme"
)

// hostsPage is the host editor on Settings, Hosts.
type hostsPage struct {
	open    bool
	old     string // the host being edited, "" for a new one
	window  bool
	name    widget.Editor
	ssh     widget.Editor
	err     string
	confirm bool // Delete was clicked once
	focus   bool // move focus to name this frame
	scroll  bool // scroll the page to the editor

	aliases []string // the ssh config's Host names, for the new-host picker

	mu         sync.Mutex
	gen        int    // bumps per probe/install; a stale result drops
	probe      int    // a probe* const
	probeOut   string // the found binary, or ssh's error line
	installing bool
	installOut string // the installer's last lines, or its failure
	installErr bool
}

// editHost opens the editor on h, or on a new host for nil.
func (p *Page) editHost(h *config.Host) {
	e := &p.ho
	e.open, e.err, e.confirm, e.focus, e.scroll = true, "", false, true, true
	e.window = h == nil // a new host shows in every window by default
	e.old = ""
	e.mu.Lock()
	e.probe, e.probeOut, e.installing, e.installOut, e.installErr = probeIdle, "", false, "", false
	e.gen++
	e.mu.Unlock()
	e.aliases = nil
	if h != nil {
		e.old, e.window = h.Name, h.Window
		e.name.SetText(h.Name)
		e.ssh.SetText(h.SSH)
	} else {
		e.name.SetText("")
		e.ssh.SetText("")
		for _, a := range sshAliasesFile() {
			if !slices.ContainsFunc(p.s.Hosts, func(h config.Host) bool { return h.Name == a || h.SSH == a }) {
				e.aliases = append(e.aliases, a)
			}
		}
	}
}

// saveHost writes the editor's host.
func (p *Page) saveHost() {
	e := &p.ho
	h := config.Host{Name: strings.TrimSpace(e.name.Text()), SSH: cleanSSHDest(e.ssh.Text()), Window: e.window}
	if err := config.SetHost(p.path(), e.old, &h); err != nil {
		e.err = err.Error()
		return
	}
	e.open, p.result = false, Saved
}

// deleteHost removes the editor's host.
func (p *Page) deleteHost() {
	e := &p.ho
	if err := config.SetHost(p.path(), e.old, nil); err != nil {
		e.err = err.Error()
		return
	}
	e.open, p.result = false, Saved
}

// hostDest is the ssh destination the editor's fields name.
func (e *hostsPage) hostDest() string {
	return cmp.Or(cleanSSHDest(e.ssh.Text()), strings.TrimSpace(e.name.Text()))
}

// checkHost probes the editor's host in the background; the form polls
// e.probe for the result.
func (p *Page) checkHost() {
	e := &p.ho
	dest := e.hostDest()
	if dest == "" {
		e.err = "fill in the name or ssh first"
		return
	}
	e.err = ""
	e.mu.Lock()
	e.gen++
	gen := e.gen
	e.probe, e.probeOut, e.installing, e.installOut = probeRunning, "", false, ""
	e.mu.Unlock()
	go func() {
		st, out := probeHost(dest)
		e.mu.Lock()
		if gen == e.gen {
			e.probe, e.probeOut = st, out
		}
		e.mu.Unlock()
	}()
}

// installHost runs get.sh --core on the editor's host in the background,
// then probes again so the result shows right away.
func (p *Page) installHost() {
	e := &p.ho
	dest := e.hostDest()
	if dest == "" || e.installing {
		return
	}
	e.mu.Lock()
	e.gen++
	gen := e.gen
	e.installing, e.installOut, e.installErr = true, "", false
	e.mu.Unlock()
	go func() {
		out, err := installCore(dest)
		tail := lastLines(string(out), 4)
		if err != nil {
			tail = err.Error() + "\n" + tail
		}
		e.mu.Lock()
		if gen == e.gen {
			e.installing, e.installOut, e.installErr = false, tail, err != nil
		}
		e.mu.Unlock()
		if err == nil {
			p.checkHost()
		}
	}()
}

// lastLines is s's last n non-empty lines.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// hosts are the [[hosts]] of Settings, Hosts, or the editor while it is
// open.
func (p *Page) hosts() []section {
	btn := func(id, label string, kind btnKind, click func()) gl.Widget {
		return func(gtx gl.Context) gl.Dimensions {
			c := p.btn(id)
			for c.Clicked(gtx) {
				click()
				gtx.Execute(op.InvalidateCmd{}) // the rows of this frame are already built
			}
			return p.button(gtx, c, kind, label)
		}
	}
	if p.ho.open {
		return []section{p.hostForm(btn)}
	}
	var rows []row
	for _, h := range p.s.Hosts {
		h := h
		desc := cmp.Or(h.SSH, h.Name)
		if h.Window {
			desc += " · in every window"
		}
		rows = append(rows, row{label: h.Name, desc: desc, extra: "host ssh window " + h.Name,
			control: btn("host-edit:"+h.Name, "Edit", secondary, func() { p.editHost(&h) })})
	}
	rows = append(rows, row{label: "New host", desc: "Another machine's pitwall, reached over ssh.",
		extra: "host add new ssh remote machine", control: btn("host-new", "New host", primary, func() { p.editHost(nil) })})
	return []section{{
		title: "Hosts",
		desc:  "pitwall --host <name> opens a window on the host alone; window on also shows its sessions in every window, under its own heading. Windows pick a change up the next time they start.",
		rows:  rows, anchor: true}}
}

// hostForm is the editor's rows.
func (p *Page) hostForm(btn func(id, label string, kind btnKind, click func()) gl.Widget) section {
	th := p.th
	e := &p.ho
	rows := []row{
		{label: "Name", desc: "What pitwall --host takes and the sidebar shows.", extra: "host name",
			control: p.edField(&e.name, "alter-core", 260, p.saveHost, &e.focus)},
		{label: "ssh", desc: "Only the destination: user@box or a Host alias from ~/.ssh/config — not the ssh command itself. Empty uses the name.", extra: "host ssh destination user",
			control: p.edField(&e.ssh, "box or me@192.168.0.5", 320, p.saveHost, nil)},
	}
	if len(e.aliases) > 0 {
		rows = append(rows, row{label: "From ~/.ssh/config", desc: "Machines ssh already knows; picking one fills the name.",
			extra: "host ssh config alias known", control: p.hostPicker})
	}
	rows = append(rows,
		row{label: "In every window", desc: "Shows this host's sessions in every window, under its own heading, connecting in the background.",
			extra: "host window sidebar show", control: p.switchOf("host-window", e.window, func() { e.window = !e.window })},
		row{label: "Saved as", desc: "The [[hosts]] table this writes to config.toml.", extra: "host config toml saved", wide: true,
			control: p.hostToml},
		row{label: "Connection", desc: "Probes ssh to the fields' destination, then checks the host has pitwall on its PATH.",
			extra: "host test check connect install daemon", wide: true,
			control: p.hostCheck(btn)})

	e.mu.Lock()
	saveOK := e.probe == probeDaemon
	e.mu.Unlock()
	saveBtn := btn("host-save", "Save", primary, p.saveHost)
	if !saveOK {
		saveBtn = p.ghostBtn("Save")
	}
	buttons := []gl.Widget{saveBtn, btn("host-cancel", "Cancel", secondary, func() { e.open = false })}
	if e.old != "" {
		if e.confirm {
			buttons = append(buttons, btn("host-delete", "Delete for good", danger, p.deleteHost))
		} else {
			buttons = append(buttons, btn("host-delete", "Delete", secondary, func() { e.confirm = true }))
		}
	}
	rows = append(rows, row{label: "", control: func(gtx gl.Context) gl.Dimensions {
		return gl.Flex{Axis: gl.Vertical}.Layout(gtx,
			gl.Rigid(func(gtx gl.Context) gl.Dimensions { return hstack(gtx, 8, buttons...) }),
			gl.Rigid(func(gtx gl.Context) gl.Dimensions {
				if e.err == "" {
					return gl.Dimensions{}
				}
				return gl.Inset{Top: 10}.Layout(gtx, func(gtx gl.Context) gl.Dimensions {
					return p.para(gtx, th.UIFont, p.th.Sp(theme.Small), th.Red, e.err)
				})
			}),
		)
	}})
	title := "New host"
	if e.old != "" {
		title = "Edit " + e.old
	}
	return section{title: title, desc: "Saved to config.toml as a [[hosts]] table.", rows: rows, anchor: true}
}

// hostToml previews the [[hosts]] table the editor's fields write.
func (p *Page) hostToml(gtx gl.Context) gl.Dimensions {
	th := p.th
	e := &p.ho
	lines := []string{"[[hosts]]", fmt.Sprintf("name = %q", strings.TrimSpace(e.name.Text()))}
	if dest := cleanSSHDest(e.ssh.Text()); dest != "" {
		lines = append(lines, fmt.Sprintf("ssh = %q", dest))
	}
	if e.window {
		lines = append(lines, "window = true")
	}
	return boxed(gtx, th.Surface, th.Border, gtx.Dp(6), image.Pt(gtx.Dp(10), gtx.Dp(8)), func(gtx gl.Context) gl.Dimensions {
		var kids []gl.FlexChild
		for _, l := range lines {
			l := l
			kids = append(kids, gl.Rigid(func(gtx gl.Context) gl.Dimensions {
				return p.text(gtx, th.MonoFont, p.th.Sp(theme.Body), th.Muted, l)
			}))
		}
		return gl.Flex{Axis: gl.Vertical}.Layout(gtx, kids...)
	})
}

// hostCheck is the Check connection button, the Install button when the
// host lacks pitwall, and the probe's result under them.
func (p *Page) hostCheck(btn func(id, label string, kind btnKind, click func()) gl.Widget) gl.Widget {
	th := p.th
	e := &p.ho
	return func(gtx gl.Context) gl.Dimensions {
		e.mu.Lock()
		probe, probeOut := e.probe, e.probeOut
		installing, installOut, installErr := e.installing, e.installOut, e.installErr
		e.mu.Unlock()

		var status string
		statusCol := th.Muted
		var buttons []gl.Widget
		switch probe {
		case probeIdle:
			status = "Check first — Save activates after a green check."
			buttons = append(buttons, btn("host-check", "Check connection", secondary, p.checkHost))
		case probeRunning:
			status = "Checking…"
			gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(200 * time.Millisecond)})
		case probeDaemon:
			status, statusCol = "All good — "+cmp.Or(probeOut, "pitwall is installed")+". Save is active.", th.Green
		case probeNoDaemon:
			status, statusCol = "ssh works, but the host has no pitwall.", th.Yellow
			buttons = append(buttons, btn("host-install", "Install pitwall on the host", primary, p.installHost))
		case probeFail:
			status, statusCol = "ssh failed: "+probeOut, th.Red
			buttons = append(buttons, btn("host-check", "Check connection", secondary, p.checkHost))
		}
		if installing {
			status = "Installing pitwall-core…"
			statusCol = th.Muted
			gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(200 * time.Millisecond)})
		}
		if installOut != "" {
			c := th.Green
			if installErr {
				c = th.Red
				buttons = append(buttons, btn("host-install", "Install pitwall on the host", primary, p.installHost))
			}
			status += "\n" + installOut
			statusCol = c
		}
		return gl.Flex{Axis: gl.Vertical}.Layout(gtx,
			gl.Rigid(func(gtx gl.Context) gl.Dimensions { return hstack(gtx, 8, buttons...) }),
			gl.Rigid(func(gtx gl.Context) gl.Dimensions {
				return gl.Inset{Top: 8}.Layout(gtx, func(gtx gl.Context) gl.Dimensions {
					return p.para(gtx, th.UIFont, p.th.Sp(theme.Small), statusCol, status)
				})
			}),
		)
	}
}

// ghostBtn is a button that only looks like one: Save before a green
// check, drawn at half opacity and taking no clicks.
func (p *Page) ghostBtn(label string) gl.Widget {
	return func(gtx gl.Context) gl.Dimensions {
		return kit.Button(gtx, p.th, p.btn("ghost:"+label), primary.Disabled(), kit.Medium, label)
	}
}

// ddHosts is the host picker's dropdown id: one dropdown is open at a
// time, and p.dd names it.
const ddHosts = "hosts"

// hostPicker is a dropdown of the ssh config's Host names; picking one
// fills the name field.
func (p *Page) hostPicker(gtx gl.Context) gl.Dimensions {
	th := p.th
	e := &p.ho
	c := p.btn("dd:" + ddHosts)
	for c.Clicked(gtx) {
		if p.dd == ddHosts {
			p.dd = ""
		} else {
			p.dd, p.ddFocus, p.ddList.Position = ddHosts, true, gl.Position{}
			p.ddFilter.SetText("")
		}
	}
	w, h := gtx.Dp(240), gtx.Dp(32)
	d := c.Layout(gtx, func(gtx gl.Context) gl.Dimensions {
		border := th.Border
		if c.Hovered() || p.dd == ddHosts {
			border = theme.Mix(th.Border, th.Fg, 0.25)
		}
		if gtx.Focused(c) {
			kit.FocusRing(gtx, th, image.Rect(0, 0, w, h), gtx.Dp(theme.RadiusControl))
		}
		rrect(gtx, border, image.Rect(0, 0, w, h), gtx.Dp(6))
		rrect(gtx, th.SurfaceElevated, image.Rect(1, 1, w-1, h-1), gtx.Dp(6)-1)
		label := "Pick a machine…"
		if cur := strings.TrimSpace(e.name.Text()); cur != "" {
			label = cur
		}
		g := gtx
		g.Constraints.Max.X = w - gtx.Dp(40)
		m := op.Record(gtx.Ops)
		td := p.text(g, th.UIFont, p.th.Sp(theme.Body), th.Fg, label)
		call := m.Stop()
		o := op.Offset(image.Pt(gtx.Dp(10), (h-td.Size.Y)/2)).Push(gtx.Ops)
		call.Add(gtx.Ops)
		o.Pop()
		is := gtx.Dp(14)
		o = op.Offset(image.Pt(w-gtx.Dp(10)-is, (h-is)/2)).Push(gtx.Ops)
		icon(gtx, th.Muted, is, chevronGlyph)
		o.Pop()
		defer clip.Rect{Max: image.Pt(w, h)}.Push(gtx.Ops).Pop()
		pointer.CursorPointer.Add(gtx.Ops)
		return gl.Dimensions{Size: image.Pt(w, h)}
	})
	if p.dd == ddHosts {
		p.hostPopover(gtx, w, h)
	}
	return d
}

// hostPopover draws the open alias dropdown over everything, below the
// picker. A press outside it closes it.
func (p *Page) hostPopover(gtx gl.Context, w, top int) {
	th := p.th
	e := &p.ho
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &p.ddTag, Kinds: pointer.Press})
		if !ok {
			break
		}
		if _, ok := ev.(pointer.Event); ok {
			p.dd = ""
		}
	}
	for {
		_, ok := gtx.Event(pointer.Filter{Target: &p.ddPanel, Kinds: pointer.Press})
		if !ok {
			break
		}
	}
	q := strings.ToLower(strings.TrimSpace(p.ddFilter.Text()))
	var shown []string
	for _, a := range e.aliases {
		if strings.Contains(strings.ToLower(a), q) {
			shown = append(shown, a)
		}
	}
	pick := func(a string) {
		e.name.SetText(a)
		e.ssh.SetText("")
		p.dd = ""
	}
	p.ddFilter.SingleLine, p.ddFilter.Submit = true, true
	for {
		ev, ok := p.ddFilter.Update(gtx)
		if !ok {
			break
		}
		switch ev.(type) {
		case widget.ChangeEvent:
			p.ddList.Position = gl.Position{}
		case widget.SubmitEvent:
			if len(shown) > 0 {
				pick(shown[0])
			}
		}
	}
	for _, a := range shown {
		for p.btn("alias:" + a).Clicked(gtx) {
			pick(a)
		}
	}
	if p.dd == "" {
		return
	}
	if p.ddFocus {
		p.ddFocus = false
		gtx.Execute(key.FocusCmd{Tag: &p.ddFilter})
	}

	m := op.Record(gtx.Ops)
	bg := clip.Rect{Min: image.Pt(-1<<14, -1<<14), Max: image.Pt(1<<14, 1<<14)}.Push(gtx.Ops)
	event.Op(gtx.Ops, &p.ddTag)
	bg.Pop()
	itemH, pad := gtx.Dp(28), gtx.Dp(6)
	listH := min(gtx.Dp(280), max(1, len(shown))*itemH)
	panel := image.Rect(0, 0, w+gtx.Dp(40), pad+gtx.Dp(32)+pad+listH+pad)
	o := op.Offset(image.Pt(0, top+gtx.Dp(4))).Push(gtx.Ops)
	kit.Surface(gtx, panel, gtx.Dp(theme.RadiusPopover), kit.Floating, theme.Mix(th.SurfaceElevated, th.Fg, 0.1), th.SurfaceElevated)
	pa := clip.Rect(panel).Push(gtx.Ops)
	event.Op(gtx.Ops, &p.ddPanel) // the panel takes presses the backdrop would
	pa.Pop()

	fo := op.Offset(image.Pt(pad, pad)).Push(gtx.Ops)
	fg := gtx
	fg.Constraints = gl.Exact(image.Pt(panel.Dx()-2*pad, gtx.Dp(32)))
	p.field(fg, &p.ddFilter, "Filter hosts", true)
	fo.Pop()

	lo := op.Offset(image.Pt(pad, pad+gtx.Dp(32)+pad)).Push(gtx.Ops)
	lg := gtx
	lg.Constraints = gl.Exact(image.Pt(panel.Dx()-2*pad, listH))
	cl := clip.Rect{Max: lg.Constraints.Max}.Push(gtx.Ops)
	if len(shown) == 0 {
		gl.Center.Layout(lg, func(gtx gl.Context) gl.Dimensions {
			return p.text(gtx, th.UIFont, p.th.Sp(theme.Small), th.Muted, "No host matches")
		})
	} else {
		p.ddList.Axis = gl.Vertical
		p.ddList.Layout(lg, len(shown), func(gtx gl.Context, i int) gl.Dimensions {
			a := shown[i]
			c := p.btn("alias:" + a)
			return c.Layout(gtx, func(gtx gl.Context) gl.Dimensions {
				sz := image.Pt(gtx.Constraints.Max.X, itemH)
				if c.Hovered() {
					rrect(gtx, theme.Mix(th.SurfaceElevated, th.Fg, 0.06), image.Rectangle{Max: sz}, gtx.Dp(4))
				}
				g := gtx
				g.Constraints.Max.X = sz.X - gtx.Dp(36)
				mm := op.Record(gtx.Ops)
				d := p.text(g, th.UIFont, p.th.Sp(theme.Body), th.Fg, a)
				call := mm.Stop()
				oo := op.Offset(image.Pt(gtx.Dp(8), (itemH-d.Size.Y)/2)).Push(gtx.Ops)
				call.Add(gtx.Ops)
				oo.Pop()
				if strings.EqualFold(a, strings.TrimSpace(e.name.Text())) {
					is := gtx.Dp(14)
					oo = op.Offset(image.Pt(sz.X-gtx.Dp(8)-is, (itemH-is)/2)).Push(gtx.Ops)
					icon(gtx, th.Primary, is, checkGlyph)
					oo.Pop()
				}
				defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
				pointer.CursorPointer.Add(gtx.Ops)
				return gl.Dimensions{Size: sz}
			})
		})
	}
	cl.Pop()
	lo.Pop()
	o.Pop()
	op.Defer(gtx.Ops, m.Stop())
}
