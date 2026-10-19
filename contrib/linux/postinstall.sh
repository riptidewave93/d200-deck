#!/bin/sh
# Package hook: let the logged-in user open the D200 now, and start the
# daemon in every user's graphical session from the next login.
udevadm control --reload-rules 2>/dev/null || true
udevadm trigger --subsystem-match=hidraw --action=change 2>/dev/null || true
systemctl --global enable deckhost.service 2>/dev/null || true
