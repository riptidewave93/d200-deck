package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// Windows: exe is a program in App Paths or on PATH; file a path with
// %VARS% and * patterns (the last match wins); uri a registered URI scheme.
func exe(name string) candidate { return candidate{kind: "exe", value: name} }
func file(path string, args ...string) candidate {
	return candidate{kind: "file", value: path, args: args}
}
func uri(u string) candidate { return candidate{kind: "uri", value: u} }

// programCandidates are the ways each app in programs may be installed.
var programCandidates = map[string][]candidate{
	// system
	"files":         {exe("explorer.exe")},
	"settings":      {uri("ms-settings:")},
	"terminal":      {exe("wt.exe"), exe("powershell.exe")},
	"task-manager":  {exe("taskmgr.exe")},
	"calculator":    {uri("calculator:"), exe("calc.exe")},
	"text-editor":   {exe("notepad.exe")},
	"screenshot":    {uri("ms-screenclip:")},
	"control-panel": {exe("control.exe")},

	// browsers
	"chrome":  {exe("chrome.exe")},
	"firefox": {exe("firefox.exe")},
	"edge":    {exe("msedge.exe")},
	"brave": {exe("brave.exe"),
		file(`%LOCALAPPDATA%\BraveSoftware\Brave-Browser\Application\brave.exe`),
		file(`%ProgramFiles%\BraveSoftware\Brave-Browser\Application\brave.exe`)},
	"opera":   {file(`%LOCALAPPDATA%\Programs\Opera\launcher.exe`)},
	"vivaldi": {exe("vivaldi.exe"), file(`%LOCALAPPDATA%\Vivaldi\Application\vivaldi.exe`)},

	// chat
	"discord":  {file(`%LOCALAPPDATA%\Discord\Update.exe`, "--processStart", "Discord.exe")},
	"telegram": {file(`%APPDATA%\Telegram Desktop\Telegram.exe`)},
	"whatsapp": {uri("whatsapp:")},
	"signal":   {file(`%LOCALAPPDATA%\Programs\signal-desktop\Signal.exe`)},
	"zoom":     {file(`%APPDATA%\Zoom\bin\Zoom.exe`), file(`%ProgramFiles%\Zoom\bin\Zoom.exe`)},
	"slack":    {file(`%LOCALAPPDATA%\slack\slack.exe`), uri("slack:")},
	"teams":    {uri("msteams:")},

	// games
	"steam": {file(`%ProgramFiles(x86)%\Steam\steam.exe`), uri("steam:")},
	"epic-games": {
		file(`%ProgramFiles(x86)%\Epic Games\Launcher\Portal\Binaries\Win64\EpicGamesLauncher.exe`),
		file(`%ProgramFiles(x86)%\Epic Games\Launcher\Portal\Binaries\Win32\EpicGamesLauncher.exe`)},
	"battle-net":      {file(`%ProgramFiles(x86)%\Battle.net\Battle.net Launcher.exe`)},
	"gog-galaxy":      {file(`%ProgramFiles(x86)%\GOG Galaxy\GalaxyClient.exe`)},
	"ea-app":          {file(`%ProgramFiles%\Electronic Arts\EA Desktop\EA Desktop\EALauncher.exe`)},
	"ubisoft-connect": {file(`%ProgramFiles(x86)%\Ubisoft\Ubisoft Game Launcher\UbisoftConnect.exe`)},
	"riot-client":     {file(`%SystemDrive%\Riot Games\Riot Client\RiotClientServices.exe`)},

	// media
	"spotify": {file(`%APPDATA%\Spotify\Spotify.exe`), uri("spotify:")},
	"vlc":     {exe("vlc.exe"), file(`%ProgramFiles%\VideoLAN\VLC\vlc.exe`)},
	"plex":    {file(`%ProgramFiles%\Plex\Plex\Plex.exe`), file(`%LOCALAPPDATA%\Programs\Plex\Plex.exe`)},
	"kodi":    {file(`%ProgramFiles%\Kodi\kodi.exe`)},
	"obs":     {file(`%ProgramFiles%\obs-studio\bin\64bit\obs64.exe`)},

	// creative
	"photoshop":       {exe("Photoshop.exe")},
	"gimp":            {file(`%ProgramFiles%\GIMP *\bin\gimp-[0-9]*.exe`)},
	"inkscape":        {file(`%ProgramFiles%\Inkscape\bin\inkscape.exe`)},
	"krita":           {file(`%ProgramFiles%\Krita (x64)\bin\krita.exe`)},
	"blender":         {file(`%ProgramFiles%\Blender Foundation\Blender *\blender-launcher.exe`)},
	"davinci-resolve": {file(`%ProgramFiles%\Blackmagic Design\DaVinci Resolve\Resolve.exe`)},
	"audacity":        {file(`%ProgramFiles%\Audacity\Audacity.exe`)},
	"figma":           {file(`%LOCALAPPDATA%\Figma\Figma.exe`)},

	// work
	"word":        {exe("winword.exe")},
	"excel":       {exe("excel.exe")},
	"powerpoint":  {exe("powerpnt.exe")},
	"outlook":     {exe("outlook.exe")},
	"libreoffice": {file(`%ProgramFiles%\LibreOffice\program\soffice.exe`)},
	"thunderbird": {exe("thunderbird.exe")},
	"notion":      {file(`%LOCALAPPDATA%\Programs\Notion\Notion.exe`)},
	"obsidian": {file(`%LOCALAPPDATA%\Programs\Obsidian\Obsidian.exe`),
		file(`%LOCALAPPDATA%\Obsidian\Obsidian.exe`)},
	"1password": {file(`%LOCALAPPDATA%\1Password\app\8\1Password.exe`)},
	"bitwarden": {file(`%LOCALAPPDATA%\Programs\Bitwarden\Bitwarden.exe`)},
	"vscode": {file(`%LOCALAPPDATA%\Programs\Microsoft VS Code\Code.exe`),
		file(`%ProgramFiles%\Microsoft VS Code\Code.exe`)},
}

