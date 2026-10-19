#!/bin/sh
# Installs deckhost for the current user: the binary, the daemon's systemd
# user unit and the tray icon's autostart entry. Usage: install.sh [deckhost]
set -eu
src=$(dirname "$0")
bin=$HOME/.local/bin/deckhost

install -Dm755 "${1:-deckhost}" "$bin"
install -Dm644 "$src/deckhost.service" "$HOME/.config/systemd/user/deckhost.service"
mkdir -p "$HOME/.config/autostart"
sed "s|@BIN@|$bin|" "$src/deckhost-tray.desktop" > "$HOME/.config/autostart/deckhost-tray.desktop"

systemctl --user daemon-reload
systemctl --user enable deckhost.service
systemctl --user restart deckhost.service
pkill -x -u "$(id -u)" -f "$bin tray" 2>/dev/null || true
setsid "$bin" tray >/dev/null 2>&1 &

if [ ! -e /etc/udev/rules.d/70-d200-deck.rules ]; then
	echo "If deckhost can't open the D200, install the udev rule once (needs root):"
	echo "  sudo install -m644 $src/70-d200-deck.rules /etc/udev/rules.d/ && sudo udevadm trigger"
fi
