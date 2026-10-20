# d200-deck design

This is the target design; the [README](../README.md) status says what works today.

One Go module, two programs that share code:

| Program | Runs on | Job |
|---|---|---|
| `deckd keys` | the D200 | draws pages, reads keys, runs HID actions, forwards PC actions |
| `deckd update` | the D200 | web page on port 80 for installing OS updates (`internal/ota`, to d200-buildroot's [updater spec](https://github.com/riptidewave93/d200-buildroot/blob/main/docs/ota.md)) and downloading debug info for bug reports (`internal/debuginfo`, `/debug`), a separate process from the keys so a crash there doesn't stop it |
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

Which runs where is the "Runs on" column of [config.md](config.md#actions).

A PC action can set a `fallback`: an action the device runs instead while `deckhost` isn't
connected (for an `app` action, while the host can't reach that app). Without one, the key flashes red
instead of silently doing nothing.

Discord keys, for one, fall back to Discord's shortcuts ([discord.md](discord.md#without-deckhost)),
and media keys to the plain media keys ([media.md](media.md)).

## App integrations

Apps such as Discord are integrated in `deckhost`, not the device, so a new one needs only a new
`deckhost`. The device knows just the action's shape and these conventions:

| | |
|---|---|
| Action | `{"type": "app", "app": "<app>", "op": "<op>", "args": {…}}`; names are lowercase letters, digits and `-` |
| State | `<app>.ready` and `<app>.<op>`, set by the host ([api.md](api.md#state)) |
| Looks | The host can give keys with a state name a label or image of its own, such as the song playing ([api.md](api.md#looks)) |
| Built-ins | Optional built-in looks, on looks and fallbacks (a keybind or media key) per app op (`deck/apps.go`) |

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
- **Custom** (`image`): PNG, JPEG or GIF files kept in the config itself (`images`, base64 by ID), so
  saving the config saves them too and an OS update can't separate them
  ([limits](config.md#limits)). The device decodes and scales an image the first time it's shown
  (under 0.1 s for a 512 px PNG on the D200) and keeps the result until the config changes, up to
  4 MiB. A GIF animates: deckd redraws just its key when the next frame is due.
- A button shows an icon, a label, or both; `color` and `background` (`#rrggbb`) are optional.
- **Fonts** (`font`): the Go font family is built in (`deckd fonts`); custom TrueType/OpenType
  fonts live in the config like images (`fonts`). A custom font is parsed on
  first use and kept while the config is loaded.

## Device <-> PC link

Configuration is done from a [hosted website](https://riptidewave93.github.io/d200-configure/) ([d200-configure](https://github.com/riptidewave93/d200-configure)), in the style of VIA (usevia.app) for keyboards:
the site talks to the device from the browser over WebHID, so it can be updated without touching
the firmware. `deckhost` uses the same channel (hidraw on Linux, the HID API on Windows).

**Config channel**: a second HID interface ([api.md](api.md#finding-it)) with 1024-byte reports
both ways on interrupt endpoints. The controller has one RX FIFO for every OUT endpoint, so an
unread one would stall them all, USB networking included: the image's f_hid keeps an OUT request
queued even when deckd isn't reading, dropping the oldest unread report
([d200-buildroot](https://github.com/riptidewave93/d200-buildroot/blob/main/docs/linux.md#usb)).

Speeds (Linux hidraw and Chrome WebHID): protocol 3 moved ~50 KB/s to the device by SET_REPORT on
ep0 (one 64-byte packet per ms) and ~27 KB/s back in 64-byte input reports. Protocol 4's 1024-byte
input reports read a 1.69 MiB picture config in 1.4 s, and the config moves gzipped both ways
(image data shrinks by about a quarter, the rest much more); the OUT endpoint takes a request
round trip from ~20 ms to ~2 ms.

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
- Release builds disable the U-Boot console, the root shell and SSH (debug builds keep them;
  see d200-buildroot's [flavours](https://github.com/riptidewave93/d200-buildroot/blob/main/README.md#building)).
- Updates need a press on the device too. The confirm screen shows the bundle's version (letters,
  digits and `._+-` only) and whether it's an official build.

## Device settings

Kept in their own file and messages, so pushing pages can't change USB settings by accident:
the [settings command](api.md#settings), and the image's `/data/usb-gadget.conf`
([d200-buildroot](https://github.com/riptidewave93/d200-buildroot/blob/main/docs/linux.md#usb)). After a change deckd reopens its HID devices,
which the re-plug re-creates. `deckd usb-net` prints the USB addresses for the image's gadget
setup (`DEV_IP4`, `PEER_IP4`, `DEV_IP6`, `NET6`): from the serial (package `netid`), or `net4=`.

## Recovery

- A key that panics while drawing (a bad custom font or image, say) is drawn again without its
  custom font and image, and they aren't used again until the config changes. deckd keeps running.
- If deckd dies 3 times in a row within 30 s of starting, the next start moves the config to
  `config.json.bad` and uses the default config. The count is in `/run`, so it resets at boot.

## USB identity rules

The USB gadget (interfaces, descriptors, endpoints, VID:PID) is d200-buildroot's
([linux.md](https://github.com/riptidewave93/d200-buildroot/blob/main/docs/linux.md#usb), [todo](https://github.com/riptidewave93/d200-buildroot/blob/main/docs/todo.md)). On the deck's side:

- `netid` must stay byte-for-byte the image's address derivation (vectors in `netid/testdata`).
- The config channel's input reports are 1024 bytes: the gadget's `report_length` comes from
  `deckd raw-report-length`, as its descriptor does from `deckd raw-descriptor`, and the IN
  endpoint needs a 1 KB TX FIFO.

## Open items

- Unicode text and per-app actions need `deckhost` (OS input injection; awkward on Wayland).
