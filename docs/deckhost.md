# deckhost

The PC side of the D200, for Linux and Windows. It runs as your user, not root.

| Part | Does |
|---|---|
| `deckhost daemon` | Runs the keys USB HID can't: `open-url`, `launch`, `http`, and apps ([Discord](discord.md), [OBS](obs.md), [media](media.md), [Open app](api.md#open-app)) |
| `deckhost tray` | Tray icon: blue while a D200 is connected, grey when not. Starts at login, and starts the daemon if it isn't running |

## Install

See [install.md](install.md). To start deckhost after quitting it, open **D200 Deck** from the
app launcher or Start menu.

## Tray menu

Below its header the menu lists the connected D200s and each app that's set up, such as
**Discord: connected** or **OBS: not running**.

| Item | Does |
|---|---|
| Configure | Opens the [configuration website](https://riptidewave93.github.io/d200-configure/) |
| Install an update file… | Opens the device's update page, to install an update file you have |
| Update device to v… | When a newer device OS is out: downloads it for the device, checks it and sends it to the device, then opens the update page. Press **Install** on the device to install it |
| Download deckhost v… | Opens the release page of a newer deckhost, when there is one or the device needs it |
| Check for updates | On by default. Off, deckhost doesn't look for new releases: no notifications, and no **Update device** or **Download deckhost** items |
| Restart | Restarts the daemon |
| Exit | Stops deckhost; it starts again at login, or from **D200 Deck** in the app launcher |

With several D200s connected, the device items become submenus, one item per device.

The same commands work from a shell: `deckhost status | configure | update [serial] |
install [serial] | checks on|off | restart | exit`. `update` opens the update page, `install`
sends the device the new OS and shows how it goes. With several D200s connected, both need the
end of the device's serial.

With **Check for updates** on, deckhost looks for new device OS and deckhost releases once a day
and tells you of each once. Turn it off if you build your own: deckhost then never asks GitHub.
If a device's software is too old or too new for this deckhost, the menu says which to update; a
device too old for this deckhost can still be updated from the menu.

## Window focus on GNOME (Wayland)

GNOME doesn't let a background program bring another app's window to the front. When a key opens
an app that's already running (Discord's Open key, or an `open-url` key with the browser open), the
app gets the request but GNOME shows a "… is ready" notification instead of switching to it. Click
the notification, or install a GNOME extension that lets apps take focus, such as
"Grand Theft Focus" (search for it on [extensions.gnome.org](https://extensions.gnome.org/)).
