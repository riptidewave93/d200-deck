# deckd API

How to talk to a deck. There are two ways in:

| | Over | Used by | For |
|---|---|---|---|
| [Config channel](#config-channel) | USB HID | `deckhost`, the configuration website (WebHID) | keys, PC actions, state, config |
| [Web API](#web-api) | USB Ethernet, HTTP on port 80 | browsers, scripts | updates, debug info |

Go clients can use `proto.Conn` (`proto/client.go`), which does the framing below.

## Config channel

### Finding it

A second HID interface on the deck, next to the keyboard:

| | |
|---|---|
| Usage page | `0xFFD2` (vendor), usage `0x01` |
| Input reports (deck to PC) | 1024 bytes, no report ID (64 on protocol 3 decks) |
| Output reports (PC to deck) | 1024 bytes, no report ID, on the interrupt OUT endpoint (not SET_REPORT: the deck refuses it) |

On Linux it's the hidraw node whose report descriptor is exactly `proto.RawDescriptor` (or
`proto.RawDescriptorV3` on an older deck); it needs the udev rule in
`contrib/linux/70-d200-deck.rules`. In a browser, ask WebHID for `{usagePage: 0xFFD2}`.

Send output reports on the OUT endpoint: hidraw `write()`, WebHID `sendReport()` and Windows
`WriteFile` do. Windows `HidD_SetOutputReport` uses the control endpoint, which the deck refuses
(older decks, with no OUT endpoint, took SET_REPORT).

### Requests

One output report per request. Numbers are little-endian.

| Bytes | Field |
|---|---|
| 0-1 | seq: any value but 0 (requests with 0 are ignored), echoed in the reply |
| 2 | command |
| 3-4 | payload length (up to 1019) |
| 5- | payload, zero-padded |

Send one request at a time and wait for its reply (the deck drops the oldest unread output
reports). Start seq at a random value and count up, so you can tell your replies from other
clients'.

### Replies and events

A reply is one or more input reports:

| Bytes | Field |
|---|---|
| 0-1 | seq of the request; 0 for an event |
| 2 | status: 0 OK, 1 unknown command, 2 bad request, 3 failed (the payload says why) |
| 3 | flags: `0x02` first report of a message, `0x01` more reports follow |
| 4-5 | payload length in this report (up to 1018) |
| 6- | payload |

Protocol 3 decks send 64-byte input reports with a 1-byte length (byte 4, up to 59) and the payload
from byte 5. Tell them apart by the input report size.

Join the payloads of a message's reports. Drop a message that loses its first report, and any over
64 KiB.

Every client sees every input report, so ignore replies to seqs you didn't send. Events are messages
the deck sends on its own, with seq 0; drop those that arrive before your first reply.

### Commands

Payloads are JSON unless noted.

| Cmd | Name | Payload | Reply |
|---|---|---|---|
| `0x01` | hello | none | `{"protocol": 3, "firmware": "<deckd version>", "os": {"name", "version", "flavour", "build"}, "serial": "...", "keys": 14, "model": "ulanzi,d200", "ipv4": "198.19.23.5"}`; `model` is the kind of device (its device tree's first compatible string), `ipv4` its address on the USB network now, for the web API |
| `0x03` | identify | 1 byte: key number; or none | none; the key flashes, or with no payload the whole screen shows white for a second, to tell decks apart (older decks answer status 2) |
| `0x04` | result | `{"id": 7, "error": "..."}`: how a PC action went; leave out `error` if it worked | none |
| `0x05` | action-host | `{"id": "<random host id>", "name": "deckhost", "version": "...", "os": "linux", "apps": [...]}`: heartbeat, every 2 s | `{"looks": {"<name>": "<rev>"}}`: the host's looks the deck shows (older decks: none) |
| `0x06` | state | `{"host": "<host id>", "state": {"discord.mute": true}}` | none |
| `0x07` | settings | none to read; to change, the settings to change, e.g. `{"usb_drive": false}` | the settings: `{"usb_drive": true}` |
| `0x08` | config-info | none, or `{"gzip": true}` | `{"rev": "<sha256 hex>", "size": 1234}`, and `"gzip_size"` if asked |
| `0x09` | config-read | binary: offset (uint32), length (uint16; 0 means 4096, at most 61440; 16384 on protocol 3), and optionally flags (byte; `0x01`: the gzip stream) | binary: the bytes there |
| `0x0a` | config-write | `{"base": "<rev>", "size": 1234, "sha256": "<hex>"}`, and `"gzip_size"` to send it gzipped | none |
| `0x0b` | config-data | binary: offset (uint32), then up to 1015 bytes (of the gzip stream, if gzipped) | none |
| `0x0c` | config-commit | none | `{"rev": "...", "size": 1234}` |
| `0x0d` | host | none | the PC helper: `{"connected": true, "name": "deckhost", "version", "os", "apps": [...], "state": {...}}` |
| `0x0e` | time | `{"unix_ms": 1760900000000}`: the time now | none; the deck has no battery-backed clock, so send it on connecting and every hour |
| `0x0f` | host-look | `{"host", "name", "rev", "label", "image_size", "image_sha256"}`: see [Looks](#looks) | none |
| `0x10` | host-look-data | binary: offset (uint32), then up to 1015 bytes of the look's image | none |

Check `protocol` in hello: this page is version 4. Protocol 3 had 64-byte input reports and no
gzip. New commands come without a new version; a deck
that doesn't know one answers status 1.

### Events

| Cmd | Name | Payload |
|---|---|---|
| `0x01` | action | `{"id": 7, "host": "<host id>", "key": 4, "action": {...}}`, and again with `"release": true` when the key is let go |
| `0x02` | usb-rebinding | none: the deck re-plugs in a moment; reconnect after |
| `0x03` | config-changed | `{"rev": "...", "size": 1234}`: someone saved a new config |
| `0x04` | key | `{"key": 4}` when a key is pressed, `{"key": 4, "release": true}` when it's let go |

### The config

The config is the deck's [config file](config.md), images included, up to
2 MiB: more than one request or reply holds, so it moves in parts, gzipped on protocol 4 decks.
`proto/config.go` does both.

To read it:

1. config-info with `{"gzip": true}` gives its rev, size and `gzip_size` (a protocol 3 deck
   leaves that out: read it plain).
2. config-read with flags `0x01` from offset 0 until you have `gzip_size` bytes, 61440 at a time.
3. Gunzip it, and check its SHA-256 is `rev`. If not, it changed while you read: start again.

To write it:

1. config-write with the new file's size and SHA-256, `base` (the rev you read; empty replaces
   whatever is there) and `gzip_size`: the size of the file gzipped.
2. config-data for each part of the gzip stream, in order, from offset 0.
3. config-commit. The deck checks the file arrived whole, that the config is still `base`, and
   that the config is valid, then saves it, shows it and sends config-changed. If not, it fails
   (status 3) and says why: show that to the user.

If the new config gives keys new actions (anything but `page`, `back` and `home`), the deck asks
on its screen first, and the reply comes after the press: wait up to 60 s for it, and tell the
user to press Save on the deck. Cancel, or no press, fails the commit. So a website or program
can't make keys run things without the user knowing.

A config-write drops any write not yet committed.

### The PC helper

The host command says whether `deckhost` is running on the PC, and what it can do. Use it to show
or hide what needs it, such as Discord keys:

```json
{
  "connected": true,
  "name": "deckhost",
  "version": "v1.2.0",
  "os": "linux",
  "apps": [{"name": "discord", "title": "Discord", "status": "connected", "ops": ["mute", "deafen", ...]}],
  "state": {"discord.ready": true, "discord.mute": false}
}
```

An app without a `status` isn't set up yet. With no helper, it's `{"connected": false}`.

### Settings

| Setting | Default | |
|---|---|---|
| `usb_drive` | on | The read-only USB drive |
| `usb_net4` | `""` | The USB network, to avoid a clash with another on the PC: an IPv4 address such as `10.77.0.4`. The deck takes the first address of the /30 holding it, the PC the second. `""` goes back to the default, picked from the serial. |
| `brightness` | 100 | The screen's brightness in percent, `brightness_min` to 100 (others are clamped). It applies at once, so a slider can send it while dragging, and is saved a second after the last change. While the PC sleeps the screen dims to `brightness_min`. |
| `brightness_min` | | Read only: the lowest `brightness` |

A deck without a dimmable screen, or with older software, leaves out both brightness fields.
A new `usb_net4` is asked about on the deck first, like a config (above). Changing a USB setting re-plugs the deck: you get the reply, then usb-rebinding, then the deck goes away for
about a second (several, for a new address). Use the deck's `ipv4` from hello afterwards, not an
address worked out from its serial.

### Running PC actions

Some actions run on the PC (`open-url`, `launch`, `http`, `app`; see the
[action types](config.md#actions)). To run them:

1. Send action-host with a random id every 2 s. The deck sends actions to the host it heard from
   last, while it heard from it in the last 5 s.
2. Run each action event whose `host` is your id.
3. Answer with result. No answer within 15 s, or an `error`, flashes the key red. Releases need no
   answer.

### State

A host can set named on/off states, which the deck forgets when the host goes away:

| State | Means |
|---|---|
| `<app>.ready` | The host can run that app's actions, e.g. `discord.ready`. Until then the deck runs a key's `fallback` instead. |
| `<app>.<op>` | On: keys with that app action show their `on` look, e.g. `discord.mute` |
| `<app>.<op>:<id>` | The same, for an action with `args.id`, e.g. one voice channel |

A name not set is off. Each message changes only the names it gives, so a host can send what's on
and what just went off, over several messages if it doesn't fit in one.

#### Looks

The action host can also set how keys with a given state name look, such as the song playing on
a `now-playing` key: host-look with a `label` (up to 96 bytes, 3 lines), an image (a JPEG, PNG or
GIF up to 64 KiB, filling the key) sent after it in host-look-data parts, or both, for the key to
pick from (a now-playing key's `args.show`); a look with neither clears it. `rev` names the version: the deck lists the looks it shows in its reply to
action-host, so a host resends only those it's missing, such as after deckd restarts. Up to 16
looks; the deck forgets them with the host.

See [App integrations](design.md#app-integrations).

## Web API

At `http://<deck IPv4>/` or `http://d200-<serial>.local/`. The deck only answers requests addressed
to its IP address or that name (421 otherwise).

| Request | Does |
|---|---|
| `GET /update` | the update page |
| `GET /update/status` | update state, as JSON (below) |
| `PUT /update/bundle` | upload an update bundle (`.tar`); needs the header `X-Deck-Update: 1` and a `Content-Length` |
| `GET /debug` | the debug info page |
| `GET /debug/bundle` | debug info as `.tar.gz` |

Uploading returns 202 once the bundle is checked; the deck then asks for Install on its screen.
Errors: 403 no header, 409 an update is already under way, 411 no length, 400 not a valid bundle,
507 not enough room.

`GET /update/status`:

```json
{
  "state": "installing",
  "step": "Kernel (3/4)",
  "update": {"version": "v1.2.0", "flavour": "release", "build": "202611010000", "signed": true},
  "slot": "b",
  "slots": {"a": {...}, "b": {...}},
  "ota_result": "ok",
  "last": {"version": "...", "signed": true, "slot": "a", "results": [{"target": "kernel", "name": "Kernel", "written": true}]}
}
```

| `state` | Means |
|---|---|
| `checking` | after a reboot, checking the new system works |
| `idle` | ready for an upload |
| `receiving` | upload in progress |
| `confirming` | waiting for Install or Cancel on the deck |
| `installing` | writing; `step` says which |
| `rebooting` | restarting into the update |
| `failed` | `error` says why |
| `cancelled` | Cancel was pressed, or no answer in 2 minutes |
| `unsupported` | this system can't update itself |

`signed` says it's an official build ([signing](https://github.com/riptidewave93/d200-buildroot/blob/main/docs/ota.md#signing)). Unsigned
builds install just the same.

`ota_result` is how the last update went; its values are in d200-buildroot's
[ota.md](https://github.com/riptidewave93/d200-buildroot/blob/main/docs/ota.md#boot).
