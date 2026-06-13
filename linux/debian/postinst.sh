#!/bin/bash
set -e

SERVICE="myhome"
CONFIG_DIR="/etc/$SERVICE"
CONFIG_FILE="$CONFIG_DIR/myhome.yaml"
EXAMPLE_CONFIG="/usr/share/$SERVICE/myhome-example.yaml"

# All systemd units to manage
SERVICES="${SERVICE}.service"
TIMERS="${SERVICE}-update.timer ${SERVICE}-db-backup.timer"

# Create necessary directories
mkdir -p /var/lib/$SERVICE
mkdir -p /var/lib/$SERVICE/backups
mkdir -p "$CONFIG_DIR"

# Create default configuration file if it doesn't exist
if [ ! -f "$CONFIG_FILE" ]; then
    if [ -f "$EXAMPLE_CONFIG" ]; then
        echo "Creating default configuration file at $CONFIG_FILE..."
        cp "$EXAMPLE_CONFIG" "$CONFIG_FILE"
        chmod 644 "$CONFIG_FILE"
        echo "Configuration file created. Please review and customize $CONFIG_FILE"
    else
        echo "Warning: Example configuration file not found at $EXAMPLE_CONFIG"
        echo "Please create $CONFIG_FILE manually"
    fi
else
    echo "Configuration file already exists at $CONFIG_FILE"
fi

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

    # Restart prometheus-mqtt-exporter to apply new configuration
    if systemctl list-unit-files prometheus-mqtt-exporter.service >/dev/null 2>&1; then
        if systemctl is-active --quiet prometheus-mqtt-exporter; then
            echo "Restarting prometheus-mqtt-exporter to apply new configuration..."
            systemctl restart prometheus-mqtt-exporter
        else
            echo "prometheus-mqtt-exporter is installed but not running, skipping restart"
        fi
    else
        echo "prometheus-mqtt-exporter is not installed, skipping restart"
    fi
fi

# Exit successfully
exit 0