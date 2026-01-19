#!/bin/bash
set -e

SERVICE="myhome"

# All systemd units to manage
SERVICES="${SERVICE}.service"
TIMERS="${SERVICE}-update.timer ${SERVICE}-db-backup.timer"

# Check if the script is being run during package removal
if [ "$1" = "remove" ] || [ "$1" = "upgrade" ]; then
    # Stop timers first
    for timer in $TIMERS; do
        echo "Stopping $timer..."
        systemctl stop "$timer" 2>/dev/null || true
    done

    # Stop services
    for svc in $SERVICES; do
        echo "Stopping $svc..."
        systemctl stop "$svc" 2>/dev/null || true
    done
fi

# Exit successfully
exit 0
