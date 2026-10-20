#!/bin/sh
# Installs deckhost for the current user on any Linux, for distros without the
# .deb/.rpm or where packages are awkward (atomic distros, SteamOS, Alpine...):
# the binary, the daemon's systemd user unit (where systemd runs) and the tray
# icon's autostart entry (the tray starts the daemon if systemd doesn't), then
# the udev rule that lets you open the D200 (as root, once).
#
#   ./install.sh               install or update
#   ./install.sh --uninstall   remove (the udev rule stays; see below)
set -eu
src=$(cd "$(dirname "$0")" && pwd)
cfg=${XDG_CONFIG_HOME:-$HOME/.config}
bin=$HOME/.local/bin/deckhost
unit=$cfg/systemd/user/deckhost.service
autostart=$cfg/autostart/deckhost-tray.desktop
rule=/etc/udev/rules.d/70-d200-deck.rules

systemd=
if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
	systemd=1
fi

# Stops the daemon and the tray, however they were started.
stop() {
	if [ -n "$systemd" ]; then
		systemctl --user disable --now deckhost.service 2>/dev/null || true
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

if [ "${1:-}" = --uninstall ]; then
	stop
	rm -f "$bin" "$unit" "$autostart"
	if [ -n "$systemd" ]; then
		systemctl --user daemon-reload 2>/dev/null || true
	fi
	echo "deckhost removed. To remove the udev rule too, as root: rm $rule"
	exit 0
fi

stop
install -Dm755 "$src/deckhost" "$bin"
mkdir -p "$(dirname "$autostart")"
sed "s|@BIN@|$bin|" "$src/deckhost-tray.desktop" > "$autostart"
running=
if [ -n "$systemd" ]; then
	mkdir -p "$(dirname "$unit")"
	sed "s|@BIN@|$bin|" "$src/deckhost.service" > "$unit"
	if { systemctl --user daemon-reload && systemctl --user enable --now deckhost.service; } 2>/dev/null; then
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
		exit 0
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
cmd="${root}install -Dm644 '$src/70-d200-deck.rules' $rule && ${root}udevadm control --reload-rules && ${root}udevadm trigger --subsystem-match=hidraw --action=change"
echo "deckhost needs a udev rule (once, as root) to open the D200:"
echo "  $cmd"
if [ -n "$root" ] && [ -t 0 ]; then
	printf "Install it now? [Y/n] "
	read -r answer
	case "$answer" in
	[nN]*) ;;
	*) if sh -c "$cmd"; then echo "udev rule installed."; else echo "Couldn't install it: run the command above as root."; fi ;;
	esac
fi
