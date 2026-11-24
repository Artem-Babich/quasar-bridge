#!/bin/sh
# Install ytm-alice on a rooted speaker without YAM (fork; owner decision E16).
#
#   install.sh [--dry-run DIR] PACKAGE.yampkg YTM_ALICE_BINARY root@SPEAKER BRIDGE_IP [SSH_PORT]
#
# PACKAGE is the official ytm-alice release for the speaker model (it brings
# the resolver and the audio player). YTM_ALICE_BINARY is the fork build of
# ytm-alice with YTM_ALICE_VOICE support; it replaces the packaged one. The
# Bluetooth media layer is not installed (E17). Running it again updates the
# files and keeps the account and queue in ROOT/var.
# --dry-run DIR prepares the files and the boot hook in DIR and stops.
set -eu

DRY=""
if [ "${1:-}" = "--dry-run" ]; then
	DRY=$2
	shift 2
fi
[ $# -ge 4 ] || { sed -n '2,13p' "$0"; exit 2; }
PKG=$1 BIN=$2 TARGET=$3 BRIDGE_IP=$4 PORT=${5:-2222}
ROOT=${YTM_ALICE_ROOT:-/data/ytm-alice}
HERE=$(cd "$(dirname "$0")" && pwd)

case "$BRIDGE_IP" in
*[!0-9.]* | "") echo "BRIDGE_IP must be an IPv4 address" >&2; exit 2 ;;
esac
command -v zstd >/dev/null || { echo "zstd is required to unpack $PKG" >&2; exit 2; }
file "$BIN" | grep -q 'ARM aarch64' || { echo "$BIN is not an arm64 Linux build" >&2; exit 2; }

WORK=${DRY:-$(mktemp -d)}
mkdir -p "$WORK/pkg" "$WORK/stage/bin" "$WORK/stage/share"
zstd -dc "$PKG" | tar -x -C "$WORK/pkg"
grep -q '^target: bergamot' "$WORK/pkg/MANIFEST.yaml" || echo "warning: package target is not bergamot" >&2
for f in ytm-alice-resolver ytm-alice-player ytm-alice-player-arm64 ytm-alice-player-armhf; do
	cp "$WORK/pkg/files/bin/$f" "$WORK/stage/bin/$f"
done
cp "$BIN" "$WORK/stage/bin/ytm-alice"
cp -R "$WORK/pkg/files/share/local.ytm-alice" "$WORK/stage/share/"
chmod 755 "$WORK/stage/bin/"*
sed -e "s|@ROOT@|$ROOT|" -e "s|@BRIDGE_IP@|$BRIDGE_IP|" "$HERE/boot-hook.sh" >"$WORK/stage/S99zytm-alice"
chmod 755 "$WORK/stage/S99zytm-alice"
sh -n "$WORK/stage/S99zytm-alice"
if [ -n "$DRY" ]; then
	echo "dry run: files in $WORK/stage"
	exit 0
fi

SSH="ssh -o BatchMode=yes -p $PORT $TARGET"
tar -C "$WORK/stage" -cf - bin share S99zytm-alice | gzip | $SSH "
	set -e
	[ -x /etc/init.d/S99zytm-alice ] && /etc/init.d/S99zytm-alice stop || true
	mkdir -p $ROOT/var $ROOT.new
	gzip -dc | tar -xf - -C $ROOT.new
	rm -rf $ROOT/bin $ROOT/share
	mv $ROOT.new/bin $ROOT.new/share $ROOT/
	mount -o remount,rw /
	cp $ROOT.new/S99zytm-alice /etc/init.d/S99zytm-alice
	mount -o remount,ro / || true
	rm -rf $ROOT.new
	/etc/init.d/S99zytm-alice start
	sleep 3
	YTM_ALICE_DATA=$ROOT/var $ROOT/bin/ytm-alice status
"
[ -z "$DRY" ] && rm -rf "$WORK"
echo "ytm-alice installed on $TARGET; panel only for $BRIDGE_IP"
