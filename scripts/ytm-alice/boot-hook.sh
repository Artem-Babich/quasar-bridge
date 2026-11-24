#!/bin/sh
# ytm-alice boot hook for a rooted speaker driven by Quasar Bridge (fork).
# Installed as /etc/init.d/S99zytm-alice by install.sh; delete it to remove.
# ytm-alice only plays: phrases are understood by the bridge, so its own
# voice commands and phone casting are off. The panel answers only the
# bridge host. @BRIDGE_IP@ and @ROOT@ are filled in by install.sh.
ROOT=@ROOT@
BRIDGE_IP=@BRIDGE_IP@
PORT=24721
RUN=${YTM_ALICE_RUN:-/tmp}
LOG=$RUN/ytm-alice.log
PIDFILE=$RUN/ytm-alice-watchdog.pid

firewall() {
	for ipt in iptables ip6tables; do
		command -v "$ipt" >/dev/null 2>&1 || continue
		while "$ipt" -D INPUT -p tcp --dport "$PORT" -j YTM_ALICE 2>/dev/null; do :; done
		"$ipt" -F YTM_ALICE 2>/dev/null
		"$ipt" -X YTM_ALICE 2>/dev/null
		[ "$1" = off ] && continue
		"$ipt" -N YTM_ALICE
		"$ipt" -A YTM_ALICE -i lo -j ACCEPT
		if [ "$ipt" = iptables ]; then
			"$ipt" -A YTM_ALICE -s "$BRIDGE_IP" -j ACCEPT
		fi
		"$ipt" -A YTM_ALICE -j DROP
		"$ipt" -I INPUT -p tcp --dport "$PORT" -j YTM_ALICE
	done
}

watchdog() {
	# Restart after a crash; a stop removes the pid file and ends the loop.
	while [ -f "$PIDFILE" ]; do
		YTM_ALICE_DATA="$ROOT/var" YTM_ALICE_VOICE=0 YTM_ALICE_CAST=0 YTM_ALICE_WEB_ADDR=":$PORT" \
			"$ROOT/bin/ytm-alice" serve >>"$LOG" 2>&1
		[ -f "$PIDFILE" ] && sleep 5
	done
}

case "$1" in
start)
	[ -x "$ROOT/bin/ytm-alice" ] || exit 0
	[ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null && exit 0
	firewall on
	: >"$LOG"
	watchdog &
	echo $! >"$PIDFILE"
	# The bridge names the track itself; ytm-alice must not say «Включаю».
	(sleep 15; [ -f "$PIDFILE" ] && YTM_ALICE_DATA="$ROOT/var" "$ROOT/bin/ytm-alice" announce-off >>"$LOG" 2>&1) &
	;;
stop)
	WATCHDOG=$(cat "$PIDFILE" 2>/dev/null || true)
	rm -f "$PIDFILE"
	[ -n "$WATCHDOG" ] && kill "$WATCHDOG" 2>/dev/null
	YTM_ALICE_DATA="$ROOT/var" "$ROOT/bin/ytm-alice" daemon stop >>"$LOG" 2>&1
	firewall off
	;;
restart)
	"$0" stop
	sleep 1
	"$0" start
	;;
esac
exit 0
