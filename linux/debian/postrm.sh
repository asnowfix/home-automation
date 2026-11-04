#!/bin/bash
set -e

SERVICE="myhome"

# All systemd units to manage
SERVICES="${SERVICE}.service"
TIMERS="${SERVICE}-update.timer ${SERVICE}-db-backup.timer"

# Check if the script is being run during package purge (complete removal)
if [ "$1" = "purge" ]; then
    # Disable all units
    for timer in $TIMERS; do
        echo "Disabling $timer..."
        systemctl disable "$timer" 2>/dev/null || true
    done

    for svc in $SERVICES; do
        echo "Disabling $svc..."
        systemctl disable "$svc" 2>/dev/null || true
    done

    # Reload systemd
    systemctl daemon-reload

    # dpkg already removed our prometheus-mqtt-exporter.service.d drop-in;
    # restart so the exporter reverts to its own default configuration
    if systemctl is-active --quiet prometheus-mqtt-exporter 2>/dev/null; then
        echo "Restarting prometheus-mqtt-exporter to drop myhome's configuration override..."
        systemctl restart prometheus-mqtt-exporter 2>/dev/null || true
    fi

    # Optionally remove data directory (commented out for safety)
    # rm -rf /var/lib/$SERVICE
fi

# On remove (not purge), just disable the units
if [ "$1" = "remove" ]; then
    for timer in $TIMERS; do
        echo "Disabling $timer..."
        systemctl disable "$timer" 2>/dev/null || true
    done

    for svc in $SERVICES; do
        echo "Disabling $svc..."
        systemctl disable "$svc" 2>/dev/null || true
    done

    # Reload systemd
    systemctl daemon-reload

    # dpkg already removed our prometheus-mqtt-exporter.service.d drop-in;
    # restart so the exporter reverts to its own default configuration
    if systemctl is-active --quiet prometheus-mqtt-exporter 2>/dev/null; then
        echo "Restarting prometheus-mqtt-exporter to drop myhome's configuration override..."
        systemctl restart prometheus-mqtt-exporter 2>/dev/null || true
    fi
fi

# Exit successfully
exit 0
