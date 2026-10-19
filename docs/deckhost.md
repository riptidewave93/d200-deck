# deckhost

The PC side of the D200, for Linux (Windows later). It runs as your user, not root.

| Part | Does |
|---|---|
| `deckhost daemon` | Runs the buttons USB HID can't: `open-url`, `launch`, `http`. A systemd user service |
| `deckhost tray` | Tray icon: blue when the D200 is connected, grey when not. Started at login |

## Install

```bash
make deckhost
contrib/linux/install.sh build/deckhost
```

This installs `~/.local/bin/deckhost`, enables `deckhost.service` (systemd user unit) and adds the
tray to login autostart. If the daemon logs a permission error on `/dev/hidraw*`, install the udev
rule once:

```bash
sudo install -m644 contrib/linux/70-d200-deck.rules /etc/udev/rules.d/ && sudo udevadm trigger
```

On GNOME the tray icon needs the [AppIndicator extension](https://extensions.gnome.org/extension/615/appindicator-support/)
(Ubuntu and Bluefin ship it; stock Fedora doesn't).

## Tray menu

| Item | Does |
|---|---|
| Configure | Opens the configuration website |
| Update | Opens the device's update page, `http://172.31.254.1/update` (only while connected) |
| Restart | Restarts the daemon (or starts it if it isn't running) |
| Exit | Stops the daemon and closes the tray; both start again at next login |

The tray passes these to the daemon over `$XDG_RUNTIME_DIR/deckhost.sock`. The same commands work
from a shell: `deckhost status | configure | update | restart | exit`.

## How it works

- The daemon finds the D200's config channel (hidraw, vendor usage page `0xFFD2`), reconnects when
  it's replugged, and sends a heartbeat every 2 s.
- A PC action key sends the action to the daemon, which runs it and reports back. The key flashes
  red if no daemon is connected, the action fails, or there's no answer within 15 s.
- Launched programs run in their own session, so they keep running when the daemon restarts or
  stops.
- `deckhost` runs whatever the device config says; see [design.md](design.md#trust).
