package main

import (
	"os"
	"path/filepath"
)

// macOS: bundles is the first of these app bundles installed, by name in the
// application folders or by full path; * patterns match versioned names
// (the last match wins).
func bundles(names ...string) []candidate {
	var cs []candidate
	for _, n := range names {
		cs = append(cs, candidate{kind: "app", value: n})
	}
	return cs
}

// programCandidates are the ways each app in programs may be installed.
var programCandidates = map[string][]candidate{
	// system
	"files":        bundles("/System/Library/CoreServices/Finder.app"),
	"settings":     bundles("System Settings.app", "System Preferences.app"),
	"terminal":     bundles("Terminal.app"),
	"task-manager": bundles("Activity Monitor.app"),
	"calculator":   bundles("Calculator.app"),
	"text-editor":  bundles("TextEdit.app"),
	"screenshot":   bundles("Screenshot.app"),

	// browsers
	"chrome":  bundles("Google Chrome.app"),
	"firefox": bundles("Firefox.app"),
	"edge":    bundles("Microsoft Edge.app"),
	"brave":   bundles("Brave Browser.app"),
	"opera":   bundles("Opera.app"),
	"vivaldi": bundles("Vivaldi.app"),

	// chat
	"discord":  bundles("Discord.app"),
	"telegram": bundles("Telegram.app", "Telegram Desktop.app"),
	"whatsapp": bundles("WhatsApp.app"),
	"signal":   bundles("Signal.app"),
	"zoom":     bundles("zoom.us.app"),
	"slack":    bundles("Slack.app"),
	"teams":    bundles("Microsoft Teams.app", "Microsoft Teams (work or school).app"),

	// games
	"steam":           bundles("Steam.app"),
	"epic-games":      bundles("Epic Games Launcher.app"),
	"battle-net":      bundles("Battle.net.app"),
	"gog-galaxy":      bundles("GOG Galaxy.app"),
	"ea-app":          bundles("EA app.app"),
	"ubisoft-connect": bundles("Ubisoft Connect.app"),
	"riot-client":     bundles("Riot Client.app"),

	// media
	"spotify": bundles("Spotify.app"),
	"vlc":     bundles("VLC.app"),
	"plex":    bundles("Plex.app"),
	"kodi":    bundles("Kodi.app"),
	"obs":     bundles("OBS.app"),

	// creative
	"photoshop":       bundles("Adobe Photoshop */Adobe Photoshop *.app"),
	"gimp":            bundles("GIMP.app", "GIMP-*.app"),
	"inkscape":        bundles("Inkscape.app"),
	"krita":           bundles("krita.app"),
	"blender":         bundles("Blender.app"),
	"davinci-resolve": bundles("DaVinci Resolve/DaVinci Resolve.app"),
	"audacity":        bundles("Audacity.app"),
	"figma":           bundles("Figma.app"),

	// work
	"word":        bundles("Microsoft Word.app"),
	"excel":       bundles("Microsoft Excel.app"),
	"powerpoint":  bundles("Microsoft PowerPoint.app"),
	"outlook":     bundles("Microsoft Outlook.app"),
	"libreoffice": bundles("LibreOffice.app"),
	"thunderbird": bundles("Thunderbird.app"),
	"notion":      bundles("Notion.app"),
	"obsidian":    bundles("Obsidian.app"),
	"1password":   bundles("1Password.app", "1Password 7.app"),
	"bitwarden":   bundles("Bitwarden.app"),
	"vscode":      bundles("Visual Studio Code.app"),
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
