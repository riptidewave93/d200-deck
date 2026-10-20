# Discord

D200 keys control the Discord desktop app: mute, deafen, push to talk, join and leave voice
channels, voice mode, noise suppression and volume, and open Discord or a channel. Keys show the
state, such as whether you're muted. This works on Linux and Windows, through `deckhost`.

Until it's set up, mute and deafen keys still work: they send Discord's keyboard shortcuts
instead (see [Without deckhost](#without-deckhost)).

## Setup

It takes about five minutes, once. You need the Discord desktop app (not Discord in a browser) and
[deckhost installed](install.md).

Discord only lets apps it has approved control your voice settings, apart from apps you create
yourself. So you create a small Discord application of your own and give `deckhost` its ID and
secret. It's free, it can only reach your own account, and nobody else sees it.

### 1. Create a Discord application

1. Open the [Discord Developer Portal](https://discord.com/developers/applications) and log in
   with the Discord account you use in the app.
2. Choose **New Application**, name it (e.g. `D200 Deck`; you'll see this name when Discord asks
   you to allow it), accept the terms and choose **Create**.
3. Open **OAuth2** in the menu on the left:
   - Copy the **Client ID**.
   - Under **Client Secret**, choose **Reset Secret** and copy the new secret. Discord shows it
     only once; if you lose it, reset it again.
   - Under **Redirects**, choose **Add Redirect**, enter `http://localhost` and choose
     **Save Changes**.

Nothing else on the portal needs changing. Keep the secret to yourself, like a password.

### 2. Give deckhost the ID and secret

Put both in a file named `discord.json`. Replace the two placeholders in the command for your
system and run it.

**Linux** (a terminal):

```bash
mkdir -p ~/.config/deckhost
(umask 077; cat > ~/.config/deckhost/discord.json) <<'EOF'
{"client_id": "YOUR CLIENT ID", "client_secret": "YOUR CLIENT SECRET"}
EOF
```

**Windows** (PowerShell):

```powershell
New-Item -ItemType Directory -Force "$env:APPDATA\deckhost" | Out-Null
Set-Content "$env:APPDATA\deckhost\discord.json" '{"client_id": "YOUR CLIENT ID", "client_secret": "YOUR CLIENT SECRET"}'
```

### 3. Allow it in Discord

Within a few seconds, with Discord open, Discord asks whether to let your application control
your voice settings. Choose **Authorize**. The tray menu then shows **Discord: connected**.

`deckhost` remembers this (in `discord-token.json` next to `discord.json`), so Discord asks only
once. To check the setup by hand, `deckhost discord-test` logs in, mutes you for 3 seconds and
unmutes you.

## Keys

Add Discord keys to the device's config:

```json
"0": { "action": { "type": "app", "app": "discord", "op": "mute" } },
"1": { "action": { "type": "app", "app": "discord", "op": "deafen" } },
"2": { "icon": "mic", "label": "Talk", "action": { "type": "app", "app": "discord", "op": "push-to-talk" } },
"3": { "icon": "mic", "action": { "type": "app", "app": "discord", "op": "mute" },
       "on": { "icon": "mic_off", "background": "#6a1b9a" } },
"4": { "label": "Lobby", "action": { "type": "app", "app": "discord", "op": "join-voice",
                                     "args": { "id": "123456789012345678" } } }
```

| `op` | Does | Built-in look | Without deckhost |
|---|---|---|---|
| `mute` | Toggles mute; while deafened, undeafens (as Discord's mic button does) | `mic`; while muted or deafened (whichever way), red `mic_off` | Ctrl+Shift+M |
| `deafen` | Toggles deafen | `headphones`; while deafened, red `headset_off` | Ctrl+Shift+D |
| `push-to-talk` | Unmutes while held | `record_voice_over` | its `fallback`, if set |
| `push-to-mute` | Mutes while held | `voice_over_off` | its `fallback`, if set |
| `open` | Opens Discord, or brings it to the front | the Discord symbol | — |
| `channel` | Opens a text channel (`args`: `id` and the server's `guild`) or a DM (`id` only) | `tag` | — |
| `join-voice` | Joins voice channel `args.id`; in it already, leaves | `headset_mic`; green while in that channel | — |
| `leave-voice` | Leaves the voice channel | `call_end`; red while in a voice channel | its `fallback`, if set |
| `voice-mode` | Switches between voice activity and push to talk | `graphic_eq` "Voice"; `keyboard` "PTT" in push to talk | — |
| `noise-suppression`, `echo-cancellation`, `auto-gain` | Toggles it | "Noise", "Echo", "Gain"; lit while on | — |
| `input-volume-up`, `-down` | Mic volume by `args.step` (default 10, of 100) | `mic` "Mic +" / "Mic −" | — |
| `output-volume-up`, `-down` | Output volume by `args.step` (default 10, of 200) | `volume_up` / `volume_down` | — |

`open` and `channel` work whenever `deckhost` runs, even before Discord is set up. For IDs, turn
on **Settings > Advanced > Developer Mode** in Discord, then right-click a channel, server or user
and choose **Copy ID**.

A key that sets no `icon`, `image` or `label` gets the built-in look: a white icon on Discord's
Blurple (`#5865f2`). Set your own fields to change it, and `on` for how it looks while on.

The `discord` icon is the Discord symbol, e.g. for a folder key:
`{ "icon": "discord", "label": "Discord", "background": "#5865f2", "action": { "type": "page", "page": "discord" } }`.
It's white unless the key sets `color`. Discord's [brand rules](https://discord.com/branding) allow
it only in white, black or Blurple (`#5865f2`).

A Discord key flashes red when the press didn't work. The tray menu's Discord line says why.

Discord gives apps no control of the camera, Go Live or the soundboard: give those a keybind in
Discord and use an `hid-key` key.

### Without deckhost

While `deckhost` isn't connected to Discord (not running, Discord not running, not set up), a
Discord key with a `fallback` runs that on the D200 instead: any action the
device can run itself, usually a keyboard shortcut set in Discord's **Settings > Keybinds**.
Mute and deafen fall back to Discord's default shortcuts unless they set their own. The key
switches back on its own when `deckhost` connects; it shows whether you're muted only then.

```json
"4": { "action": { "type": "app", "app": "discord", "op": "push-to-talk",
                   "fallback": { "type": "hid-key", "keys": ["ctrl", "shift", "f14"] } } }
```

The shortcut goes to whichever app has focus, and Discord only sees it if it's running.

## Troubleshooting

| Tray says | Means |
|---|---|
| no Discord line | `discord.json` isn't there: see [step 2](#2-give-deckhost-the-id-and-secret) |
| Discord: not running | `deckhost` can't find the Discord desktop app; it checks every 5 s |
| Discord: not logged in | You refused, didn't answer in 2 minutes, or `discord.json` is wrong (the log says which). Fix it if needed, then choose **Restart** in the tray menu to try again |
| Discord: connected | Working |

On Linux the log is `journalctl --user -u deckhost`; on Windows it's
`%LOCALAPPDATA%\deckhost\deckhost.log`. Discord installed as a Flatpak or Snap works too.

If you reset the client secret, update `discord.json`.

On GNOME (Wayland), **Open** and channel keys open Discord but may not bring it to the front: see
[window focus on GNOME](deckhost.md#window-focus-on-gnome-wayland).

## Turning it off

In Discord, **User Settings > Authorized Apps** and **Deauthorize** your application. Delete
`discord.json` and `discord-token.json`, and delete the application on the Developer Portal if
you like.
