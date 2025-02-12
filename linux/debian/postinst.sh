#!/bin/bash
set -e

SERVICE="myhome"
SERVICE_FILE="${SERVICE}.service"

mkdir -p /var/lib/$SERVICE

# Check if the script is being run during package installation
if [ "$1" = "configure" ]; then
    # Copy the service file to the systemd directory
    cp /usr/share/$SERVICE/$SERVICE_FILE /etc/systemd/system/

    # Reload systemd to recognize the new service
    systemctl daemon-reload

    # Enable the service to start on boot
    systemctl enable $SERVICE_FILE

    # Start the service immediately
    systemctl start $SERVICE_FILE
fi

# Exit successfully
exit 0