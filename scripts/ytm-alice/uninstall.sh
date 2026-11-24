#!/bin/sh
# Remove ytm-alice installed by install.sh (fork).
#
#   uninstall.sh root@SPEAKER [SSH_PORT] [--purge]
#
# Stops the player, removes the boot hook and the panel firewall rule, and
# deletes the program files. --purge also deletes ROOT/var: the YouTube
# account and the saved queue.
set -eu
[ $# -ge 1 ] || { sed -n '2,9p' "$0"; exit 2; }
TARGET=$1
shift
PORT=2222 PURGE=""
for arg in "$@"; do
	case "$arg" in
	--purge) PURGE=--purge ;;
	*[!0-9]* | "") echo "unknown argument: $arg" >&2; exit 2 ;;
	*) PORT=$arg ;;
	esac
done
ROOT=/data/ytm-alice
VAR=""
[ "$PURGE" = "--purge" ] && VAR="$ROOT/var"
ssh -o BatchMode=yes -p "$PORT" -- "$TARGET" "
	set -e
	[ -x /etc/init.d/S99zytm-alice ] && /etc/init.d/S99zytm-alice stop || true
	mount -o remount,rw /
	rm -f /etc/init.d/S99zytm-alice
	mount -o remount,ro / || true
	rm -rf $ROOT/bin $ROOT/share $VAR
"
echo "ytm-alice removed from $TARGET"
