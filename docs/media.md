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

A key that sets no `icon`, `image` or `label` gets the built-in look. Not every player supports
shuffle and repeat.
