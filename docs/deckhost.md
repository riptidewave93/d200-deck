# deckhost

The PC side of the D200, for Linux and Windows (the OS-specific parts are in
`cmd/deckhost/os_*.go` and `internal/hidraw`). It runs as your user, not root.

| Part | Does |
|---|---|
| `deckhost daemon` | Runs the buttons USB HID can't: `open-url`, `launch`, `http`, and `app` integrations ([Discord](discord.md), [OBS](obs.md), [media](media.md)). A systemd user service on Linux; started by the tray on Windows |
| `deckhost tray` | Tray icon: blue when the D200 is connected, grey when not. Started at login |

## Install

See [install.md](install.md).

## Tray menu

Below its header the menu lists the connected D200s and how each app integration is, such as
**Discord: connected**, **OBS: not running** or the media players. An app that isn't set up has
no line.

| Item | Does |
|---|---|
| Configure | Opens the [configuration website](https://riptidewave93.github.io/d200-configure/) |
| Update | Opens the device's update page, the deck's IPv4 address, e.g. `http://198.19.23.5/update`, since sandboxed browsers often can't resolve `.local` (only while connected; a submenu per device when several are) |
| Download OS v… | Opens the GitHub release page of a newer device OS than a connected device has (only then) |
| Download deckhost v… | Opens the GitHub release page of a newer deckhost, shown when there is one, or when a device's software is too new for this deckhost |
| Restart | Restarts the daemon (or starts it if it isn't running) |
| Exit | Stops the daemon and closes the tray; both start again at next login |

The tray passes these to the daemon over `$XDG_RUNTIME_DIR/deckhost.sock`. The same commands work
from a shell: `deckhost status | configure | update [serial] | restart | exit`. With several D200s
connected, `update` needs the end of the device's serial.

## How it works

- The daemon finds the D200's config channel (the hidraw node whose report descriptor is exactly
  `proto.RawDescriptor`), reconnects when it's replugged, and sends a heartbeat every 2 s.
- Only one daemon runs per user: it holds `$XDG_RUNTIME_DIR/deckhost.sock.lock`.
- A PC action key sends the action to the daemon, which runs it and reports back. The key flashes
  red if no daemon is connected, the action fails, or there's no answer within 15 s.
- Launched programs (and the browser) run in their own session and, through `systemd-run --user
  --scope`, their own systemd scope, so they keep running when the daemon restarts or stops.
- `deckhost` runs whatever the device config says; see [design.md](design.md#trust).
- At start and every 24 h, the daemon reads the latest device OS and deckhost releases from
  GitHub. If one is newer than a connected device's OS, or than deckhost, a notification says so
  once per release until deckhost restarts; clicking it opens the release page. A build with no
  release tag (a bare commit) is never told.
- deckhost talks protocols `proto.MinVersion` to `proto.Version`. A device outside that shows
  "update it" or "update deckhost" in the menu instead of connecting.

## Window focus on GNOME (Wayland)

GNOME doesn't let a background program bring another app's window to the front. When a key opens
an app that's already running (Discord's Open key, or an `open-url` key with the browser open), the
app gets the request but GNOME shows a "… is ready" notification instead of switching to it. Click
the notification, or install a GNOME extension that lets apps take focus, such as
"Grand Theft Focus" (search for it on [extensions.gnome.org](https://extensions.gnome.org/)).
