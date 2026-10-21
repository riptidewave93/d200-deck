package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

// The "Open app" keys: {"type": "app", "app": "programs", "op": "open",
// "args": {"id": "chrome"}}. deckhost opens only the apps in its own list,
// finding each the way its OS installs it. It sets "programs.available:<id>"
// for the apps it found, so the configuration website can grey out the rest.
const (
	appPrograms  = "programs"
	programsOpen = "open"
)

// A candidate is one way an app may be installed.
type candidate struct {
	kind  string   // exe, file, uri (Windows); desktop, run (Linux)
	value string   // its name, path, URI or desktop file ID
	args  []string // for file and run
}

// Windows: exe is a program in App Paths or on PATH; file a path with
// %VARS% and * patterns (the last match wins); uri a registered URI scheme.
func exe(name string) candidate { return candidate{kind: "exe", value: name} }
func file(path string, args ...string) candidate {
	return candidate{kind: "file", value: path, args: args}
}
func uri(u string) candidate { return candidate{kind: "uri", value: u} }

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

type program struct {
	id      string
	windows []candidate
	linux   []candidate
}

// programs are the apps "Open app" keys can open, by id. Keep docs/api.md's
// list in step.
var programs = []program{
	// system
	{"files", []candidate{exe("explorer.exe")}, []candidate{command("xdg-open", "~"), command("gio", "open", "~")}},
	{"settings", []candidate{uri("ms-settings:")},
		desktop("org.gnome.Settings", "gnome-control-center", "systemsettings", "org.kde.systemsettings")},
	{"terminal", []candidate{exe("wt.exe"), exe("powershell.exe")},
		desktop("org.gnome.Ptyxis", "org.gnome.Console", "org.gnome.Terminal", "org.kde.konsole",
			"xfce4-terminal", "com.mitchellh.ghostty", "Alacritty", "kitty")},
	{"task-manager", []candidate{exe("taskmgr.exe")},
		desktop("org.gnome.SystemMonitor", "gnome-system-monitor", "org.kde.plasma-systemmonitor",
			"io.missioncenter.MissionCenter")},
	{"calculator", []candidate{uri("calculator:"), exe("calc.exe")},
		desktop("org.gnome.Calculator", "org.kde.kcalc", "org.kde.kalk")},
	{"text-editor", []candidate{exe("notepad.exe")},
		desktop("org.gnome.TextEditor", "org.gnome.gedit", "org.kde.kate", "org.kde.kwrite", "org.xfce.mousepad")},
	{"screenshot", []candidate{uri("ms-screenclip:")}, desktop("org.kde.spectacle", "org.gnome.Screenshot")},
	{"control-panel", []candidate{exe("control.exe")}, nil},

	// browsers
	{"chrome", []candidate{exe("chrome.exe")}, desktop("google-chrome", "com.google.Chrome")},
	{"firefox", []candidate{exe("firefox.exe")}, desktop("firefox", "org.mozilla.firefox", "firefox_firefox")},
	{"edge", []candidate{exe("msedge.exe")}, desktop("microsoft-edge", "com.microsoft.Edge")},
	{"brave", []candidate{exe("brave.exe"),
		file(`%LOCALAPPDATA%\BraveSoftware\Brave-Browser\Application\brave.exe`),
		file(`%ProgramFiles%\BraveSoftware\Brave-Browser\Application\brave.exe`)},
		desktop("brave-browser", "com.brave.Browser", "brave_brave")},
	{"opera", []candidate{file(`%LOCALAPPDATA%\Programs\Opera\launcher.exe`)}, desktop("opera", "com.opera.Opera")},
	{"vivaldi", []candidate{exe("vivaldi.exe"), file(`%LOCALAPPDATA%\Vivaldi\Application\vivaldi.exe`)},
		desktop("vivaldi-stable", "com.vivaldi.Vivaldi")},

	// chat
	{"discord", []candidate{file(`%LOCALAPPDATA%\Discord\Update.exe`, "--processStart", "Discord.exe")},
		desktop("discord", "com.discordapp.Discord", "discord_discord")},
	{"telegram", []candidate{file(`%APPDATA%\Telegram Desktop\Telegram.exe`)},
		desktop("org.telegram.desktop", "telegram-desktop", "telegram-desktop_telegram-desktop")},
	{"whatsapp", []candidate{uri("whatsapp:")}, nil},
	{"signal", []candidate{file(`%LOCALAPPDATA%\Programs\signal-desktop\Signal.exe`)},
		desktop("signal-desktop", "org.signal.Signal", "signal-desktop_signal-desktop")},
	{"zoom", []candidate{file(`%APPDATA%\Zoom\bin\Zoom.exe`), file(`%ProgramFiles%\Zoom\bin\Zoom.exe`)},
		desktop("Zoom", "us.zoom.Zoom")},
	{"slack", []candidate{file(`%LOCALAPPDATA%\slack\slack.exe`), uri("slack:")},
		desktop("slack", "com.slack.Slack", "slack_slack")},
	{"teams", []candidate{uri("msteams:")}, nil},

	// games
	{"steam", []candidate{file(`%ProgramFiles(x86)%\Steam\steam.exe`), uri("steam:")},
		desktop("steam", "com.valvesoftware.Steam")},
	{"epic-games", []candidate{
		file(`%ProgramFiles(x86)%\Epic Games\Launcher\Portal\Binaries\Win64\EpicGamesLauncher.exe`),
		file(`%ProgramFiles(x86)%\Epic Games\Launcher\Portal\Binaries\Win32\EpicGamesLauncher.exe`)}, nil},
	{"battle-net", []candidate{file(`%ProgramFiles(x86)%\Battle.net\Battle.net Launcher.exe`)}, nil},
	{"gog-galaxy", []candidate{file(`%ProgramFiles(x86)%\GOG Galaxy\GalaxyClient.exe`)}, nil},
	{"ea-app", []candidate{file(`%ProgramFiles%\Electronic Arts\EA Desktop\EA Desktop\EALauncher.exe`)}, nil},
	{"ubisoft-connect", []candidate{file(`%ProgramFiles(x86)%\Ubisoft\Ubisoft Game Launcher\UbisoftConnect.exe`)}, nil},
	{"riot-client", []candidate{file(`%SystemDrive%\Riot Games\Riot Client\RiotClientServices.exe`)}, nil},

	// media
	{"spotify", []candidate{file(`%APPDATA%\Spotify\Spotify.exe`), uri("spotify:")},
		desktop("spotify", "com.spotify.Client", "spotify_spotify")},
	{"vlc", []candidate{exe("vlc.exe"), file(`%ProgramFiles%\VideoLAN\VLC\vlc.exe`)},
		desktop("vlc", "org.videolan.VLC", "vlc_vlc")},
	{"plex", []candidate{file(`%ProgramFiles%\Plex\Plex\Plex.exe`), file(`%LOCALAPPDATA%\Programs\Plex\Plex.exe`)},
		desktop("tv.plex.PlexDesktop", "plex-desktop_plex-desktop")},
	{"kodi", []candidate{file(`%ProgramFiles%\Kodi\kodi.exe`)}, desktop("kodi", "tv.kodi.Kodi")},
	{"obs", []candidate{file(`%ProgramFiles%\obs-studio\bin\64bit\obs64.exe`)}, desktop("com.obsproject.Studio")},

	// creative
	{"photoshop", []candidate{exe("Photoshop.exe")}, nil},
	{"gimp", []candidate{file(`%ProgramFiles%\GIMP *\bin\gimp-[0-9]*.exe`)}, desktop("gimp", "org.gimp.GIMP")},
	{"inkscape", []candidate{file(`%ProgramFiles%\Inkscape\bin\inkscape.exe`)}, desktop("org.inkscape.Inkscape", "inkscape")},
	{"krita", []candidate{file(`%ProgramFiles%\Krita (x64)\bin\krita.exe`)}, desktop("org.kde.krita")},
	{"blender", []candidate{file(`%ProgramFiles%\Blender Foundation\Blender *\blender-launcher.exe`)},
		desktop("blender", "org.blender.Blender", "blender_blender")},
	{"davinci-resolve", []candidate{file(`%ProgramFiles%\Blackmagic Design\DaVinci Resolve\Resolve.exe`)},
		desktop("com.blackmagicdesign.resolve")},
	{"audacity", []candidate{file(`%ProgramFiles%\Audacity\Audacity.exe`)},
		desktop("audacity", "org.audacityteam.Audacity")},
	{"figma", []candidate{file(`%LOCALAPPDATA%\Figma\Figma.exe`)}, nil},

	// work
	{"word", []candidate{exe("winword.exe")}, nil},
	{"excel", []candidate{exe("excel.exe")}, nil},
	{"powerpoint", []candidate{exe("powerpnt.exe")}, nil},
	{"outlook", []candidate{exe("outlook.exe")}, nil},
	{"libreoffice", []candidate{file(`%ProgramFiles%\LibreOffice\program\soffice.exe`)},
		desktop("libreoffice-startcenter", "org.libreoffice.LibreOffice", "libreoffice_libreoffice")},
	{"thunderbird", []candidate{exe("thunderbird.exe")},
		desktop("thunderbird", "org.mozilla.Thunderbird", "net.thunderbird.Thunderbird", "thunderbird_thunderbird")},
	{"notion", []candidate{file(`%LOCALAPPDATA%\Programs\Notion\Notion.exe`)}, nil},
	{"obsidian", []candidate{file(`%LOCALAPPDATA%\Programs\Obsidian\Obsidian.exe`),
		file(`%LOCALAPPDATA%\Obsidian\Obsidian.exe`)},
		desktop("obsidian", "md.obsidian.Obsidian", "obsidian_obsidian")},
	{"1password", []candidate{file(`%LOCALAPPDATA%\1Password\app\8\1Password.exe`)}, desktop("1password")},
	{"bitwarden", []candidate{file(`%LOCALAPPDATA%\Programs\Bitwarden\Bitwarden.exe`)},
		desktop("bitwarden", "com.bitwarden.desktop", "bitwarden_bitwarden")},
	{"vscode", []candidate{file(`%LOCALAPPDATA%\Programs\Microsoft VS Code\Code.exe`),
		file(`%ProgramFiles%\Microsoft VS Code\Code.exe`)},
		desktop("code", "com.visualstudio.code", "code_code")},
}