// findProgram returns how to open the first of cs that's installed, or nil.
func findProgram(cs []candidate) func() error {
	for _, c := range cs {
		switch c.kind {
		case "exe":
			if appPath(c.value) {
				name := c.value
				return func() error { return openURL(name) } // ShellExecute reads App Paths
			}
			if path, err := exec.LookPath(c.value); err == nil {
				return func() error { return start(path) }
			}
		case "file":
			p, err := registry.ExpandString(c.value)
			if err != nil {
				continue
			}
			matches, _ := filepath.Glob(p)
			if len(matches) == 0 {
				continue
			}
			path := matches[len(matches)-1]
			argv := append([]string{path}, c.args...)
			return func() error { return startIn(filepath.Dir(path), argv...) }
		case "uri":
			if uriScheme(c.value) {
				u := c.value
				return func() error { return openURL(u) }
			}
		}
	}
	return nil
}

// appPath reports whether program name (e.g. chrome.exe) is registered in
// App Paths, for the user or the PC, to a file that's there.
func appPath(name string) bool {
	const key = `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\`
	for _, k := range []struct {
		root   registry.Key
		access uint32
	}{
		{registry.CURRENT_USER, 0},
		{registry.LOCAL_MACHINE, 0},
		{registry.LOCAL_MACHINE, registry.WOW64_32KEY},
	} {
		h, err := registry.OpenKey(k.root, key+name, registry.QUERY_VALUE|k.access)
		if err != nil {
			continue
		}
		v, _, err := h.GetStringValue("")
		h.Close()
		if err != nil {
			continue
		}
		if v, err = registry.ExpandString(strings.Trim(v, `"`)); err == nil {
			if _, err := os.Stat(v); err == nil {
				return true
			}
		}
	}
	return false
}

// uriScheme reports whether the scheme of URI u (e.g. "spotify:") is
// registered.
func uriScheme(u string) bool {
	scheme, _, _ := strings.Cut(u, ":")
	h, err := registry.OpenKey(registry.CLASSES_ROOT, scheme, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer h.Close()
	_, _, err = h.GetStringValue("URL Protocol")
	return err == nil
}
