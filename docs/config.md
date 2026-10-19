# Config

The keys' setup: named pages of buttons, in one JSON file, images and fonts included. The deck
keeps it at `/data/deckd/config.json`; read and replace it over USB with the
[config commands](api.md#the-config). Without one, or with a broken one, the deck uses its default
(`deckd default-config` prints it).

```json
{
  "version": 1,
  "home": "main",
  "pages": {
    "main": {
      "keys": {
        "0": {"icon": "public", "label": "Browser", "action": {"type": "page", "page": "browser"}},
        "1": {"icon": "play-pause", "action": {"type": "hid-consumer", "usage": "play-pause"}},
        "2": {"label": "BRB", "background": "#c62828", "action": {"type": "type-text", "text": "brb"}}
      }
    },
    "browser": {
      "keys": {
        "0": {"icon": "back", "label": "Back", "action": {"type": "back"}},
        "1": {"icon": "refresh", "label": "Reload", "action": {"type": "hid-key", "keys": ["ctrl", "r"]}}
      }
    }
  },
  "images": {"logo": "<base64 PNG or JPEG>"},
  "fonts": {"brand": "<base64 TTF or OTF>"}
}
```

| Field | |
|---|---|
| `version` | `1` |
| `home` | the page shown at start |
| `pages` | pages by name; each has `keys`, buttons by key number. Keys without one are blank. |
| `images` | custom images by ID, base64 |
| `fonts` | custom fonts by ID, base64 |

## Keys

Numbered in reading order. On the D200 (hello says `"keys": 14`):

```
 0  1  2  3  4
 5  6  7  8  9
10 11 12 [ 13 ]
```

Key 13 is the wide one. The `render` package draws keys as the deck does; it builds for WebAssembly.

## Buttons

| Field | |
|---|---|
| `icon` | a built-in icon: any [Material Symbols](https://fonts.google.com/icons) name, such as `volume_up`, or `play-pause`, `next`, `prev`, `mute`, `volume-up`, `volume-down`, `github`, `discord`. `deckd icons` lists them all, and `deckd icons-json` gives them as SVG. |
| `image` | a custom image's ID, instead of `icon` |
| `label` | text, up to 32 characters; up to 3 lines (`\n`) on a key without an icon |
| `font` | the label's font: a custom font's ID, or `go-regular`, `go-medium` (default), `go-bold`, `go-italic`, `go-mono`, ... (`deckd fonts`) |
| `color`, `background` | `#rrggbb`; the defaults are light grey on dark grey |
| `action` | what the key does (below) |
| `on` | another look (`icon`, `image`, `label`, `font`, `color`, `background`) while the action's state is on, e.g. while Discord is muted. Discord mute and deafen have a built-in one. |

A button shows an icon, a label, or both.

## Actions

| `type` | Runs on | Fields |
|---|---|---|
| `hid-key` | deck | `keys`: held while the key is held, e.g. `["ctrl", "shift", "m"]` |
| `hid-consumer` | deck | `usage`: `play-pause`, `next-track`, `prev-track`, `mute`, `volume-up`, `volume-down`, or a number |
| `hid-system` | deck | `system`: `sleep`, `power-down`, `wake-up` |
| `type-text` | deck | `text`: up to 1024 characters, ASCII, typed on a US layout |
| `macro` | deck | `steps`: deck actions, and `{"type": "delay", "ms": 500}`; up to 256. Press again to stop it. |
| `page` | deck | `page`: a page name. A page already open is gone back to. |
| `back`, `home` | deck | none |
| `open-url` | PC | `url`: http or https |
| `launch` | PC | `command`: the program and its arguments, e.g. `["code", "~/notes"]` |
| `http` | PC | `method`, `url`, `headers`, `body` |
| `app` | PC | `app`, `op`, `args` (an object): see [App integrations](design.md#app-integrations) and [Discord](discord.md) |

PC actions need `deckhost` on the PC. Without it the key flashes red, unless it has a `fallback`:
a deck action to run instead, such as a keyboard shortcut.

**Key names** for `hid-key`: `a`-`z`, `0`-`9`, `f1`-`f24`, `enter`, `esc`, `backspace`, `tab`,
`space`, `minus`, `equal`, `leftbrace`, `rightbrace`, `backslash`, `semicolon`, `apostrophe`,
`grave`, `comma`, `dot`, `slash`, `capslock`, `printscreen`, `scrolllock`, `pause`, `insert`,
`home`, `pageup`, `delete`, `end`, `pagedown`, `up`, `down`, `left`, `right`, `menu`; modifiers
`ctrl`, `shift`, `alt`, `gui` (also `win`, `cmd`, `super`) and right-hand `rctrl`, `rshift`,
`ralt`, `rgui`. Up to 6 keys plus modifiers.

F13-F24 make good shortcuts for apps: nothing else uses them, so bind them in Discord, OBS and
so on.

## Limits

| | |
|---|---|
| Config file | 2 MiB, images and fonts included |
| Image | 256 KiB and 512x512 px; keys are 196 px, so size them to that |
| Font | 512 KiB |
| One PC action | 8 KiB as JSON |
