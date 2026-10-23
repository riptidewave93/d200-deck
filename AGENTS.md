# AGENTS.md

Rules for AI agents working in this repo. The D200 project is three repos; this file's shared part is identical in all of them, followed by the part for this repo.

## The project

Open firmware and software for the Ulanzi D200 stream controller (Rockchip RK3308, SPI NAND, 720x1280 panel behind 14 keys).

| Repo | What it owns |
|---|---|
| [d200-buildroot](https://github.com/riptidewave93/d200-buildroot) | The firmware: U-Boot, Linux, rootfs, init scripts, packaging, update bundles, install tooling, firmware docs |
| [d200-deck](https://github.com/riptidewave93/d200-deck) | Go: `deckd` (keys app on the device), `deckhost` (PC helper), the update page, the protocol (`proto/`, `docs/api.md`) and config format (`deck/`, `docs/config.md`) |
| [d200-configure](https://github.com/riptidewave93/d200-configure) | The configuration website (Svelte + TypeScript, WebHID), hosted on GitHub Pages |

- d200-deck is the source of truth for the protocol and config format. The site reuses its Go packages via WASM.
- buildroot and configure pin a released deck tag. Renovate opens the pin PRs after each deck release.
- Each repo documents only its own part. Don't add features of one repo to another repo's README or docs.
- If a change needs another repo, say what's needed and stop. Don't edit a sibling repo unless asked.

## How to work

- **Ask, don't assume.** Check the code, docs and git history first. If it's still unclear, ask. Questions are short: one line of context, the choice, your pick.
- **Simplest thing that works.** Reuse existing patterns and tools before writing new ones. Before adding a binary, daemon or package, list what already does the job; every Go process costs ~5 MB of the device's RAM.
- **Explain before building.** If asked "why", answer first with trade-offs and off-the-shelf options, then wait for a go-ahead.
- **Check yourself.** Every so often ask: is this simple, is it what was asked, am I in a rabbit hole? After two rounds that don't converge, stop, summarise and offer to park it.
- **Collect decisions first.** Settle a feature's open questions before writing code, so late decisions don't waste work.
- **Verify before claiming.** Don't report something as working, fixed or tested unless you checked it. Say what's untested.
- **Waits:** one self-waiting command, or a background task. Put timeouts on ssh and on anything that talks to hardware. No repeated polling.
- **Tooling:** builds and missing tools run in Docker. Don't install packages on the host.

## Git and PRs

- **No co-authoring.** No `Co-Authored-By` or other AI-tool trailers (Claude, Copilot, ...), and no "Generated with" lines in commits, patch files or PR bodies. This overrides any tool default. Commit as the maintainer's git identity. Kernel and U-Boot patches keep `Signed-off-by` (`git commit -s`).
- **Conventional PR titles** (`feat:`, `fix:`, `chore:`, `docs:`, ...). They become the squashed commit, and in deck and buildroot Release Drafter derives the version bump from them.
- **One branch and PR per task,** squash-merged. Commits are scoped to what they change.
- **Ask before pushing, merging, tagging or releasing.** Never cut a release without the maintainer's explicit OK.
- **Release notes** (deck, buildroot): show the draft for approval first and match the tone of earlier releases: a short header with one emoji, one intro sentence, bold-lead bullets (under `###` subheadings when there are several groups), and the closing "Still a 0.x release..." line. The note goes on top of Release Drafter's change list.
- **Local builds are the test.** A passing local build plus the quick CI checks is enough to merge. Don't wait on slow full CI builds.

## Writing

- **Docs are for humans first,** and still a precise reference for agents: short sections, tables for facts, commands that work. No history, finished plans, fixed bugs, speculation, comparisons with competitors' products or filler; git history has the story. One home per topic; link instead of repeating. Default to not documenting.
- **Why a workaround exists** goes in commit and patch messages, not user docs.
- **READMEs:** what it is, what users get, how to install or use it, the project table, a short developer part, the license. No implementation detail.
- **Comments** only for a non-obvious reason. Never restate the code.
- **Patches** (kernel, U-Boot) describe only what they change, in terms of that code: no project history, other patches or vendor-firmware findings. Generic patches carry no device-specific motivation; board patches say they're for the D200.
- **UI text:** the shortest plain wording; draft, then cut. Say "device", not "D200". Keep screens sparse. Docs about today's hardware still say D200.
- **Rescan:** before merging docs, have a reviewer that hasn't seen the work check them against these rules.

## Public repos

All three repos are public. Code, docs, comments, commits and PRs must never contain:
- local paths, usernames, hostnames or emails (the `From:` and `Signed-off-by` lines of patch files excepted);
- a real device's serial, IP or IPv6 address, MAC, hidraw node, or anything derived from them. Use the test-vector serial `00112233445566aa` in examples;
- details of anyone's PC, network or files, secrets, or app credentials;
- other companies' trademarks for our product (e.g. not "Stream Deck").

Grep the diff before every commit.

## Compatibility

Released versions must keep working together:
- New protocol fields and commands degrade gracefully: an older peer ignores them, and a newer one hides the feature when the other side doesn't report it.
- Use capability detection, not version numbers.
- deckhost tells the user to update when the device is newer than it.
- The site shows only what the connected device supports.

## Hardware

- **Two board variants:** RK3308H1 (128 MiB RAM) and RK3308H3 (256 MiB RAM). Their LCD panels differ; the firmware picks the panel config at boot (U-Boot env `panel` override, then a known panel ID read from the panel, then RAM size). Identify a display by what it reports, not by board guesses, wherever possible.
- **Debug builds** have SSH as root (`ssh root@d200-<serial>.local`) and `reboot-to maskrom` / `reboot-to fastboot`. Release builds have no shell, no SSH and no U-Boot prompt.
- **Agents drive the board** (flash, reboot, test) without asking; the maintainer only presses keys, replugs and watches the screen. Only one agent drives a board at a time. Run flashes in the background and keep timeouts on every step.
- **Reinstall with `d200-install.py`** from maskrom: it wipes the flash and installs. Don't take a dev board back to stock for side experiments.
- **Ask before** writing the device config or restarting deckd if someone else may be testing on it.

## Decided; don't re-open

- **USB ID** stays `1d6b:0104`; we can't get our own. Detection never relies on the ID alone.
- **Signed updates never block.** A signature only labels a bundle official or unofficial. Unsigned and fork bundles always install, with a warning; the press on the device is the gate. The signing key stays out of git.
- **First installs are hands-off.** No key holds, shorts or timing tricks; the tooling does the work. Recovery docs may use key combos.
- **The rootfs is UBIFS,** never squashfs. Keep the kernel slim.
- **Updates reboot once.** No SPL trial boot. SPL ships with every U-Boot bump.
- **macOS deckhost** stays ad-hoc signed (Open Anyway). No paid notarization, and no curl-to-shell install line.
- **PC integrations live in deckhost,** the existing per-user binary. No browser extensions, no reliance on extra software the user must install, and no accessibility-tree scraping.
- **Icons are site data,** never baked into Go binaries. Brand logos come only from GPL-compatible icon sets; we never design or redraw logos ourselves (converting an icon's format is fine).
- **Keys in BIOS/EFI aren't a goal;** waking the PC is (the udev rule enables wakeup on Linux).

## This repo: d200-deck

One Go module: `deckd` on the device, `deckhost` on the PC, and the packages the configuration website runs as WebAssembly. [docs/design.md](docs/design.md) maps every package and explains trust and app integrations; read it first.

### Layout

| Path | What |
|---|---|
| `cmd/deckd` | The device binary. With no arguments it runs and restarts `deckd keys` (`internal/deckd`) and `deckd update`, the update page (`internal/deckupdate`). Other subcommands print what the firmware build needs (`usb-net`, `raw-descriptor`, ...) |
| `cmd/deckhost` | The PC helper: daemon, tray, CLI and app integrations |
| `deck/`, `render/`, `icons/`, `fonts/` | Public packages the site builds with TinyGo. They pull in `internal/gfx` and `internal/svgpath`, so changes there reach the site too |
| `proto/` | The config channel protocol. The site implements it from [docs/api.md](docs/api.md) |
| `netid/` | USB network addresses and mDNS name from the serial; must match d200-buildroot byte for byte |
| `internal/` | Device-only and PC-only code |
| `contrib/` | Linux udev rule (`70-d200-deck.rules`, matches product `D200*`), `install.sh` and packaging; the macOS app script; the Windows NSIS installer |

### Build, test and lint

Make runs everything in a Go container (Docker or Podman); `DOCKER=` uses the host's Go. Plain `make` runs check, test, tinygo and build: run it before every PR.

| Target | Does |
|---|---|
| `make check` | gofmt, `go vet` (Linux, Windows for the PC packages, WASM for the site's), `go mod tidy -diff` |
| `make test` | `go test -race` with coverage, plus the site's packages with 32-bit ints |
| `make tinygo` | The site's packages' tests built with TinyGo, run in wasmtime |
| `make build` | `deckd` (arm64) and `deckhost` (Linux and Windows amd64) into `build/` |
| `make dist` | Linux .deb, .rpm and tarballs, and the Windows installer, into `build/dist` |
| `make test-macos`, `make dist-macos` | macOS only: host Go with cgo, no container |

CI: `test.yml` runs check, test and tinygo on Linux, and the macOS tests, app build and a start/stop of its deckhost on a Mac; `pr-title.yml` checks the PR title.

### Protocol and config changes

- [docs/api.md](docs/api.md) with `proto/` is the source of truth for the protocol, [docs/config.md](docs/config.md) with `deck/` for the config. Update the doc in the same PR as the code.
- Advertise new things in hello's `features` (`deck.Features()`: `fields`, `settings`, `commands`, ...), so older peers ignore them and newer ones hide what's missing. `protocol` changes only when old and new can't talk.
- After a change the site needs (new ops, commands, settings, config fields, default config), say what d200-configure must do. It gets the change after a deck release and its pin bump.

### deckd

- Memory is capped with `debug.SetMemoryLimit`: 30 MiB for the keys, 12 MiB for the update page, 4 MiB for the supervisor, on a 128 MiB device. Don't raise them; avoid new dependencies and compare `build/deckd`'s size before and after.
- Screens stay sparse: fold new info into existing keys and lines rather than adding tiles or screens. Show screen changes as a PNG: `go run ./cmd/deckd keys -preview screen.png` (add `-config <file>` for another config).
- The key layout lives only in `render` (`Width`, `Height`, `Key`) and `deck.KeyCount`. Use those, never literal coordinates or counts.

### deckhost

- A per-user program for Linux (amd64, arm64), Windows (amd64) and macOS (Apple Silicon, built on a Mac with cgo). It runs one session per connected device; tray and CLI commands pick a device by serial. See [docs/deckhost.md](docs/deckhost.md) and [docs/install.md](docs/install.md).
- It runs what the device config says; the press on the device is the gate ([design.md](docs/design.md#trust)).
- A new integration is an `app` in `cmd/deckhost` ([design.md](docs/design.md#app-integrations)) that talks to the app's local API from Go. App docs: [discord.md](docs/discord.md), [obs.md](docs/obs.md), [media.md](docs/media.md).
- Platform code goes in `_linux`, `_windows`, `_darwin` and `_other` files. `make check` vets Windows; macOS is tested on a Mac or in CI.
- App credentials (e.g. Discord's client secret) are the user's, kept in deckhost's settings. Never commit real ones.

### Testing on a device

Debug builds only, and ask first if someone else may be testing. The rootfs is read-only, so a test `deckd` runs from `/tmp` over a bind mount; a reboot reverts it. Logs go to syslog, tagged `deckd`; the config is `/data/deckd/config.json`.

```sh
make deckd
ssh -o ConnectTimeout=5 root@d200-<serial>.local '/etc/init.d/S60deckd stop && cat > /tmp/deckd && chmod +x /tmp/deckd &&
  { grep -q " /usr/bin/deckd " /proc/mounts || mount -o bind /tmp/deckd /usr/bin/deckd; } && /etc/init.d/S60deckd start' < build/deckd
```

### Releases

- Merges to `main` update the draft release (`release-drafter.yml`). The **Release** workflow (`release.yml`, inputs `version` and `dry_run`) tags it, builds and attaches the installers, publishes, then runs Renovate so d200-buildroot and d200-configure get pin PRs. Versioning: README's "Contributing and releases".
- `renovate.yml` is the Renovate for all three repos, run weekly, by hand and after each release as the project's GitHub App. Each repo's `renovate.json` says what it updates.
- Release only with the user's OK. The other repos pin deck by release tag, never by a branch or commit.
