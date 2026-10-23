# Microphone

Mic keys mute the PC's microphone for every app at once, such as Zoom, Google Meet, Teams or
Discord, and show whether it's muted. They work whichever app is in front. They need `deckhost`.

| PC | How |
|---|---|
| Linux | the default input of PipeWire or PulseAudio |
| Windows | the default recording device, and the default communications one if it differs |
| macOS | the default input device; one without a mute control is turned down to 0 and back |

The call app doesn't know: it still shows you as unmuted, but no one hears you. To use the app's
own mute, give a key its shortcut instead, such as Alt+A in Zoom or Ctrl+D in Meet (the app or tab
must be in front).

## Keys

```json
"0": { "action": { "type": "app", "app": "mic", "op": "mute" } }
```

| `op` | Does | Built-in look |
|---|---|---|
| `mute` | Mutes or unmutes | `mic`; `mic_off` on red while muted |
| `push-to-talk` | Unmutes while the key is held, then mutes | `record_voice_over` |

A key that sets no `icon`, `image` or `label` gets the built-in look. The tray menu's Mic line
shows **on**, **muted** or **no microphone**. Muting from the PC, such as from its sound settings,
shows on the key within a second.
