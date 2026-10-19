# Discord

Discord is the first app `deckhost` integrates with. The aim is to match the Stream Deck plugin and
add Linux, which it lacks. Nothing is built yet.

## What the Stream Deck plugin does

Elgato's [Discord plugin](https://marketplace.elgato.com/product/discord-e3559f36-d31d-4529-ab1f-739955e2ac7a)
(v2.4, Windows and macOS):

- Mute, deafen, push to talk, push to mute
- Join/leave a voice channel, jump to a text channel
- Camera toggle, Go Live toggle
- Since [2.0](https://alternativeto.net/news/2023/5/stream-deck-x-discord-elgato-introduces-improved-discord-plugin-for-stream-deck-offering-enhanced-control):
  soundboard clips (Nitro), switch audio input/output device, voice activity / push to talk mode,
  per-user volume, unread message count, server stats

Keys show live state (muted, deafened, in a channel), not just a fixed icon.

## How it works

The Discord desktop app has a local [RPC socket](https://docs.discord.com/developers/topics/rpc)
(`discord-ipc-N` in `$XDG_RUNTIME_DIR` on Linux, a named pipe on Windows). So the work is in
`deckhost`, not on the device. Mute and deafen are `SET_VOICE_SETTINGS`; live state comes from
`VOICE_SETTINGS_UPDATE`, `VOICE_CHANNEL_SELECT` and voice state events.

Authorisation is OAuth2: `AUTHORIZE` (Discord shows a prompt), exchange the code for a token with
the app's client secret, then `AUTHENTICATE`.

**The catch:** changing voice settings needs the `rpc.voice.write`
[scope](https://docs.discord.com/developers/topics/oauth2), which only Discord-approved partners
get. An unapproved app works only for up to 50 testers it lists. Open source plugins
([example](https://github.com/fredemmott/StreamDeck-Discord)) have each user create their own
Discord application and paste in its client ID and secret. An open source project can't keep a
shared secret.

## Plan

1. Protocol: let the PC set a key's state on the device (for the live display). Do this together
   with the multi-client fixes to the config channel.
2. `deckhost`: mute, deafen, push to talk/mute and voice channel with live state, using the
   user's own Discord application.
3. Ask Discord for approved-partner status, so users can skip creating an application.
4. Then the rest of the list above.

Confirmed: a user's own unapproved application can read and change its owner's voice settings
and gets `VOICE_SETTINGS_UPDATE` events (tested on Linux with Flatpak Discord).

## Setup (for now)

1. At <https://discord.com/developers/applications>, create an application. Under OAuth2, copy
   the client ID, reset and copy the client secret, and add the redirect `http://localhost`.
2. Save them, readable only by you, in `~/.config/deckhost/discord.json`:
   `{"client_id":"…","client_secret":"…"}`
3. `deckhost discord-test` connects, asks you to authorize the app in Discord the first time
   (the token is kept in `discord-token.json` next to it), and toggles mute on and off.

`internal/discord` finds Discord's socket for native, Flatpak
(`$XDG_RUNTIME_DIR/app/com.discordapp.Discord/`) and Snap installs.
