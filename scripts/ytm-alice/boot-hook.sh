#!/bin/sh
# ytm-alice boot hook for a rooted speaker driven by Quasar Bridge (fork).
# Installed as /etc/init.d/S99zytm-alice by install.sh; delete it to remove.
# ytm-alice only plays: phrases are understood by the bridge, so its own
# voice commands and phone casting are off. The panel listens on the
# speaker's IPv4 address only and answers only the bridge host; if that rule
# cannot be set, the player does not start. @ROOT@, @BRIDGE_IP@ and
# @SPEAKER_IP@ are filled in by install.sh.
ROOT=@ROOT@
BRIDGE_IP=@BRIDGE_IP@
SPEAKER_IP=@SPEAKER_IP@
PORT=24721
RUN=${YTM_ALICE_RUN:-/tmp}
LOG=$RUN/ytm-alice.log
PIDFILE=$RUN/ytm-alice-watchdog.pid

firewall_off() {
	for ipt in iptables ip6tables; do
		command -v "$ipt" >/dev/null 2>&1 || continue
		while "$ipt" -D INPUT -p tcp --dport "$PORT" -j YTM_ALICE 2>/dev/null; do :; done
		"$ipt" -F YTM_ALICE 2>/dev/null
		"$ipt" -X YTM_ALICE 2>/dev/null
	done
}

firewall_ok() {
	iptables -C INPUT -p tcp --dport "$PORT" -j YTM_ALICE 2>/dev/null &&
		iptables -C YTM_ALICE -s "$BRIDGE_IP" -j ACCEPT 2>/dev/null &&
		iptables -C YTM_ALICE -j DROP 2>/dev/null
}

firewall_on() {
	firewall_off
	iptables -N YTM_ALICE &&
		iptables -A YTM_ALICE -i lo -j ACCEPT &&
		iptables -A YTM_ALICE -s "$BRIDGE_IP" -j ACCEPT &&
		iptables -A YTM_ALICE -j DROP &&
		iptables -I INPUT -p tcp --dport "$PORT" -j YTM_ALICE || true
	# The panel does not listen on IPv6; drop it there too where possible.
	if command -v ip6tables >/dev/null 2>&1; then
		ip6tables -N YTM_ALICE 2>/dev/null &&
			ip6tables -A YTM_ALICE -i lo -j ACCEPT &&
			ip6tables -A YTM_ALICE -j DROP &&
			ip6tables -I INPUT -p tcp --dport "$PORT" -j YTM_ALICE || true
	fi
	firewall_ok
}

player_pids() {
	for p in /proc/[0-9]*; do
		[ "$(readlink "$p/exe" 2>/dev/null)" = "$ROOT/bin/ytm-alice" ] && echo "${p#/proc/}"
	done
}

watchdog() {
	# Restart after a crash; a stop removes the pid file and ends the loop.
	# The rule is checked before every start and once a minute while playing.
	while [ -f "$PIDFILE" ]; do
		if ! firewall_ok && ! firewall_on; then
			echo "ytm-alice: panel firewall missing, not starting" >>"$LOG"
			sleep 30
			continue
		fi
		YTM_ALICE_DATA="$ROOT/var" YTM_ALICE_VOICE=0 YTM_ALICE_CAST=0 YTM_ALICE_WEB_ADDR="$SPEAKER_IP:$PORT" \
			"$ROOT/bin/ytm-alice" serve >>"$LOG" 2>&1 &
		child=$!
		ticks=0
		while [ -f "$PIDFILE" ] && kill -0 "$child" 2>/dev/null; do
			ticks=$((ticks + 1))
			if [ $((ticks % 30)) -eq 0 ] && ! firewall_ok && ! firewall_on; then
				echo "ytm-alice: panel firewall lost, stopping the player" >>"$LOG"
				kill "$child" 2>/dev/null
			fi
			sleep 2
		done
		wait "$child" 2>/dev/null
		[ -f "$PIDFILE" ] && sleep 5
	done
}

case "$1" in
start)
	[ -x "$ROOT/bin/ytm-alice" ] || exit 0
	[ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null && exit 0
	: >"$LOG"
	if ! firewall_on; then
		echo "ytm-alice: cannot protect the panel, not starting" | tee -a "$LOG" >&2
		exit 1
	fi
	# The pid file must exist before the loop checks it.
	echo starting >"$PIDFILE"
	watchdog </dev/null >/dev/null 2>&1 &
	echo $! >"$PIDFILE"
	# The bridge names the track itself; ytm-alice must not say «Включаю».
	(sleep 15; [ -f "$PIDFILE" ] && YTM_ALICE_DATA="$ROOT/var" "$ROOT/bin/ytm-alice" announce-off >>"$LOG" 2>&1) </dev/null >/dev/null 2>&1 &
	;;
stop)
	WATCHDOG=$(cat "$PIDFILE" 2>/dev/null || true)
	rm -f "$PIDFILE"
	[ -n "$WATCHDOG" ] && kill "$WATCHDOG" 2>/dev/null
	YTM_ALICE_DATA="$ROOT/var" "$ROOT/bin/ytm-alice" daemon stop >>"$LOG" 2>&1
	for pid in $(player_pids); do kill "$pid" 2>/dev/null; done
	sleep 1
	# Open the port again only when no player is left behind it.
	if [ -z "$(player_pids)" ]; then
		firewall_off
	else
		echo "ytm-alice: player still running, panel rule kept" | tee -a "$LOG" >&2
	fi
	;;
restart)
	"$0" stop
	sleep 1
	"$0" start
	;;
esac
exit 0
