# d200-deck

The key software for the [Ulanzi D200](https://www.ulanzi.com/) running
[d200-buildroot](https://github.com/riptidewave93/d200-buildroot), and `deckhost`, its helper for
your PC.

## What you get

- Pages of keys with your own icons, pictures (animated GIFs too), labels, colours and fonts
- Keyboard shortcuts, typed text, macros, and media and volume keys that work on any PC, with
  nothing installed
- With `deckhost` on the PC (Linux or Windows): keys that open websites, start programs and call
  webhooks, and control [Discord](docs/discord.md), [OBS](docs/obs.md) and
  [media players](docs/media.md), showing live state such as mute or the song playing
- A default layout to start from: media and volume keys, the song playing, and pages of browser
  shortcuts, Discord, OBS, macros and power keys
- A sleep screen while the PC sleeps, dimmed or off
- OS updates from a page in your browser, confirmed on the keys

macOS isn't supported yet: the default shortcuts are for Windows and Linux, and deckhost runs on
Linux and Windows.

## Using it

1. Put the D200 OS on the device: see [d200-buildroot](https://github.com/riptidewave93/d200-buildroot).
2. Set up the keys with the [configuration website](https://riptidewave93.github.io/d200-configure/).
3. For the PC keys, [install deckhost](docs/install.md); its [tray menu](docs/deckhost.md) also
   opens the website and the device's update page.

## The D200 project

| Repo | What |
|---|---|
| [d200-buildroot](https://github.com/riptidewave93/d200-buildroot) | The firmware: installing, updating, recovery |
| [d200-deck](https://github.com/riptidewave93/d200-deck) | The keys on the D200, and the PC helper |
| [d200-configure](https://github.com/riptidewave93/d200-configure) | The configuration website: https://riptidewave93.github.io/d200-configure/ |

## Developers

You need Docker or Podman, and `make` (without either, `make` uses the host's Go):

```bash
make            # check, test and build
make test       # unit tests
make install    # build deckhost and install it for your user (Linux)
make dist       # deckhost installers into build/dist
```

`go run ./cmd/deckd keys -preview screen.png` renders the default config on a PC.

### Contributing and releases

Changes go in as pull requests, squash-merged, so the PR title becomes the commit on `main`. Titles
are [conventional commits](https://www.conventionalcommits.org/): `feat(deckd): …` for a feature,
`fix(gfx): …` for a fix, `ci: …`, `docs: …` and so on; a check enforces it.

Release Please keeps a release PR open with the next version and its changelog, from those titles.
Merging it tags the release, and the installers are built and attached to it. Versions follow
semver: a major release is one older parts can't work with (a new minimum protocol, a config format
change, a raised `min_updater`), a minor one adds features, a patch fixes bugs.

| Doc | |
|---|---|
| [config.md](docs/config.md) | The config format |
| [api.md](docs/api.md) | Talking to the device over USB, and its web API |
| [design.md](docs/design.md) | How the parts fit together |

## License

GPL-3.0-or-later; third-party parts are listed in [NOTICE.md](NOTICE.md).
