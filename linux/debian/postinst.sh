#!/bin/bash
set -e

SERVICE="myhome"
SERVICE_FILES="${SERVICE}.service ${SERVICE}-update.service ${SERVICE}-update.timer"

mkdir -p /var/lib/$SERVICE

# Check if the script is being run during package installation
if [ "$1" = "configure" ]; then
    # Link the service files to the systemd directory
    for f in $SERVICE_FILES; do
        (cd /etc/systemd/system && ln -sf /lib/systemd/system/$f .)
    done

    # Reload systemd to recognize the new service
    systemctl daemon-reload

    # Enable the service to start on boot
    systemctl enable $SERVICE.service
    systemctl enable $SERVICE-update.timer

    # (Re)Start the service immediately
    systemctl restart $SERVICE.service
    systemctl restart $SERVICE-update.timer
fi

# Exit successfully
exit 0