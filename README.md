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

On the D200, `deckd` runs both parts as child processes and restarts either if it exits or stops
answering for 30 s (`deckd keys` or `deckd update` runs just one). While it can, it writes a new
count to `/run/deckd.alive` every 5 s, so init can restart a hung `deckd`.
See [docs/design.md](docs/design.md); the API is in [docs/api.md](docs/api.md), and Discord keys are in [docs/discord.md](docs/discord.md).

## Configuration

`deckd` reads `/data/deckd/config.json` (`/data` is the D200's writable partition), or uses a
built-in default if it is missing or invalid.
`deckd default-config` prints the default as a starting point. A config is a set of named
pages of buttons; the format is in [docs/config.md](docs/config.md).

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
