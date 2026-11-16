package settings

import (
	"cmp"
	"fmt"
	"image"
	"slices"
	"strings"
	"sync"
	"time"

	gl "gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/quanticstudios/pitwall/internal/config"
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
	e.window = false
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
		chips := make([]gl.Widget, 0, len(e.aliases))
		for _, a := range e.aliases {
			a := a
			chips = append(chips, btn("host-alias:"+a, a, secondary, func() {
				e.name.SetText(a)
				e.ssh.SetText("")
			}))
		}
		rows = append(rows, row{label: "From ~/.ssh/config", desc: "Machines ssh already knows; one click fills the name.",
			extra: "host ssh config alias known", wide: true,
			control: func(gtx gl.Context) gl.Dimensions { return hstack(gtx, 8, chips...) }})
	}
	rows = append(rows,
		row{label: "In every window", desc: "Shows this host's sessions in every window, under its own heading, connecting in the background.",
			extra: "host window sidebar show", control: p.switchOf("host-window", e.window, func() { e.window = !e.window })},
		row{label: "Saved as", desc: "The [[hosts]] table this writes to config.toml.", extra: "host config toml saved", wide: true,
			control: p.hostToml},
		row{label: "Connection", desc: "Probes ssh to the fields' destination, then checks the host has pitwall on its PATH.",
			extra: "host test check connect install daemon", wide: true,
			control: p.hostCheck(btn)})

	buttons := []gl.Widget{btn("host-save", "Save", primary, p.saveHost), btn("host-cancel", "Cancel", secondary, func() { e.open = false })}
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
		buttons := []gl.Widget{btn("host-check", "Check connection", secondary, p.checkHost)}
		switch probe {
		case probeIdle:
			status = "Not checked yet."
		case probeRunning:
			status, statusCol = "Checking…", th.Muted
			gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(200 * time.Millisecond)})
		case probeDaemon:
			status, statusCol = "ssh works — "+cmp.Or(probeOut, "pitwall is installed"), th.Green
		case probeNoDaemon:
			status, statusCol = "ssh works, but the host has no pitwall.", th.Yellow
			buttons = append(buttons, btn("host-install", "Install pitwall-core", primary, p.installHost))
		case probeFail:
			status, statusCol = "ssh failed: "+probeOut, th.Red
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
