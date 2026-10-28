#!/bin/zsh
# See docs/monitor-solar.md for usage and for what each column means.
# Gate-3 solar monitor. Observes the experiment, not just failures.
#  - every 5 min: cheap RPCs (status, relay, solar)
#  - on EVERY relay transition: one wrapped Eval to capture WHY (window facts,
#    solar want, runtime) and print it loudly
#  - hourly: re-read window facts, since the sunrise daily check rewrites them
set -u
DEV=${DEV:-filtration-hiver}
SID=${SID:-2}
B=${BROKER:-tcp://192.168.1.2:1883}
M=${MH:-"go run ./myhome"}
INTERVAL=${INTERVAL:-300}
ITERS=${ITERS:-200}
HERE=${0:a:h}
LOG=${LOG:-solar-day-$(date +%Y%m%d).log}
EV="(function(){try{return 'win='+F_WIN_START+'-'+F_WIN_STOP+' want='+F_SOLAR_WANT+' runT='+STATE.runtimeTodaySec+' out='+STATE.activeOutput}catch(e){return 'ERR:'+e}})()"
DEATHS=0; LAST_OUT=""; LAST_T=0
echo "# ts,running,mem_used,mem_free,relay,solar_w,stale,note" >> $LOG
for i in $(seq 1 $ITERS); do
  TS=$(date '+%H:%M:%S')
  S=$(timeout 60 $M ctl shelly call -B $B -T 45s $DEV Script.GetStatus "{\"id\":$SID}" 2>/dev/null)
  RUN=$(echo "$S"|grep -o '"running": [a-z]*'|awk '{print $2}')
  MU=$(echo "$S"|grep -o '"mem_used": [0-9]*'|awk '{print $2}')
  MF=$(echo "$S"|grep -o '"mem_free": [0-9]*'|awk '{print $2}')
  O=$(timeout 60 $M ctl shelly call -B $B -T 45s $DEV Switch.GetStatus '{"id":0}' 2>/dev/null|grep -o '"output": [a-z]*'|awk '{print $2}')
  SOL=$(python3 "$HERE/solar-probe.py" 2>/dev/null); SW=${SOL%%|*}; ST=${SOL##*|}
  NOTE=""
  # RPC failure is NOT a death: a dead script still answers with running:false.
  if [ -z "$RUN" ]; then NOTE="RPC-FAIL(not counted)"
  elif [ "$RUN" != "true" ]; then
    DEATHS=$((DEATHS+1)); NOTE="SCRIPT-DOWN(count=$DEATHS)"
    timeout 60 $M ctl shelly call -B $B -T 45s $DEV Script.Start "{\"id\":$SID}" >/dev/null 2>&1
    [ $DEATHS -ge 2 ] && NOTE="$NOTE ALERT-ABORT-2"
  fi
  [ -n "$MF" ] && [ "$MF" -lt 5000 ] && NOTE="$NOTE ALERT-HEAP($MF)"
  NOW=$(date +%s)
  # THE event worth reporting: a relay transition. Capture why, and say so.
  if [ -n "$O" ] && [ -n "$LAST_OUT" ] && [ "$O" != "$LAST_OUT" ]; then
    W=$(timeout 90 $M ctl shelly call -B $B -T 60s $DEV Script.Eval "{\"id\":$SID,\"code\":\"$EV\"}" 2>/dev/null|grep -o '"result": "[^"]*"'|cut -d'"' -f4)
    D=$((NOW-LAST_T)); [ $LAST_T -eq 0 ] && D=99999
    echo "$TS,,,,TRANSITION,$SW,$ST,$LAST_OUT->$O gap=${D}s $W" >> $LOG
    echo ">>> RELAY $LAST_OUT -> $O at $TS | solar=${SW}W | $W"
    [ $D -lt 300 ] && NOTE="$NOTE ALERT-CYCLE(${D}s)"
    LAST_T=$NOW
  fi
  [ -n "$O" ] && LAST_OUT=$O
  # hourly: window facts, because the sunrise daily check rewrites them
  if [ $((i % 12)) -eq 1 ]; then
    W=$(timeout 90 $M ctl shelly call -B $B -T 60s $DEV Script.Eval "{\"id\":$SID,\"code\":\"$EV\"}" 2>/dev/null|grep -o '"result": "[^"]*"'|cut -d'"' -f4)
    NOTE="$NOTE [$W]"
  fi
  echo "$TS,$RUN,$MU,$MF,$O,$SW,$ST,$NOTE" >> $LOG
  case "$NOTE" in *ALERT*) echo "ALERT at $TS: $NOTE" ;; esac
  sleep $INTERVAL
done
