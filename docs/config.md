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
  "icons": {
    "public": {"d": "M480-80q-83 0-156-31.5T197-197...Z", "viewBox": "0 -960 960 960"},
    "play-pause": {"d": "M8 22L8 78L50 50Z...", "viewBox": "0 0 100 100"},
    "back": {"d": "...", "viewBox": "0 -960 960 960"},
    "refresh": {"d": "...", "viewBox": "0 -960 960 960"}
  },
  "images": {"logo": "<base64 PNG, JPEG or GIF>"},
  "fonts": {"brand": "<base64 TTF or OTF>"}
}
```

| Field | |
|---|---|
| `version` | `1` |
| `home` | the page shown at start |
| `pages` | pages by name; each has `keys`, buttons by key number. Keys without one are blank. |
| `icons` | the icons keys use, by name: `d` (SVG path data, filled nonzero, drawn in the key's `color`) and `viewBox`, or `png` (base64 PNG or GIF, drawn as it is, e.g. an emoji) |
| `images` | custom images by ID, base64 PNG, JPEG or GIF; one alone fills the key. GIFs animate. |
| `fonts` | custom fonts by ID, base64 TTF or OTF. An ID can't be a built-in font's name. |

## Keys

Numbered in reading order. On the D200 (hello says `"keys": 14`):

```
 0  1  2  3  4
 5  6  7  8  9
10 11 12 [ 13 ]
```

Key 13 is the wide one. The `render` package draws keys as the deck does; the configuration website runs it as
WebAssembly for its previews.

## Buttons

| Field | |
|---|---|
| `icon` | an icon in `icons`, by name |
| `image` | a custom image's ID, instead of `icon` |
| `label` | text, up to 32 characters; up to 3 lines (`\n`) on a key without an icon |
| `font` | the label's font: a custom font's ID, or `go-regular`, `go-medium` (default), `go-bold`, `go-italic`, `go-mono`, ... (`deckd fonts`) |
| `color`, `background` | `#rrggbb`; the defaults are light grey on dark grey |
| `action` | what the key does (below) |
| `on` | another look (`icon`, `image`, `label`, `font`, `color`, `background`) while the action's state is on, e.g. while Discord is muted. Discord mute and deafen have a built-in one. |

A button shows an icon, a label, or both.

Every icon a key can show must be in `icons`, including the built-in looks of app keys (for
example Discord mute shows `mic`, and `mic_off` while muted). The
[configuration website](https://riptidewave93.github.io/d200-configure/) adds them; the deck refuses to save a config that's missing any.

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
| `launch` | PC | `command`: the program and its arguments, e.g. `["code", "--new-window"]`; no shell, so `~` and `$VARS` aren't expanded |
| `http` | PC | `method`, `url`, `headers`, `body` |
| `app` | PC | `app`, `op`, `args` (an object): see [App integrations](design.md#app-integrations), [Discord](discord.md), [OBS](obs.md) and [media](media.md) |

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
| Font | 512 KiB; `fonts.Subset` cuts a TrueType font to the characters labels use |
| Icons | 256; a path up to 16 KiB, a PNG or GIF up to 24 KiB and 128x128 px |
| GIF | 100 frames and 4 M pixels in all (frames x width x height); frames show for 50 ms at least. A page animates up to 8 MiB of frames as drawn, a byte a pixel (4 for a GIF of more than 256 colours in all): a full-key GIF of 100 frames takes 3.8 MiB. Past that, the rest show their first frame. Devices without the `anim_bytes` limit animate 2 M pixels, 4 bytes each. |
| One PC action | 8 KiB as JSON |
