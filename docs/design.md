# d200-deck design

This is the target design; the [README](../README.md) status says what works today.

One Go module, two programs that share code:

| Program | Runs on | Job |
|---|---|---|
| `deckd` | the D200 | draws pages, reads keys, runs HID actions, forwards PC actions |
| `deckhost` | the PC (Linux, Windows; macOS later) | runs the actions HID can't do, pushes config and images to the device |

```
deck/          config model + validation (shared)
proto/         device <-> PC messages (shared)
cmd/deckd/     device daemon
cmd/deckhost/  PC daemon
internal/      device-only (drm, gfx, input, hidg, screen) and PC-only packages
```

## Where an action runs

A button's action runs on the device whenever USB HID can do it, so those buttons work on any
PC with nothing installed. Only what HID can't do goes to `deckhost`.

| Action `type` | Runs on | Fields | Examples |
|---|---|---|---|
| `hid-consumer` | device | `usage` | play/pause, volume |
| `hid-key` | device | `keys` (e.g. `["ctrl","shift","m"]`), held while the key is held | browser back (`alt+left`), reload (`ctrl+r`), Discord mute/deafen keybinds, push-to-talk |
| `type-text` | device | `text` (ASCII, US layout) | canned text |
| `macro` | device; PC steps forwarded | `steps`: actions and `{"type":"delay","ms":N}` | key sequences |
| `page` / `back` / `home` | device | `page` | folders |
| `open-url` | PC | `url` | open a page in the default browser |
| `launch` | PC | `command` (argv), optional per-OS overrides | start an app or script |
| `http` | PC | `method`, `url`, `headers`, `body` | Discord webhook, Home Assistant |

`hid-key` with F13–F24 combos is a good way to drive apps: nothing else uses those keys, so
users bind them to global shortcuts in Discord, OBS, etc.

If `deckhost` isn't connected, a PC action flashes its key red instead of silently doing nothing.

**Discord:** mute (Ctrl+Shift+M) and deafen (Ctrl+Shift+D) are default Discord keybinds and work
while Discord is in the background, so they are `hid-key` buttons. Disconnect has no default
keybind; the user binds one (e.g. Ctrl+Shift+F13). Discord's local RPC API could also show the
mute state on the key, but its voice-control scope needs Discord's approval or a user-created
Discord app, so it's an optional later `deckhost` feature.

## Icons

- Built-in vector icons, by name (`icon`).
- Custom images (`image`): PNGs uploaded by `deckhost`, stored on the device by SHA-256
  (`/etc/deckd/images/<sha256>.png`) and referenced by hash. Pushing a config uploads the
  missing images first, then the config. The device decodes and scales an image once and caches it
  for the current page only.

## Device <-> PC link

Configuration is done from a **hosted website**, in the style of VIA (usevia.app) for keyboards:
the site talks to the device from the browser over WebHID, so it can be updated without touching
the firmware. `deckhost` uses the same channel (hidraw on Linux, the HID API on Windows).

**Config channel** (`proto/raw.go`): a second HID interface with vendor usage page `0xFFD2`,
64-byte input reports and 1024-byte output reports, and no OUT endpoint (output reports arrive
by SET_REPORT on ep0, so the shared RX FIFO can't stall). One request in flight at a time.

Measured (Linux hidraw and Chrome WebHID): ~50 KB/s to the device, limited by ep0 moving 64-byte
packets at ~1.25 ms each. Enough for configs; custom icons in bulk would want a 64-byte
interrupt OUT endpoint (about 10x faster), which needs a check against the shared-FIFO stall.

Firefox and Safari have no WebHID; their users go through `deckhost`. Linux needs a udev
`uaccess` rule for the hidraw node.

| Message | Direction | Purpose |
|---|---|---|
| `hello` | both | protocol version, device serial/firmware/key geometry, PC OS |
| `action` | device -> PC | run a PC action (`press`/`release`, with an id) |
| `result` | PC -> device | ok/error for an action id (error flashes the key) |
| `config.get` / `config.put` | client -> device | read or replace the config (`base` revision, validated, written atomically, applied live) |
| `config.changed` | device -> clients | the config changed |
| `settings.get` / `settings.put` | client -> device | device settings, kept apart from pages |
| `image.put` / `image.list` | client -> device | upload / list custom images |
| `identify`, `key` | both | flash a key; live key events for the web app |
| `usb.rebinding` | device -> clients | the USB gadget is about to re-plug; reconnect |
| `state` | PC -> device | live key overrides (e.g. a mic-muted icon), later |

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

## USB identity rules

- Interface order is fixed, always-on functions first: keyboard HID, config channel (raw HID),
  USB Ethernet, drive last. Turning the drive off then moves nothing else, so PC drivers stay bound.
- `bcdDevice` is the descriptor-layout version: bump it when always-on interfaces or OS
  descriptors change, not for the drive toggle. The firmware version goes through the API.
- Keep the MS OS descriptors (vendor code 0xcd), the serial-derived MACs and the USB serial.
- Get our own VID:PID (e.g. pid.codes) before release; 1d6b:0104 is the kernel's shared test ID.
- Endpoints: keyboard HID, config channel, NCM and the drive use 5 of 6 IN endpoints; HID input
  reports stay at 64 bytes or less.

## Open items

- Two devices on one PC would both be 172.31.254.1: derive the /30 from the serial if multiple
  devices matter.
- Unicode text and per-app actions need `deckhost` (OS input injection; awkward on Wayland).
- Config lives in the root filesystem, so an OS update may reset it.
