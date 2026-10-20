# d200-deck design

One Go module, two programs that share code:

| Program | Runs on | Job |
|---|---|---|
| `deckd keys` | the D200 | draws pages, reads keys, runs HID actions, forwards PC actions |
| `deckd update` | the D200 | the update page on port 80 (`internal/ota`, to d200-buildroot's [updater spec](https://github.com/riptidewave93/d200-buildroot/blob/main/docs/ota.md)) and debug info (`internal/debuginfo`, `/debug`); a separate process, so a crash in the keys can't stop an update |
| `deckhost` | the PC (Linux, Windows) | runs the actions HID can't do |

```
deck/          config model and validation (shared, also built into the website)
proto/         device <-> PC messages (shared)
render/        draws keys as the device does (shared)
cmd/deckd/     device program: runs the keys (internal/deckd) and the update page
               (internal/deckupdate) as child processes and restarts them
cmd/deckhost/  PC program
internal/      device-only and PC-only packages
```

## Where an action runs

On the device whenever USB HID can do it, so those keys work on any PC with nothing installed;
only what HID can't do goes to `deckhost` (the "Runs on" column in
[config.md](config.md#actions)). A PC action can set a `fallback` the device runs while `deckhost`
can't; without one the key flashes red.

## App integrations

Apps such as Discord live in `deckhost`, so a new one needs only a new `deckhost`. The device
knows just these conventions:

| | |
|---|---|
| Action | `{"type": "app", "app": "<app>", "op": "<op>", "args": {…}}`; names are lowercase letters, digits and `-` |
| State | `<app>.ready` and `<app>.<op>`, set by the host ([api.md](api.md#state)) |
| Looks | the host can give a key a label or image of its own, such as the song playing ([api.md](api.md#looks)) |
| Built-ins | optional looks and fallbacks per op, and the args each known app takes (`deck/apps.go`, `deck.AppArgs`) |

In `deckhost`, an integration implements `app` (`cmd/deckhost/apps.go`): it stays connected, runs
ops in order, sets its state and gives the tray a status line. Its settings go in `<app>.json`
next to deckhost's other settings.

Icons come from the `icons` package (the website's catalog); `icons.Embed` adds the ones a config
needs.

## Trust

`deckhost` runs what the device config says, so the config can't change behind the user's back:

- A config that gives keys new actions, or a new USB network, is saved only after a press on the
  device ([api.md](api.md#the-config)).
- New screens ignore presses for a second, so a press meant for the keys doesn't answer them.
- `deckhost` checks again that URLs are http(s).
- Release builds have no U-Boot console, root shell or SSH (d200-buildroot's
  [flavours](https://github.com/riptidewave93/d200-buildroot/blob/main/README.md#building)).
- Updates need a press on the device too; the confirm screen shows the version and whether it's an
  official build.

## Recovery

- A key whose custom font or image fails to draw is drawn without it until the config changes.
- If deckd dies 3 times in a row within 30 s of starting, the next start moves the config to
  `config.json.bad` and uses the default config.

## Shared with d200-buildroot

The USB gadget is d200-buildroot's ([linux.md](https://github.com/riptidewave93/d200-buildroot/blob/main/docs/linux.md#usb)). On this side:

- `netid` must match the image's address derivation byte for byte (vectors in `netid/testdata`);
  `deckd usb-net` prints the addresses for the gadget setup.
- The gadget takes the config channel's descriptor and report length from `deckd raw-descriptor`
  and `deckd raw-report-length`.
- Updates follow its [ota.md](https://github.com/riptidewave93/d200-buildroot/blob/main/docs/ota.md);
  `ota.UpdaterVersion` is the updater version bundles name in `min_updater`.
