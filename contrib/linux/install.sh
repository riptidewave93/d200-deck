#!/bin/sh
# Installs deckhost for the current user on any Linux, for distros without the
# .deb/.rpm or where packages are awkward (atomic distros, SteamOS, Alpine...):
# the binary, the daemon's systemd user unit (where systemd runs), the tray
# icon's autostart entry (the tray starts the daemon if systemd doesn't), an
# app launcher entry with its icon, then the udev rule that lets you open the
# D200 and lets it wake the PC (as root, when the rule changes).
#
#   ./install.sh               install or update
#   ./install.sh --uninstall   remove (the udev rule stays; see below)
set -eu
src=$(cd "$(dirname "$0")" && pwd)
cfg=${XDG_CONFIG_HOME:-$HOME/.config}
bin=$HOME/.local/bin/deckhost
unit=$cfg/systemd/user/deckhost.service
autostart=$cfg/autostart/deckhost-tray.desktop
data=${XDG_DATA_HOME:-$HOME/.local/share}
app=$data/applications/deckhost.desktop
icon=$data/icons/hicolor/scalable/apps/deckhost.svg
rule=/etc/udev/rules.d/70-d200-deck.rules

systemd=
if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
	systemd=1
fi

# Stops the daemon and the tray, however they were started.
stop() {
	if [ -n "$systemd" ]; then
		systemctl --user stop deckhost.service 2>/dev/null || true
	fi
	stopped=
	for p in /proc/[0-9]*; do
		case $(readlink "$p/exe" 2>/dev/null) in
		"$bin" | "$bin (deleted)") kill "${p#/proc/}" 2>/dev/null && stopped=1 ;;
		esac
	done
	if [ -n "$stopped" ]; then
		sleep 1
	fi
}

# put installs file $1 as $2 with mode $3 and @BIN@ replaced, unless that's
# already there, and sets changed if it wasn't. It renames a hidden temporary
# file into place: GNOME Shell reloads its app menu on any change to the
# applications folder, and can crash on a partly written entry.
put() {
	mkdir -p "$(dirname "$2")"
	tmp=$(mktemp "$(dirname "$2")/.deckhost.XXXXXX")
	sed "s|@BIN@|$bin|" "$1" >"$tmp"
	if cmp -s "$tmp" "$2"; then
		rm -f "$tmp"
	else
		chmod "$3" "$tmp"
		mv -f "$tmp" "$2"
		changed=1
	fi
}

if [ "${1:-}" = --uninstall ]; then
	if [ -n "$systemd" ]; then
		systemctl --user disable deckhost.service 2>/dev/null || true
	fi
	stop
	rm -f "$bin" "$unit" "$autostart" "$app" "$icon"
	if [ -n "$systemd" ]; then
		systemctl --user daemon-reload 2>/dev/null || true
	fi
	echo "deckhost removed. To remove the udev rule too, as root: rm $rule"
	exit 0
fi

stop
if ! cmp -s "$src/deckhost" "$bin"; then
	install -Dm755 "$src/deckhost" "$bin"
fi
put "$src/deckhost-tray.desktop" "$autostart" 644
put "$src/deckhost.desktop" "$app" 644
put "$src/deckhost.svg" "$icon" 644
changed=
if [ -n "$systemd" ]; then
	put "$src/deckhost.service" "$unit" 644
fi
running=
if [ -n "$systemd" ]; then
	if [ -n "$changed" ]; then
		systemctl --user daemon-reload 2>/dev/null || true
	fi
	if { systemctl --user is-enabled -q deckhost.service || systemctl --user enable deckhost.service; } 2>/dev/null &&
		systemctl --user start deckhost.service 2>/dev/null; then
		running=1
	else
		echo "Couldn't reach systemd: once logged in, run: systemctl --user enable --now deckhost.service"
	fi
fi
if [ -n "${DISPLAY:-}${WAYLAND_DISPLAY:-}" ]; then
	if command -v setsid >/dev/null 2>&1; then
		setsid "$bin" tray >/dev/null 2>&1 &
	else
		nohup "$bin" tray >/dev/null 2>&1 &
	fi
	running=1
	if command -v dbus-send >/dev/null 2>&1 && ! dbus-send --session --print-reply \
		--dest=org.freedesktop.DBus /org/freedesktop/DBus org.freedesktop.DBus.NameHasOwner \
		string:org.kde.StatusNotifierWatcher 2>/dev/null | grep -q true; then
		echo "No tray host found, so the tray icon won't show (on GNOME, add the AppIndicator extension); deckhost works without it."
	fi
fi
if [ -n "$running" ]; then
	echo "deckhost installed and running; it starts at login."
else
	echo "deckhost installed; it starts when you next log in to your desktop."
fi
case ":$PATH:" in
*":$HOME/.local/bin:"*) ;;
*) echo "~/.local/bin isn't on your PATH: run deckhost as $bin, or add it to PATH." ;;
esac

for d in /etc /usr/lib /lib; do
	if [ -e "$d/udev/rules.d/70-d200-deck.rules" ]; then
		if cmp -s "$src/70-d200-deck.rules" "$d/udev/rules.d/70-d200-deck.rules"; then
			exit 0
		fi
		break
	fi
done
echo
if ! command -v udevadm >/dev/null 2>&1; then
	echo "This system doesn't run udev: give your user read and write access to the D200's /dev/hidraw nodes."
	exit 0
fi
root=
for t in sudo doas; do
	if command -v "$t" >/dev/null 2>&1; then
		root="$t "
		break
	fi
done
cmd="${root}install -Dm644 '$src/70-d200-deck.rules' $rule && ${root}udevadm control --reload-rules && ${root}udevadm trigger --subsystem-match=hidraw --action=change && ${root}udevadm trigger --subsystem-match=usb --attr-match=idVendor=1d6b --attr-match=idProduct=0104 --action=change"
echo "deckhost needs a new or updated udev rule (as root) so you can open the D200 and it can wake the PC:"
echo "  $cmd"
if [ -n "$root" ] && [ -t 0 ]; then
	printf "Install it now? [Y/n] "
	read -r answer
	case "$answer" in
	[nN]*) ;;
	*) if sh -c "$cmd"; then echo "udev rule installed."; else echo "Couldn't install it: run the command above as root."; fi ;;
	esac
fi
