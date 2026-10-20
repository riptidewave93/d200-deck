# Media players

Media keys control the PC's media players (Spotify, browsers, VLC and others) through `deckhost`,
and show whether they're playing, shuffling or repeating. Without `deckhost` they still work, as
the plain media keys.

| PC | How |
|---|---|
| Linux | MPRIS, which most players support |
| Windows 10 1809 or later | the system media controls (the ones in the volume flyout) |

## Keys

```json
"0": { "action": { "type": "app", "app": "media", "op": "play-pause" } },
"1": { "action": { "type": "app", "app": "media", "op": "next", "args": { "id": "spotify" } } }
```

Without `args.id`, a key is for the player that's playing, or else the last one that was. With it,
for the player whose name starts with `id`. The tray menu's Media line lists the players' names,
such as `spotify, firefox (playing)`.

| `op` | Does | Built-in look | Without deckhost |
|---|---|---|---|
| `play-pause` | Plays or pauses | `play_arrow`; `pause` while playing | the Play/Pause key |
| `next` | Next track | `skip_next` | the Next key |
| `previous` | Previous track | `skip_previous` | the Previous key |
| `stop` | Stops | `stop` | the Stop key |
| `shuffle` | Turns shuffle on or off | `shuffle`; `shuffle_on` while on | — |
| `repeat` | Turns repeating the playlist on or off | `repeat`; `repeat_on` while on | — |
| `now-playing` | Plays or pauses | the song (see below); blank with no song or no deckhost | the Play/Pause key |

A key that sets no `icon`, `image` or `label` gets the built-in look. Not every player supports
shuffle and repeat.

The default config has a `now-playing` key next to the media keys. `deckhost` reads the song and
its art from the player (on Linux, the art URL MPRIS gives, a file or http(s); on Windows, the
system's thumbnail), scales the art to fill the key and sends it once per song.

A `now-playing` key's `args.show` picks what it shows:

| `show` | Shows |
|---|---|
| `art` (default) | the album art, or the title and artist if the song has none |
| `text` | the title and artist |
| `both` | the art and the title and artist in turn, `args.cycle` seconds each (1 to 60, default 3) |

```json
"4": { "action": { "type": "app", "app": "media", "op": "now-playing", "args": { "show": "both", "cycle": 5 } } }
```
