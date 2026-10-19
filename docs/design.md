# d200-deck design

This is the target design; the [README](../README.md) status says what works today.

One Go module, two programs that share code:

| Program | Runs on | Job |
|---|---|---|
| `deckd keys` | the D200 | draws pages, reads keys, runs HID actions, forwards PC actions |
| `deckd update` | the D200 | web page on port 80 for installing OS updates (`internal/ota`) and downloading debug info for bug reports (`internal/debuginfo`, `/debug`), a separate process from the keys so a crash there doesn't stop it |
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

The device runs `hid-key`, `hid-consumer`, `hid-system`, `type-text`, `macro`, `page`, `back` and
`home`; the PC runs `open-url`, `launch`, `http` and `app`. Their fields are in
[config.md](config.md#actions).

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

- **Icons** (`icon`): the config carries the icons its keys use (`icons`, by name), so the
  device has none of its own (bar its own screens'). Each is a single-colour SVG path, drawn in
  the key's colour, or a small PNG (emoji) drawn as it is. The configuration website picks them
  from a catalog (package `icons`: Material Symbols, Rounded, filled, ~3,900; `play-pause`,
  `next`, `prev`, `mute`, `volume-up`, `volume-down`, `github`, `discord`; older aliases such as
  `back`) and `icons.Embed` adds the ones a config needs, app keys' built-in looks included.
  Saving a config without them fails; a config already on the device shows such keys without
  the icon.
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

**Config channel**: a second HID interface (vendor usage page `0xFFD2`) with 64-byte input reports
and 1024-byte output reports, and no OUT endpoint (output reports arrive by SET_REPORT on ep0, so
the shared RX FIFO can't stall). The messages are in [api.md](api.md).

Measured (Linux hidraw and Chrome WebHID): ~50 KB/s to the device, limited by ep0 moving 64-byte
packets at ~1.25 ms each. Enough for configs; custom icons in bulk would want a 64-byte
interrupt OUT endpoint (about 10x faster), which needs a check against the shared-FIFO stall.

Firefox and Safari have no WebHID; their users go through `deckhost`. Linux needs a udev
`uaccess` rule for the hidraw node (`contrib/linux/70-d200-deck.rules`).

Whatever can write the config can make `deckhost` run commands on the PC (via `launch`/`http`
actions), so the config channel is security-critical.

## Trust

`deckhost` runs what the device config says. That is safe only if the config can't be changed
behind the user's back:

- A config that gives keys new actions, or a new USB network, is saved only after a press on the
  device (see [the API](api.md#the-config)). Any website allowed to use the device, or program on
  the PC, can ask, but not without the user seeing it.
- New screens ignore presses for a second, so a press meant for the keys doesn't answer them.
- `deckhost` checks again that URLs are http(s), in case the device isn't ours.
- Release builds disable the U-Boot console, the root shell and SSH (debug builds keep them).
- Updates need a press on the device too. The confirm screen shows the bundle's version (letters,
  digits and `._+-` only) and whether it's an official build.

## Device settings

Kept in their own file and messages, so pushing pages can't change USB settings by accident.
`deckd usb-net` prints the USB addresses for the image's gadget setup (`DEV_IP4`, `PEER_IP4`,
`DEV_IP6`, `NET6`): from the serial (package `netid`), or `net4=`.

- USB drive (default on): the read-only USB drive. The image owns the setting: `drive=0|1` in
  `/data/usb-gadget.conf`, read by `S30usb-gadget` at boot; `S30usb-gadget apply` re-binds the
  gadget to match. To change it (the settings command), deckd rewrites the file (keeping lines it
  doesn't know), replies, sends usb-rebinding, runs `apply` and reopens its HID devices. The PC sees a re-plug of about a second, `usb0`
  keeps its address, and `/dev/hidg*` are re-created.

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