// programsLink opens the apps in programs.
type programsLink struct {
	publish func(map[string]bool)
	// find returns how to open the first of an app's candidates that's
	// installed, or nil.
	find  func([]candidate) func() error
	every time.Duration // how often to look again for installed apps

	mu    sync.Mutex
	found map[string]func() error
}

func newProgramsLink(publish func(map[string]bool)) *programsLink {
	return &programsLink{publish: publish, find: findProgram, every: time.Minute}
}

func (l *programsLink) title() string { return "Open app" }

func (l *programsLink) ops() []string { return []string{programsOpen} }

func (l *programsLink) statusText() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.found == nil {
		return ""
	}
	if len(l.found) == 1 {
		return "1 app found"
	}
	return fmt.Sprintf("%d apps found", len(l.found))
}

func (l *programsLink) run(stop <-chan struct{}) {
	t := time.NewTicker(l.every)
	defer t.Stop()
	for {
		l.look()
		select {
		case <-stop:
			return
		case <-t.C:
		}
	}
}

// look finds the installed apps and publishes them.
func (l *programsLink) look() {
	found := map[string]func() error{}
	state := map[string]bool{appPrograms + ".ready": true}
	for _, p := range programs {
		if open := l.find(p.candidates()); open != nil {
			found[p.id] = open
		}
		state[appPrograms+".available:"+p.id] = found[p.id] != nil
	}
	l.mu.Lock()
	l.found = found
	l.mu.Unlock()
	l.publish(state)
}

func (l *programsLink) do(op string, args json.RawMessage, release bool) error {
	if release {
		return nil
	}
	if op != programsOpen {
		return fmt.Errorf("programs has no op %q", op)
	}
	var a struct{ ID string }
	if err := json.Unmarshal(args, &a); err != nil || a.ID == "" {
		return errors.New("open needs args.id")
	}
	i := slices.IndexFunc(programs, func(p program) bool { return p.id == a.ID })
	if i < 0 {
		return fmt.Errorf("no app %q to open; is deckhost up to date?", a.ID)
	}
	l.mu.Lock()
	open := l.found[a.ID]
	l.mu.Unlock()
	if open == nil {
		// installed since the last look?
		if open = l.find(programs[i].candidates()); open == nil {
			return fmt.Errorf("%s isn't installed on this PC", a.ID)
		}
	}
	return open()
}
