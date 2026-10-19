# Installing deckhost

Download the installer from [Releases](https://github.com/riptidewave93/d200-deck/releases);
`SHA256SUMS` there checks them. Pushing a `v*` tag builds them into a draft release
(`.github/workflows/release.yml`), and `make dist` builds them into `build/dist` (in Docker).

## Linux

**Debian, Ubuntu, Mint** and **Fedora Workstation** (amd64 or arm64):

```bash
sudo apt install ./deckhost_<version>_amd64.deb
sudo dnf install ./deckhost-<version>.x86_64.rpm
```

The package installs `/usr/bin/deckhost`, the udev rule that lets you open the D200, the daemon
as a systemd user service (enabled for every user) and the tray's autostart entry. Log out and in,
or start them now: `systemctl --user start deckhost.service && deckhost tray &`.

**Atomic distros** (Fedora Silverblue, Bluefin, Bazzite, SteamOS) and anything else: unpack
`deckhost-<version>-linux-<arch>.tar.gz` and run `./install.sh` in it. It installs for your user
(`~/.local/bin`) and starts deckhost, then offers to install the udev rule with `sudo` (needed
once). `./install.sh --uninstall` removes it.

On GNOME the tray icon needs the
[AppIndicator extension](https://extensions.gnome.org/extension/615/appindicator-support/)
(Ubuntu and Bluefin ship it; stock Fedora doesn't).

## Windows

Run `deckhost-<version>-windows-amd64-setup.exe` (it asks for admin rights; no driver is
needed). It installs to `C:\Program Files\D200 Deck` for every user, with a Start menu entry and
autostart at login; deckhost itself runs as each user, and the tray starts the daemon. Uninstall
it from Settings > Apps.

The installer isn't signed yet, so SmartScreen warns: choose **More info > Run anyway**. Run it
from a local disk: started from a network share or a VM's shared folder, Windows often can't run it
as admin.

The daemon logs to `%LOCALAPPDATA%\deckhost\deckhost.log`; settings (such as the Discord app)
are in `%APPDATA%\deckhost` and stay when you uninstall.

## Discord

Discord keys need a one-time setup on each PC: see [discord.md](discord.md#setup).
