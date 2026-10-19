# OBS Studio

D200 keys control [OBS Studio](https://obsproject.com) 28 or later: switch scenes, start and stop
streaming, recording, the replay buffer and the virtual camera, and mute audio sources. Keys show
the state, such as which scene is live. This works on Linux and Windows, through `deckhost`.

## Setup

1. In OBS, open **Tools > WebSocket Server Settings**, tick **Enable WebSocket server** and choose
   **Apply**.
2. If **Enable Authentication** is ticked (the default), choose **Show Connect Info**, copy the
   password and put it in `obs.json`:

**Linux:**

```bash
mkdir -p ~/.config/deckhost
(umask 077; cat > ~/.config/deckhost/obs.json) <<'END'
{"password": "YOUR OBS PASSWORD"}
END
```

**Windows** (PowerShell):

```powershell
New-Item -ItemType Directory -Force "$env:APPDATA\deckhost" | Out-Null
Set-Content "$env:APPDATA\deckhost\obs.json" '{"password": "YOUR OBS PASSWORD"}'
```

Within 5 seconds the tray menu shows **OBS: connected**. For OBS on another PC or port, add
`"url": "ws://HOST:PORT"` (the default is `ws://127.0.0.1:4455`).

## Keys

```json
"0": { "label": "Gaming", "action": { "type": "app", "app": "obs", "op": "scene", "args": { "id": "Gaming" } } },
"1": { "action": { "type": "app", "app": "obs", "op": "record" } },
"2": { "action": { "type": "app", "app": "obs", "op": "mute", "args": { "id": "Mic/Aux" } } }
```

| `op` | Does | Built-in look |
|---|---|---|
| `scene` | Switches to scene `args.id` (its name in OBS) | `tv`; green while live |
| `stream` | Starts or stops streaming | `cell_tower` "Stream"; red "Live" while streaming |
| `record` | Starts or stops recording | `radio_button_checked` "Record"; red "REC" while recording |
| `pause-record` | Pauses or resumes the recording | `pause_circle` "Pause"; red `play_circle` "Resume" while paused |
| `replay-buffer` | Starts or stops the replay buffer | `replay` "Replay"; green while on |
| `save-replay` | Saves the replay buffer | `save` "Clip" |
| `virtual-cam` | Starts or stops the virtual camera | `videocam` "Cam"; green while on |
| `mute` | Mutes or unmutes audio source `args.id` (its name in OBS) | `mic`; red `mic_off` while muted |

A key that sets no `icon`, `image` or `label` gets the built-in look. A key flashes red when the
press didn't work, such as a scene that doesn't exist; `deckhost`'s log says why.

## Troubleshooting

| Tray says | Means |
|---|---|
| no OBS line | OBS isn't running (or its WebSocket server is off) and there's no `obs.json` |
| OBS: not running | OBS isn't running, or its WebSocket server is off; `deckhost` checks every 5 s |
| OBS: needs its WebSocket password | Put the password in `obs.json` (step 2) |
| OBS: connected | Working |
