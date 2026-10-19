#!/bin/sh
# Package hook: on removal (not upgrade), stop enabling the daemon.
case "$1" in
remove | 0) systemctl --global disable deckhost.service 2>/dev/null || true ;;
esac
