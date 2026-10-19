# d200-deck design

This is the target design; the [README](../README.md) status says what works today.

One Go module, two programs that share code:

| Program | Runs on | Job |
|---|---|---|
| `deckd keys` | the D200 | draws pages, reads keys, runs HID actions, forwards PC actions |
| `deckd update` | the D200 | web page on port 80 for installing OS updates (`internal/ota`), a separate process from the keys so a crash there doesn't stop it |
| `deckhost` | the PC (Linux, Windows; macOS later) | runs the actions HID can't do, pushes config and images to the device |

```
deck/          config model + validation (shared)
proto/         device <-> PC messages (shared)
cmd/deckd/     device program: runs the keys (internal/deckd) and the update page
               (internal/deckupdate) as child processes and restarts them; init restarts deckd
cmd/deckhost/  PC daemon
internal/      device-only (deckd, deckupdate, ota, drm, gfx, input, hidg, screen) and PC-only packages
```

## Where an action runs

A button's action runs on the device whenever USB HID can do it, so those buttons work on any
PC with nothing installed. Only what HID can't do goes to `deckhost`.

| Action `type` | Runs on | Fields | Examples |
|---|---|---|---|
| `hid-consumer` | device | `usage` | play/pause, volume |
| `hid-key` | device | `keys` (e.g. `["ctrl","shift","m"]`), held while the key is held | browser back (`alt+left`), reload (`ctrl+r`), Discord mute/deafen keybinds, push-to-talk |
| `type-text` | device | `text` (ASCII, US layout) | canned text |
| `macro` | device (no PC steps yet) | `steps`: actions and `{"type":"delay","ms":N}` | key sequences; press again to stop it |
| `page` / `back` / `home` | device | `page` (one already open is gone back to) | folders |
| `open-url` | PC | `url` | open a page in the default browser |
| `launch` | PC | `command` (argv), optional per-OS overrides | start an app or script |
| `http` | PC | `method`, `url`, `headers`, `body` | Discord webhook, Home Assistant |
| `app` | PC | `app`, `op`, optional `args` (an object): see [App integrations](#app-integrations) | Discord mute ([discord.md](discord.md)) |

`hid-key` with F13–F24 combos is a good way to drive apps: nothing else uses those keys, so
users bind them to global shortcuts in Discord, OBS, etc.

A PC action can set a `fallback`: an action the device runs instead while `deckhost` isn't
connected (for an `app` action, while the host can't reach that app). Without one, the key flashes red
instead of silently doing nothing.

**Discord:** Discord keys go through Discord's local API and show whether you're muted, but need
a Discord app set up ([discord.md](discord.md)). Until then mute and deafen fall back to
Discord's default shortcuts, Ctrl+Shift+M and Ctrl+Shift+D.

## App integrations

Apps such as Discord are integrated in `deckhost`, not the device, so a new one needs only a new
`deckhost`. The device knows just the action's shape and these conventions:

| | |
|---|---|
| Action | `{"type": "app", "app": "<app>", "op": "<op>", "args": {…}}`; names are lowercase letters, digits and `-` |
| `<app>.ready` | State the host sets while it can run the app's actions (e.g. logged in to it). Until then the device runs the key's `fallback`, if it has one, or flashes the key red |
| `<app>.<op>` | State that, while on, shows the key's `on` look (e.g. `discord.mute`) |
| Built-ins | Optional built-in looks, on looks and fallback keybinds per app op (`deck/apps.go`) |

In `deckhost`, an integration implements `app` (`cmd/deckhost/apps.go`): it keeps itself
connected, runs ops (each app's in order), sets its state and reports a status line for the tray.
Its settings, such as the user's own app registration, go in `<app>.json` next to deckhost's
other settings. An app it doesn't know, or an unknown op, is an error result.

## Icons and fonts

- **Built-in** (`icon`): all of Material Symbols (Rounded, filled; ~3,900, by their own names such
  as `volume_up`, see [fonts.google.com/icons](https://fonts.google.com/icons)), a few drawn ones
  (`play-pause`, `next`, `prev`, `mute`, `volume-up`, `volume-down`, `github`) and older aliases
  (`back`, `globe`, ...). `deckd icons` lists every name. Paths are parsed only when drawn.
- **Custom** (`image`): PNG or JPEG files kept in the config itself (`images`, base64 by ID), so
  saving the config saves them too and an OS update can't separate them. Limits: 256 KiB and
  512x512 px per image, 2 MiB per config. Size them to the keys (196 px) before upload. The device
  decodes and scales an image the first time it's shown (under 0.1 s for a 512 px PNG on the D200) and
  keeps the result until the config changes, up to 4 MiB.
- A button shows an icon, a label, or both; `color` and `background` (`#rrggbb`) are optional.
- **Fonts** (`font`): the Go font family is built in (`deckd fonts`); custom TrueType/OpenType
  fonts live in the config like images (`fonts`, up to 512 KiB each). A custom font is parsed on
  first use and kept while the config is loaded.

## Device <-> PC link

Configuration is done from a **hosted website**, in the style of VIA (usevia.app) for keyboards:
the site talks to the device from the browser over WebHID, so it can be updated without touching
the firmware. `deckhost` uses the same channel (hidraw on Linux, the HID API on Windows).

**Config channel** (`proto/raw.go`): a second HID interface with vendor usage page `0xFFD2`,
64-byte input reports and 1024-byte output reports, and no OUT endpoint (output reports arrive
by SET_REPORT on ep0, so the shared RX FIFO can't stall). Protocol version 3: requests and
replies carry a 16-bit seq, and each client starts at a random one and has one request in flight
at a time (the device keeps only the latest unread output report).

Measured (Linux hidraw and Chrome WebHID): ~50 KB/s to the device, limited by ep0 moving 64-byte
packets at ~1.25 ms each. Enough for configs; custom icons in bulk would want a 64-byte
interrupt OUT endpoint (about 10x faster), which needs a check against the shared-FIFO stall.

Firefox and Safari have no WebHID; their users go through `deckhost`. Linux needs a udev
`uaccess` rule for the hidraw node (`contrib/linux/70-d200-deck.rules`).

Every client (the website, `deckhost`, `deckhost hello`) sees every input report, so clients
ignore replies to seqs they didn't send. The device also sends **events** on its own: messages
with seq 0, which requests never use. Clients reassemble each message from its reports (the first
is flagged, so one cut short is dropped, as is one over 64 KiB), events and replies can
interleave, and clients drop events that arrive before their first reply (left over from before
they connected).

| Message | Direction | Status | Purpose |
|---|---|---|---|
| `hello` (0x01) | client -> device | done | protocol version, deckd version, OS version and flavour, serial, key count |
| `identify` (0x03) | client -> device | done | flash a key |
| `action-host` (0x05) | PC -> device | done | heartbeat every 2 s from `deckhost`, with a random host id; PC actions are only sent while one arrived in the last 5 s (otherwise the key flashes red), to the host heard from last |
| `action` (event 0x01) | device -> PC | done | run a PC action, with an id and the host's id; on release an event with `release` follows (no result) |
| `result` (0x04) | PC -> device | done | ok/error for an action id; an error, or no result within 15 s, flashes the key red |
| `config.get` / `config.put` | client -> device | | read or replace the config (`base` revision, validated, written atomically, applied live) |
| `config.changed` | device -> clients | | the config changed |
| `settings.get` / `settings.put` | client -> device | | device settings, kept apart from pages |
| `image.put` / `image.list` | client -> device | | upload / list custom images |
| `key` | device -> clients | | live key events for the web app |
| `usb.rebinding` | device -> clients | | the USB gadget is about to re-plug; reconnect |
| `state` (0x06) | PC -> device | done | named on/off state (e.g. `discord.mute`) from the current action host; forgotten when it goes away. A key whose action has that state shows its `on` look; `<app>.ready` says that app's keys can go to the host |

Whatever can write the config can make `deckhost` run commands on the PC (via `launch`/`http`
actions), so the config channel is security-critical.

## Trust

`deckhost` runs what the device config says. That is safe only if the config can't be changed
behind the user's back, so release builds disable the U-Boot console, the root shell and SSH
(debug builds keep them).

## Device settings

Kept in their own file and messages, so pushing pages can't change USB settings by accident.

- `usb.drive` (default on): the read-only USB drive. `S30usb-gadget` takes its function list from
  `deckd usb-functions`. A change unbinds the gadget, removes or adds the drive and rebinds: the PC
  sees a 1-3 s re-plug, `usb0` keeps its address, and `/dev/hidg0` is re-created (deckd reopens it).

## Recovery

- A key that panics while drawing (a bad custom font or image, say) is drawn again without its
  custom font and image, and they aren't used again until the config changes. deckd keeps running.
- If deckd dies 3 times in a row within 30 s of starting, the next start moves the config to
  `config.json.bad` and uses the default config. The count is in `/run`, so it resets at boot.

## USB identity rules

- Interface order is fixed, always-on functions first: keyboard HID, config channel (raw HID),
  USB Ethernet, drive last. Turning the drive off then moves nothing else, so PC drivers stay bound.
- `bcdDevice` is the descriptor-layout version: bump it when always-on interfaces or OS
  descriptors change, not for the drive toggle. The firmware version goes through the API.
- Keep the MS OS descriptors (vendor code 0xcd), the serial-derived MACs and the USB serial.
- USB networking is per device: its /30 in 198.18.0.0/15 and `d200-<serial>.local` come from its
  serial. The image's boot scripts do it; `netid` must stay byte-for-byte the same derivation
  (vectors in `netid/testdata`).
- Get our own VID:PID (e.g. pid.codes) before release; 1d6b:0104 is the kernel's shared test ID.
- Endpoints: keyboard HID, config channel, NCM and the drive use 5 of 6 IN endpoints; HID input
  reports stay at 64 bytes or less.

## Open items

- Unicode text and per-app actions need `deckhost` (OS input injection; awkward on Wayland).
