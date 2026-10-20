# Third-party notices

d200-deck is licensed under the GNU GPL v3 or later ([LICENSE](LICENSE)). It includes or links:

| Component | Where | Licence |
|---|---|---|
| [Material Symbols](https://github.com/google/material-design-icons) (Rounded, filled) icon paths, Copyright Google LLC | `icons/symbols.go`; the ones the device uses itself in `deck/default_icons.go` and `internal/deckd/system_icons.go` | Apache-2.0 ([LICENSES/Apache-2.0.txt](LICENSES/Apache-2.0.txt)) |
| GitHub mark from [Primer Octicons](https://github.com/primer/octicons), Copyright GitHub Inc. | `icons/marks.go`, `deck/default_icons.go` | MIT ([LICENSES/MIT-primer-octicons.txt](LICENSES/MIT-primer-octicons.txt)) |
| Discord symbol from [Discord's brand assets](https://discord.com/branding), Copyright Discord Inc. | `icons/marks.go`, `deck/default_icons.go` | used under Discord's brand guidelines |
| OBS Studio logo from [Simple Icons](https://simpleicons.org) (`obsstudio`) | `icons/marks.go`, `deck/default_icons.go` | CC0-1.0 |
| [golang.org/x/image](https://pkg.go.dev/golang.org/x/image), including the Go fonts | Go module | BSD-3-Clause (in the module) |
| [golang.org/x/text](https://pkg.go.dev/golang.org/x/text) (used by x/image) | Go module | BSD-3-Clause (in the module) |
| [fyne.io/systray](https://github.com/fyne-io/systray) (deckhost tray icon) | Go module | Apache-2.0 (in the module) |
| [godbus/dbus](https://github.com/godbus/dbus) (deckhost's tray and media keys on Linux) | Go module | BSD-2-Clause (in the module) |
| [coder/websocket](https://github.com/coder/websocket) (deckhost's OBS keys) | Go module | ISC (in the module) |
| Go standard library | | BSD-3-Clause |

The GitHub logo is a trademark of GitHub, Inc., used here only to link to this project's repository.
Discord and the Discord symbol are trademarks of Discord Inc., used here only to label keys that
control the Discord app; this project isn't affiliated with or endorsed by Discord. OBS Studio and
its logo are named and shown only to label keys that control OBS; this project isn't affiliated
with or endorsed by the OBS Project.
