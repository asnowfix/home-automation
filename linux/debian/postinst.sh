#!/bin/bash
set -e

SERVICE="myhome"

# All systemd units to manage
SERVICES="${SERVICE}.service"
TIMERS="${SERVICE}-update.timer ${SERVICE}-db-backup.timer"

mkdir -p /var/lib/$SERVICE
mkdir -p /var/lib/$SERVICE/backups

# Check if the script is being run during package installation
if [ "$1" = "configure" ]; then
    # Reload systemd to recognize the new services
    systemctl daemon-reload

    # Enable and start services
    for svc in $SERVICES; do
        echo "Enabling and starting $svc..."
        systemctl reenable "$svc"
        systemctl restart "$svc"
    done

    # Enable and start timers
    for timer in $TIMERS; do
        echo "Enabling and starting $timer..."
        systemctl reenable "$timer"
        systemctl restart "$timer"
    done
fi

# Exit successfully
exit 0