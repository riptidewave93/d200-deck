# d200-deck

Stream-deck software for the [Ulanzi D200](https://www.ulanzi.com/) running mainline Linux
(see [d200-buildroot](https://github.com/riptidewave93/d200-buildroot) for the OS).

## The D200 project

| Repo | What |
|---|---|
| [d200-buildroot](https://github.com/riptidewave93/d200-buildroot) | The firmware: U-Boot, Linux, the root filesystem; installing, updating, recovery |
| [d200-deck](https://github.com/riptidewave93/d200-deck) | `deckd` (keys and update page on the device), `deckhost` (the PC helper), the config format and USB API |
| [d200-configure](https://github.com/riptidewave93/d200-configure) | The configuration website: https://riptidewave93.github.io/d200-configure/ |

## This repo

| Program | Runs on | Does |
|---|---|---|
| `deckd keys` | the D200 | draws pages of buttons, runs key actions (USB HID keyboard, media keys, macros), serves the config channel |
| `deckd update` | the D200 | the update page at `http://<deck IPv4>/update` (or `d200-<serial>.local`), confirmed on the keys ([update guide](https://github.com/riptidewave93/d200-buildroot/blob/main/docs/update.md)); `/debug` downloads debug info for bug reports |
| `deckhost` | the PC | runs the PC actions (open a URL, launch a program, HTTP requests, Discord, OBS, media players), with a tray icon; Linux and Windows ([deckhost](docs/deckhost.md), [install](docs/install.md)) |

Configure the keys with the [configuration website](https://riptidewave93.github.io/d200-configure/).

| Doc | |
|---|---|
| [config.md](docs/config.md) | The config format |
| [api.md](docs/api.md) | The USB config channel and the web API |
| [design.md](docs/design.md) | How the parts fit together |
| [Discord](docs/discord.md), [OBS](docs/obs.md), [media](docs/media.md) | App keys |

## Building

You need Docker or Podman, and `make`. Everything builds and tests in a Go container, the same
way locally and in CI (without either, `make` uses the host's Go):

```bash
make            # check (gofmt, vet, go.mod tidy), test, build
make test       # unit tests with the race detector, and the coverage total
make cover      # per-function coverage of the last test run
make build      # build/deckd (arm64), build/deckhost and
                # build/deckhost.exe (amd64)
make install    # build deckhost and install or update it for your user (Linux)
make dist       # deckhost installers into build/dist: .deb, .rpm, Linux tarball,
                # Windows setup.exe
make test DOCKER=   # use the host's Go instead of a container
```

Pushing a `v*` tag builds the installers into a draft release.
`go run ./cmd/deckd keys -preview screen.png` renders the default config on a PC.

## License

GPL-3.0-or-later; third-party parts are listed in [NOTICE.md](NOTICE.md).
