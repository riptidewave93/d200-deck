# d200-deck

Stream-deck software for the [Ulanzi D200](https://www.ulanzi.com/) running mainline Linux
(see [d200-buildroot](https://github.com/riptidewave93/d200-buildroot) for the OS).

| Program | Runs on | Does |
|---|---|---|
| `deckd keys` | the D200 | draws pages of buttons on the screen, runs key actions (USB HID keyboard, media keys, macros), serves the config channel |
| `deckd update` | the D200 | serves the update page at `http://<deck IPv4>/update` (or `d200-<serial>.local`) and installs updates uploaded to it, confirmed on the keys; `/debug` downloads debug info for bug reports ([update guide](https://github.com/riptidewave93/d200-buildroot/blob/main/docs/update.md)) |
| `deckhost` | the PC | daemon that runs the PC actions (open a URL, launch a program, HTTP requests, Discord) plus a tray icon; Linux and Windows ([docs/deckhost.md](docs/deckhost.md), [install](docs/install.md)) |

Status: early. Buttons, pages, HID actions and macros work on the device, and `deckhost` runs
the PC-side actions (open a URL, launch an app, webhooks) on Linux and Windows. Updates install over USB from
the update page. The configuration website is not done yet.

On the D200, `deckd` runs both parts as child processes and restarts either if it exits
(`deckd keys` or `deckd update` runs just one).
See [docs/design.md](docs/design.md); Discord keys are in [docs/discord.md](docs/discord.md).

## Configuration

`deckd` reads `/data/deckd/config.json` (`/data` is the D200's writable partition), or uses a
built-in default if it is missing or invalid.
`deckd default-config` prints the default as a starting point. A config is a set of named
pages of buttons:

```json
{
  "version": 1,
  "home": "main",
  "images": { "logo": "<base64 PNG or JPEG>" },
  "fonts": { "brand": "<base64 TTF or OTF>" },
  "pages": {
    "main": {
      "keys": {
        "0": { "icon": "public", "label": "Browser", "action": { "type": "page", "page": "browser" } },
        "1": { "image": "logo", "action": { "type": "open-url", "url": "https://example.com" } },
        "2": { "icon": "play-pause", "action": { "type": "hid-consumer", "usage": "play-pause" } },
        "3": { "label": "BRB", "font": "go-mono-bold", "background": "#c62828", "color": "#ffffff", "action": { "type": "type-text", "text": "brb" } }
      }
    },
    "browser": {
      "keys": {
        "0": { "icon": "back", "label": "Back", "action": { "type": "back" } },
        "2": { "icon": "refresh", "label": "Reload", "action": { "type": "hid-key", "keys": ["ctrl", "r"] } }
      }
    }
  }
}
```

| Button field | |
|---|---|
| `icon` | A built-in icon: any [Material Symbol](https://fonts.google.com/icons) name, e.g. `volume_up`, plus a few of our own such as `github` and `discord`; `deckd icons` lists them all |
| `image` | A custom icon from `images` instead (PNG or JPEG, base64; up to 256 KiB and 512x512 px) |
| `label` | Text; up to 3 lines (`\n`) on a key without an icon |
| `font` | The label's font: a built-in one (`deckd fonts`: the Go family, e.g. `go-bold`, `go-mono`; default `go-medium`) or a custom one from `fonts` (TrueType/OpenType, base64, up to 512 KiB) |
| `color`, `background` | `#rrggbb`; the defaults are light grey on dark grey |
| `action` | What the key does |
| `on` | How the key looks while its action's state is on (e.g. Discord muted): any of the fields above, over the key's own. Discord mute and deafen keys have a default: a red `mic_off` / `headset_off` |

Keys are numbered in reading order: 0-4 top row, 5-9 middle row, 10-12 bottom row, 13 the wide
key. Action types are listed in [docs/design.md](docs/design.md#where-an-action-runs).

## Building

You need Docker and `make`. Everything builds and tests in a Go container, the same way locally
and in CI:

```bash
make            # check (gofmt, vet, go.mod tidy), test, build
make test       # unit tests with the race detector, and the coverage total
make cover      # per-function coverage of the last test run
make build      # build/deckd (arm64), build/deckhost and
                # build/deckhost.exe (amd64)
make dist       # deckhost installers into build/dist: .deb, .rpm, Linux tarball,
                # Windows setup.exe (see docs/install.md)
make test DOCKER=   # use the host's Go instead of Docker
```

`go run ./cmd/deckd keys -preview screen.png` renders the default config on a PC.

## License

GPL-3.0-or-later; third-party parts are listed in [NOTICE.md](NOTICE.md).
