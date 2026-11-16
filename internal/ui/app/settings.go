package app

import (
	"errors"
	"log"

	"gioui.org/io/key"
	gl "gioui.org/layout"

	"github.com/quanticstudios/pitwall/internal/config"
	"github.com/quanticstudios/pitwall/internal/model"
	"github.com/quanticstudios/pitwall/internal/ui/settings"
	"github.com/quanticstudios/pitwall/internal/ui/sidebar"
)

// openSettings shows the settings page in place of the active tab's panes.
func (u *ui) openSettings() {
	u.settingsWS = u.nav.workspace
	path := u.cfg.Path
	if path == "" {
		path = config.Path()
	}
	u.settings.Version = Version
	u.settings.Show(path)
}

// settingsKeys lets the page take its keys before the window's shortcuts:
// a chord being recorded, and Escape. An open switcher or dialog keeps
// Escape.
func (u *ui) settingsKeys(gtx gl.Context) {
	if !u.nav.switcherVisible() && u.modal.kind == modalNone {
		u.settingsResult(u.settings.Keys(gtx))
	}
}

// settingsShortcut handles open_settings, which opens and closes the page.
func (u *ui) settingsShortcut(e key.Event) bool {
	if u.nav.tabMode || u.nav.paneMode || u.nav.bind().Action(e) != "open_settings" {
		return false
	}
	if e.State == key.Press {
		u.toggleSettings()
	}
	return true
}

// toggleSettings opens the settings page, or closes it when it is shown.
func (u *ui) toggleSettings() {
	if u.settings.Shown() {
		u.settings.Hide()
	} else {
		u.openSettings()
	}
}

// layoutSettings draws the page, or the panes again once another tab is
// selected (from the keyboard, say).
func (u *ui) layoutSettings(gtx gl.Context, st *model.State) {
	if u.nav.workspace != u.settingsWS {
		u.settings.Hide()
		u.layoutPanes(gtx, st)
		return
	}
	u.settings.Limits = u.limits.get()
	u.settings.Skillshare = ssShown
	u.settings.WindowHosts, u.settings.OneHost = windowHosts(u.hosts), Host != ""
	r := u.settings.Layout(gtx, u.th, u.cfg, u.probs)
	if r != settings.RestartWindow {
		u.settingsResult(r)
		return
	}
	u.settings.RestartErr = ""
	if err := u.restartWindow(st); err != nil {
		log.Printf("restart the window for its hosts: %q", err)
		u.settings.RestartErr = err.Error()
	}
}

// windowHosts are the hosts among hosts, the window's machines, for the
// settings page: every one but this machine.
func windowHosts(hosts []sidebar.HostInfo) []settings.WindowHost {
	var out []settings.WindowHost
	for _, h := range hosts {
		if h.Name != "" {
			out = append(out, settings.WindowHost{Name: h.Name, SSH: h.SSH})
		}
	}
	return out
}

// restartWindow opens this window again in a process of its own, on the
// same session and tab, and closes this one. The new window reads
// [[hosts]] as it is now; the daemon keeps every tab meanwhile.
func (u *ui) restartWindow(st *model.State) error {
	s := st.Session(u.nav.session)
	switch {
	case Relaunch == nil:
		return errors.New("this build cannot open a window")
	case s == nil || s.Name == "":
		return errors.New("no session to reopen on")
	}
	if err := Relaunch(s.Name, u.nav.workspace); err != nil {
		return err
	}
	u.relaunched = true
	return nil
}

func (u *ui) settingsResult(r settings.Result) {
	switch r {
	case settings.Closed:
		u.settings.Hide()
	case settings.Saved:
		u.reloadConfig()
	case settings.InstallHooks:
		u.openHooks()
	case settings.InstallStatusline:
		u.openHooksWith(true)
	case settings.AskAgent:
		u.task.preset, u.nav.task = u.settings.TaskPrompt(), true
	}
}

// reloadConfig applies config.toml now, after the page wrote it, rather
// than when the watcher next polls.
func (u *ui) reloadConfig() {
	l := loadConfig()
	u.cfgMu.Lock()
	u.watchTheme = l.s.ThemeName
	u.cfgMu.Unlock()
	u.apply(l)
	if u.report != nil {
		u.report(l.probs)
	}
}
