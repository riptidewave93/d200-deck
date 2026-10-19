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
| Input reports (deck to PC) | 64 bytes, no report ID |
| Output reports (PC to deck) | 1024 bytes, no report ID, sent with SET_REPORT |

On Linux it's the hidraw node whose report descriptor is exactly `proto.RawDescriptor`; it needs
the udev rule in `contrib/linux/70-d200-deck.rules`. In a browser, ask WebHID for
`{usagePage: 0xFFD2}`.

### Requests

One output report per request. Numbers are little-endian.

| Bytes | Field |
|---|---|
| 0-1 | seq: any value but 0 (requests with 0 are ignored), echoed in the reply |
| 2 | command |
| 3-4 | payload length (up to 1019) |
| 5- | payload, zero-padded |

Send one request at a time and wait for its reply (the deck keeps only the latest unread output
report). Start seq at a random value and count up, so you can tell your replies from other
clients'.

### Replies and events

A reply is one or more input reports:

| Bytes | Field |
|---|---|
| 0-1 | seq of the request; 0 for an event |
| 2 | status: 0 OK, 1 unknown command, 2 bad request, 3 failed (the payload says why) |
| 3 | flags: `0x02` first report of a message, `0x01` more reports follow |
| 4 | payload length in this report (up to 59) |
| 5- | payload |

Join the payloads of a message's reports. Drop a message that loses its first report, and any over
64 KiB.

Every client sees every input report, so ignore replies to seqs you didn't send. Events are messages
the deck sends on its own, with seq 0; drop those that arrive before your first reply.

### Commands

Payloads are JSON unless noted.

| Cmd | Name | Payload | Reply |
|---|---|---|---|
| `0x01` | hello | none | `{"protocol": 3, "firmware": "<deckd version>", "os": {"name", "version", "flavour", "build"}, "serial": "...", "keys": 14, "model": "ulanzi,d200", "ipv4": "198.19.148.17"}`; `model` is the kind of device (its device tree's first compatible string), `ipv4` its address on the USB network now, for the web API |
| `0x03` | identify | 1 byte: key number | none; the key flashes |
| `0x04` | result | `{"id": 7, "error": "..."}`: how a PC action went; leave out `error` if it worked | none |
| `0x05` | action-host | `{"id": "<random host id>", "name": "deckhost", "version": "...", "os": "linux", "apps": [...]}`: heartbeat, every 2 s | none |
| `0x06` | state | `{"host": "<host id>", "state": {"discord.mute": true}}` | none |
| `0x07` | settings | none to read; to change, the settings to change, e.g. `{"usb_drive": false}` | the settings: `{"usb_drive": true}` |
| `0x08` | config-info | none | `{"rev": "<sha256 hex>", "size": 1234}` |
| `0x09` | config-read | binary: offset (uint32), length (uint16; 0 means 4096, at most 16384) | binary: the config's bytes there |
| `0x0a` | config-write | `{"base": "<rev>", "size": 1234, "sha256": "<hex>"}` | none |
| `0x0b` | config-data | binary: offset (uint32), then up to 1015 bytes | none |
| `0x0c` | config-commit | none | `{"rev": "...", "size": 1234}` |
| `0x0d` | host | none | the PC helper: `{"connected": true, "name": "deckhost", "version", "os", "apps": [...], "state": {...}}` |

Check `protocol` in hello: this page is version 3. New commands come without a new version; a deck
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
2 MiB: more than one request or reply holds, so it moves in parts. `proto/config.go` does both.

To read it:

1. config-info gives its rev and size.
2. config-read from offset 0 until you have `size` bytes.
3. Check its SHA-256 is `rev`. If not, it changed while you read: start again.

To write it:

1. config-write with the new file's size and SHA-256, and `base`: the rev you read. An empty
   `base` replaces whatever is there.
2. config-data for each part, in order, from offset 0.
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

A new `usb_net4` is asked about on the deck first, like a config (above). Changing either re-plugs the deck: you get the reply, then usb-rebinding, then the deck goes away for
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
  "update": {"version": "4b96a04", "flavour": "debug", "build": "202610192038", "signed": false},
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

`signed` says it's an official build: its `manifest.sig` checks out with the key in
`/etc/d200/update-key.pub`. Unsigned builds install just the same.

`ota_result` is how the last update went: `trial` (just installed, being checked), `ok`, `failed`
or `rolled-back`. Or `fell-back`: the system stopped starting, so the device started the one in
the other slot; the next update writes over the broken one.
