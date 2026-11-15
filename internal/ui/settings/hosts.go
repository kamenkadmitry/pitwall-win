package settings

import (
	"cmp"
	"strings"

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
}

// editHost opens the editor on h, or on a new host for nil.
func (p *Page) editHost(h *config.Host) {
	e := &p.ho
	e.open, e.err, e.confirm, e.focus, e.scroll = true, "", false, true, true
	e.window = false
	e.old = ""
	if h != nil {
		e.old, e.window = h.Name, h.Window
		e.name.SetText(h.Name)
		e.ssh.SetText(h.SSH)
	} else {
		e.name.SetText("")
		e.ssh.SetText("")
	}
}

// saveHost writes the editor's host.
func (p *Page) saveHost() {
	e := &p.ho
	h := config.Host{Name: strings.TrimSpace(e.name.Text()), SSH: strings.TrimSpace(e.ssh.Text()), Window: e.window}
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
		{label: "ssh", desc: "The ssh destination as you would type it after ssh: user@box, or a Host from ~/.ssh/config. Empty means the name.", extra: "host ssh destination user",
			control: p.edField(&e.ssh, "me@10.0.0.2", 320, p.saveHost, nil)},
		{label: "In every window", desc: "Shows this host's sessions in every window, under its own heading, connecting in the background.",
			extra: "host window sidebar show", control: p.switchOf("host-window", e.window, func() { e.window = !e.window })},
	}
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
