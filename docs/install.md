# Installing deckhost

Download the installer for your system from
[Releases](https://github.com/riptidewave93/d200-deck/releases); `SHA256SUMS` there checks them.
To update, install the new one the same way.

## Linux

**Debian, Ubuntu, Mint, Fedora Workstation** (amd64 or arm64):

```bash
sudo apt install ./deckhost_<version>_amd64.deb
sudo dnf install ./deckhost-<version>-1.x86_64.rpm
```

Then log out and in, or start it now from **D200 Deck** in the app launcher.

**Any other distro**, including atomic ones (Silverblue, Bluefin, Bazzite, SteamOS): unpack
`deckhost-<version>-linux-<arch>.tar.gz` and run `./install.sh` in it. It installs deckhost for
your user, adds **D200 Deck** to the app launcher and starts it. Once, it also needs a udev rule so
you can open the D200: it prints the root command and offers to run it with `sudo` or `doas`.
`./install.sh --uninstall` removes deckhost.

From a checkout of this repo, `make install` builds deckhost and installs it the same way.

On GNOME the tray icon needs the
[AppIndicator extension](https://extensions.gnome.org/extension/615/appindicator-support/)
(Ubuntu and Bluefin ship it; stock Fedora doesn't). deckhost works without it.
If your desktop doesn't start autostart entries (some window managers), start `deckhost tray` at
login.

The log is `journalctl --user -u deckhost`, or `$XDG_RUNTIME_DIR/deckhost.log` on systems without
systemd.

## Windows

Run `deckhost-<version>-windows-amd64-setup.exe` (it asks for admin rights; no driver is needed).
It adds **D200 Deck** to the Start menu and starts it at login. Uninstall it from Settings > Apps.

The installer isn't signed yet, so SmartScreen warns: choose **More info > Run anyway**. Run it
from a local disk; from a network share or a VM's shared folder, Windows often can't run it as
admin.

The log is `%LOCALAPPDATA%\deckhost\deckhost.log`; settings (such as the Discord app) are in
`%APPDATA%\deckhost` and stay when you uninstall.

## Discord

Discord keys need a one-time setup on each PC: see [discord.md](discord.md#setup).
