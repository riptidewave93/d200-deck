#!/bin/sh
# Installs deckhost for the current user, for distros without the .deb/.rpm
# (or where packages are awkward, e.g. Fedora Silverblue, Bluefin, SteamOS):
# the binary, the daemon's systemd user unit and the tray icon's autostart
# entry, then the udev rule that lets you open the D200 (asks for sudo once).
#
#   ./install.sh               install or update
#   ./install.sh --uninstall   remove (the udev rule stays; see below)
set -eu
src=$(cd "$(dirname "$0")" && pwd)
bin=$HOME/.local/bin/deckhost
unit=$HOME/.config/systemd/user/deckhost.service
autostart=$HOME/.config/autostart/deckhost-tray.desktop
rule=/etc/udev/rules.d/70-d200-deck.rules

stop() {
	systemctl --user disable --now deckhost.service 2>/dev/null || true
	pkill -x -u "$(id -u)" deckhost 2>/dev/null || true
}

if [ "${1:-}" = --uninstall ]; then
	stop
	rm -f "$bin" "$unit" "$autostart"
	systemctl --user daemon-reload
	echo "deckhost removed. To remove the udev rule too: sudo rm $rule"
	exit 0
fi

stop
install -Dm755 "$src/deckhost" "$bin"
mkdir -p "$(dirname "$unit")" "$(dirname "$autostart")"
sed "s|@BIN@|$bin|" "$src/deckhost.service" > "$unit"
sed "s|@BIN@|$bin|" "$src/deckhost-tray.desktop" > "$autostart"
systemctl --user daemon-reload
systemctl --user enable --now deckhost.service
setsid "$bin" tray >/dev/null 2>&1 &
echo "deckhost installed: the daemon is running and the tray icon starts at login."

if [ -e "$rule" ] || [ -e /usr/lib/udev/rules.d/70-d200-deck.rules ]; then
	exit 0
fi
cmd="sudo install -m644 '$src/70-d200-deck.rules' $rule && sudo udevadm control --reload-rules && sudo udevadm trigger --subsystem-match=hidraw --action=change"
echo
echo "deckhost needs a udev rule (once, as root) to open the D200:"
echo "  $cmd"
if [ -t 0 ]; then
	printf "Install it now? [Y/n] "
	read -r answer
	case "$answer" in
	[nN]*) ;;
	*) sh -c "$cmd" && echo "udev rule installed." ;;
	esac
fi
