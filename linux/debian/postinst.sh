#!/bin/bash
set -e

SERVICE="myhome"
SERVICE_FILES="${SERVICE}.service ${SERVICE}-update.service ${SERVICE}-update.timer"

mkdir -p /var/lib/$SERVICE

# Check if the script is being run during package installation
if [ "$1" = "configure" ]; then
    # Reload systemd to recognize the new service
    systemctl daemon-reload

    # Enable the service to start on boot
    systemctl reenable $SERVICE.service
    systemctl reenable $SERVICE-update.timer

    # (Re)Start the service immediately
    systemctl restart $SERVICE.service
    systemctl restart $SERVICE-update.timer
fi

# Exit successfully
exit 0