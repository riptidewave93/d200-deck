package main

import (
	"os"
	"path/filepath"
)

// macOS: app is the first of these app bundles installed, by name in the
// application folders or by full path; * patterns match versioned names
// (the last match wins).
func app(names ...string) []candidate {
	var cs []candidate
	for _, n := range names {
		cs = append(cs, candidate{kind: "app", value: n})
	}
	return cs
}

// programCandidates are the ways each app in programs may be installed.
var programCandidates = map[string][]candidate{
	// system
	"files":        app("/System/Library/CoreServices/Finder.app"),
	"settings":     app("System Settings.app", "System Preferences.app"),
	"terminal":     app("Terminal.app"),
	"task-manager": app("Activity Monitor.app"),
	"calculator":   app("Calculator.app"),
	"text-editor":  app("TextEdit.app"),
	"screenshot":   app("Screenshot.app"),

	// browsers
	"chrome":  app("Google Chrome.app"),
	"firefox": app("Firefox.app"),
	"edge":    app("Microsoft Edge.app"),
	"brave":   app("Brave Browser.app"),
	"opera":   app("Opera.app"),
	"vivaldi": app("Vivaldi.app"),

	// chat
	"discord":  app("Discord.app"),
	"telegram": app("Telegram.app", "Telegram Desktop.app"),
	"whatsapp": app("WhatsApp.app"),
	"signal":   app("Signal.app"),
	"zoom":     app("zoom.us.app"),
	"slack":    app("Slack.app"),
	"teams":    app("Microsoft Teams.app", "Microsoft Teams (work or school).app"),

	// games
	"steam":           app("Steam.app"),
	"epic-games":      app("Epic Games Launcher.app"),
	"battle-net":      app("Battle.net.app"),
	"gog-galaxy":      app("GOG Galaxy.app"),
	"ea-app":          app("EA app.app"),
	"ubisoft-connect": app("Ubisoft Connect.app"),
	"riot-client":     app("Riot Client.app"),

	// media
	"spotify": app("Spotify.app"),
	"vlc":     app("VLC.app"),
	"plex":    app("Plex.app"),
	"kodi":    app("Kodi.app"),
	"obs":     app("OBS.app"),

	// creative
	"photoshop":       app("Adobe Photoshop */Adobe Photoshop *.app"),
	"gimp":            app("GIMP.app", "GIMP-*.app"),
	"inkscape":        app("Inkscape.app"),
	"krita":           app("krita.app"),
	"blender":         app("Blender.app"),
	"davinci-resolve": app("DaVinci Resolve/DaVinci Resolve.app"),
	"audacity":        app("Audacity.app"),
	"figma":           app("Figma.app"),

	// work
	"word":        app("Microsoft Word.app"),
	"excel":       app("Microsoft Excel.app"),
	"powerpoint":  app("Microsoft PowerPoint.app"),
	"outlook":     app("Microsoft Outlook.app"),
	"libreoffice": app("LibreOffice.app"),
	"thunderbird": app("Thunderbird.app"),
	"notion":      app("Notion.app"),
	"obsidian":    app("Obsidian.app"),
	"1password":   app("1Password.app", "1Password 7.app"),
	"bitwarden":   app("Bitwarden.app"),
	"vscode":      app("Visual Studio Code.app"),
}

// findProgram returns how to open the first of cs that's installed, or nil.
func findProgram(cs []candidate) func() error {
	for _, c := range cs {
		dirs := applicationDirs()
		if filepath.IsAbs(c.value) {
			dirs = []string{""}
		}
		for _, dir := range dirs {
			matches, _ := filepath.Glob(filepath.Join(dir, c.value))
			if len(matches) > 0 {
				path := matches[len(matches)-1]
				return func() error { return start("open", "-a", path) }
			}
		}
	}
	return nil
}

// applicationDirs are where apps are installed, the user's first.
func applicationDirs() []string {
	home, _ := os.UserHomeDir()
	return []string{filepath.Join(home, "Applications"), "/Applications", "/Applications/Utilities",
		"/System/Applications", "/System/Applications/Utilities"}
}
