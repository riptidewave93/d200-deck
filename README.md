# d200-deck

Stream-deck software for the [Ulanzi D200](https://www.ulanzi.com/) running mainline Linux
(see [d200-buildroot](https://github.com/riptidewave93/d200-buildroot) for the OS).

| Program | Runs on | Does |
|---|---|---|
| `deckd` | the D200 | draws pages of buttons on the screen, runs key actions (USB HID keyboard, media keys, macros), serves the config channel |
| `deckhost` | the PC | talks to the device over the config channel; early stage, Linux only for now |

Status: early. Buttons, pages, HID actions and macros work on the device; the configuration
website and the PC-side actions (open a URL, launch an app, webhooks) are not done yet.
See [docs/design.md](docs/design.md).

## Configuration

`deckd` reads `/etc/deckd/config.json`, or uses a built-in default if it is missing or invalid.
`deckd default-config` prints the default as a starting point. A config is a set of named
pages; each key on a page has an `icon`, a `label` and an `action`:

```json
{
  "version": 1,
  "home": "main",
  "pages": {
    "main": {
      "keys": {
        "0": { "icon": "globe", "label": "Browser", "action": { "type": "page", "page": "browser" } },
        "2": { "icon": "play-pause", "action": { "type": "hid-consumer", "usage": "play-pause" } }
      }
    },
    "browser": {
      "keys": {
        "0": { "icon": "back", "label": "Back", "action": { "type": "back" } },
        "2": { "icon": "reload", "label": "Reload", "action": { "type": "hid-key", "keys": ["ctrl", "r"] } }
      }
    }
  }
}
```

Keys are numbered in reading order: 0-4 top row, 5-9 middle row, 10-12 bottom row, 13 the wide
key. Action types are listed in [docs/design.md](docs/design.md#where-an-action-runs).

## Building

```bash
go test ./...
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags='-s -w' ./cmd/deckd
go build ./cmd/deckhost
go run ./cmd/deckd -preview screen.png   # render the default config on a PC
```

## License

GPL-3.0-or-later; third-party parts are listed in [NOTICE.md](NOTICE.md).
