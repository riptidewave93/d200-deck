package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
)

// Linux: desktop is the first of these desktop file IDs installed;
// command a program on PATH, with "~" for the home directory.
func desktop(ids ...string) []candidate {
	var cs []candidate
	for _, id := range ids {
		cs = append(cs, candidate{kind: "desktop", value: id})
	}
	return cs
}
func command(argv0 string, args ...string) candidate {
	return candidate{kind: "run", value: argv0, args: args}
}

// programCandidates are the ways each app in programs may be installed.
var programCandidates = map[string][]candidate{
	// system
	"files":    {command("xdg-open", "~"), command("gio", "open", "~")},
	"settings": desktop("org.gnome.Settings", "gnome-control-center", "systemsettings", "org.kde.systemsettings"),
	"terminal": desktop("org.gnome.Ptyxis", "org.gnome.Console", "org.gnome.Terminal", "org.kde.konsole",
		"xfce4-terminal", "com.mitchellh.ghostty", "Alacritty", "kitty"),
	"task-manager": desktop("org.gnome.SystemMonitor", "gnome-system-monitor", "org.kde.plasma-systemmonitor",
		"io.missioncenter.MissionCenter"),
	"calculator":  desktop("org.gnome.Calculator", "org.kde.kcalc", "org.kde.kalk"),
	"text-editor": desktop("org.gnome.TextEditor", "org.gnome.gedit", "org.kde.kate", "org.kde.kwrite", "org.xfce.mousepad"),
	"screenshot":  desktop("org.kde.spectacle", "org.gnome.Screenshot"),

	// browsers
	"chrome":  desktop("google-chrome", "com.google.Chrome"),
	"firefox": desktop("firefox", "org.mozilla.firefox", "firefox_firefox"),
	"edge":    desktop("microsoft-edge", "com.microsoft.Edge"),
	"brave":   desktop("brave-browser", "com.brave.Browser", "brave_brave"),
	"opera":   desktop("opera", "com.opera.Opera"),
	"vivaldi": desktop("vivaldi-stable", "com.vivaldi.Vivaldi"),

	// chat
	"discord":  desktop("discord", "com.discordapp.Discord", "discord_discord"),
	"telegram": desktop("org.telegram.desktop", "telegram-desktop", "telegram-desktop_telegram-desktop"),
	"signal":   desktop("signal-desktop", "org.signal.Signal", "signal-desktop_signal-desktop"),
	"zoom":     desktop("Zoom", "us.zoom.Zoom"),
	"slack":    desktop("slack", "com.slack.Slack", "slack_slack"),

	// games
	"steam": desktop("steam", "com.valvesoftware.Steam"),

	// media
	"spotify": desktop("spotify", "com.spotify.Client", "spotify_spotify"),
	"vlc":     desktop("vlc", "org.videolan.VLC", "vlc_vlc"),
	"plex":    desktop("tv.plex.PlexDesktop", "plex-desktop_plex-desktop"),
	"kodi":    desktop("kodi", "tv.kodi.Kodi"),
	"obs":     desktop("com.obsproject.Studio"),

	// creative
	"gimp":            desktop("gimp", "org.gimp.GIMP"),
	"inkscape":        desktop("org.inkscape.Inkscape", "inkscape"),
	"krita":           desktop("org.kde.krita"),
	"blender":         desktop("blender", "org.blender.Blender", "blender_blender"),
	"davinci-resolve": desktop("com.blackmagicdesign.resolve"),
	"audacity":        desktop("audacity", "org.audacityteam.Audacity"),

	// work
	"libreoffice": desktop("libreoffice-startcenter", "org.libreoffice.LibreOffice", "libreoffice_libreoffice"),
	"thunderbird": desktop("thunderbird", "org.mozilla.Thunderbird", "net.thunderbird.Thunderbird", "thunderbird_thunderbird"),
	"obsidian":    desktop("obsidian", "md.obsidian.Obsidian", "obsidian_obsidian"),
	"1password":   desktop("1password"),
	"bitwarden":   desktop("bitwarden", "com.bitwarden.desktop", "bitwarden_bitwarden"),
	"vscode":      desktop("code", "com.visualstudio.code", "code_code"),
}

// findProgram returns how to open the first of cs that's installed, or nil.
func findProgram(cs []candidate) func() error {
	for _, c := range cs {
		switch c.kind {
		case "desktop":
			if open := findDesktop(c.value); open != nil {
				return open
			}
		case "run":
			path, err := exec.LookPath(c.value)
			if err != nil {
				continue
			}
			argv := []string{path}
			for _, a := range c.args {
				if a == "~" {
					a = homeDir()
				}
				argv = append(argv, a)
			}
			return func() error { return startIn(homeDir(), argv...) }
		}
	}
	return nil
}

// findDesktop returns how to open the app of desktop file ID id, or nil if
// it isn't installed. As on the desktop, the first file with the ID counts.
func findDesktop(id string) func() error {
	for _, dir := range applicationDirs() {
		f, err := os.Open(filepath.Join(dir, id+".desktop"))
		if err != nil {
			continue
		}
		e, err := parseDesktopEntry(f)
		f.Close()
		if err != nil || e.Hidden {
			return nil
		}
		if e.TryExec != "" {
			if _, err := exec.LookPath(e.TryExec); err != nil {
				return nil
			}
		}
		path, err := exec.LookPath(e.Exec[0])
		if err != nil {
			return nil
		}
		argv := append([]string{path}, e.Exec[1:]...)
		dir := e.Path
		if dir == "" {
			dir = homeDir()
		}
		return func() error { return startIn(dir, argv...) }
	}
	return nil
}

// homeDir is where apps start unless their desktop file says otherwise, as
// they do from the desktop: deckhost's own directory may be anything.
func homeDir() string {
	home, _ := os.UserHomeDir()
	return home
}

// applicationDirs are where desktop files are, the user's first. Flatpak's
// and Snap's are added, since deckhost's service may not have them in
// XDG_DATA_DIRS.
func applicationDirs() []string {
	home, _ := os.UserHomeDir()
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(home, ".local", "share")
	}
	dataDirs := os.Getenv("XDG_DATA_DIRS")
	if dataDirs == "" {
		dataDirs = "/usr/local/share:/usr/share"
	}
	dirs := append([]string{dataHome}, filepath.SplitList(dataDirs)...)
	dirs = append(dirs, filepath.Join(home, ".local", "share", "flatpak", "exports", "share"),
		"/var/lib/flatpak/exports/share")
	var out []string
	for _, d := range dirs {
		if d != "" && !slices.Contains(out, filepath.Join(d, "applications")) {
			out = append(out, filepath.Join(d, "applications"))
		}
	}
	return append(out, "/var/lib/snapd/desktop/applications")
}
