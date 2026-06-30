#!/bin/bash
set -e

SERVICE="myhome"
CONFIG_DIR="/etc/$SERVICE"
CONFIG_FILE="$CONFIG_DIR/myhome.yaml"
EXAMPLE_CONFIG="/usr/share/$SERVICE/myhome-example.yaml"
PROM_CONFIG_FILE="$CONFIG_DIR/prometheus-mqtt-exporter.yaml"
PROM_EXAMPLE_CONFIG="/usr/share/$SERVICE/prometheus-mqtt-exporter.yaml.sample"
ENV_FILE="/var/lib/$SERVICE/.env"

# All systemd units to manage
SERVICES="${SERVICE}.service"
TIMERS="${SERVICE}-update.timer ${SERVICE}-db-backup.timer"

# Create necessary directories
mkdir -p /var/lib/$SERVICE
mkdir -p /var/lib/$SERVICE/backups
mkdir -p "$CONFIG_DIR"

# ---------------------------------------------------------------------------
# Credentials helper: read a current value from the .env file
# ---------------------------------------------------------------------------
_env_get() {
    local key="$1"
    if [ -f "$ENV_FILE" ]; then
        grep "^${key}=" "$ENV_FILE" 2>/dev/null | cut -d'=' -f2- || true
    fi
}

# ---------------------------------------------------------------------------
# Write (or rewrite) the .env file from the credential variables.
# Called after interactive prompting or when creating the skeleton.
# ---------------------------------------------------------------------------
_write_env() {
    local beem_email="$1"
    local beem_password="$2"
    local sfr_username="$3"
    local sfr_password="$4"
    local smtp_username="$5"
    local smtp_password="$6"
    local smtp_from="$7"
    local smtp_to="$8"

    cat > "$ENV_FILE" <<EOF
# MyHome credentials — loaded by systemd via EnvironmentFile.
# Run: dpkg-reconfigure $SERVICE   to update interactively.
MYHOME_BEEM_EMAIL=${beem_email}
MYHOME_BEEM_PASSWORD=${beem_password}
MYHOME_SFR_USERNAME=${sfr_username}
MYHOME_SFR_PASSWORD=${sfr_password}
# Notice digest email (smtp.host/smtp.port live in myhome.yaml, not here).
# Leave MYHOME_SMTP_FROM blank to disable email sending entirely.
MYHOME_SMTP_USERNAME=${smtp_username}
MYHOME_SMTP_PASSWORD=${smtp_password}
MYHOME_SMTP_FROM=${smtp_from}
MYHOME_SMTP_TO=${smtp_to}
EOF
    chmod 600 "$ENV_FILE"
}

# ---------------------------------------------------------------------------
# Credential configuration — interactive or skeleton
# ---------------------------------------------------------------------------
if [ -t 0 ] && [ "${DEBIAN_FRONTEND:-}" != "noninteractive" ]; then
    # --- Interactive path: prompt for credentials ---

    # Read existing values (pre-fill prompts on reconfigure)
    cur_beem_email="$(_env_get MYHOME_BEEM_EMAIL)"
    cur_sfr_username="$(_env_get MYHOME_SFR_USERNAME)"

    echo ""
    echo "=== Beem Energy credentials ==="
    echo "Set email and password to enable solar production polling."
    echo "Leave email blank to disable Beem integration."
    echo ""

    read -rp "Beem email [${cur_beem_email}]: " beem_email
    beem_email="${beem_email:-$cur_beem_email}"

    beem_password=""
    if [ -n "$beem_email" ]; then
        while true; do
            read -rsp "Beem password: " beem_password
            echo ""
            if [ -n "$beem_password" ]; then
                break
            fi
            echo "Password cannot be empty when email is set. Try again."
        done
    fi

    echo ""
    echo "=== SFR box credentials (optional) ==="
    echo "Leave username blank to skip SFR authentication."
    echo ""

    read -rp "SFR username [${cur_sfr_username}]: " sfr_username
    sfr_username="${sfr_username:-$cur_sfr_username}"

    sfr_password=""
    if [ -n "$sfr_username" ]; then
        read -rsp "SFR password: " sfr_password
        echo ""
    fi

    cur_smtp_from="$(_env_get MYHOME_SMTP_FROM)"
    cur_smtp_to="$(_env_get MYHOME_SMTP_TO)"
    cur_smtp_username="$(_env_get MYHOME_SMTP_USERNAME)"

    echo ""
    echo "=== Email (SMTP) credentials — notice digest (optional) ==="
    echo "Used to email a daily summary of notice events (pool/garden plans,"
    echo "solar pump on/off, motion alerts). Leave 'From' blank to disable"
    echo "email sending entirely. For Gmail, use an App Password:"
    echo "https://myaccount.google.com/apppasswords"
    echo ""

    read -rp "SMTP From address [${cur_smtp_from}]: " smtp_from
    smtp_from="${smtp_from:-$cur_smtp_from}"

    smtp_to=""
    smtp_username=""
    smtp_password=""
    if [ -n "$smtp_from" ]; then
        read -rp "SMTP To address(es) [${cur_smtp_to}]: " smtp_to
        smtp_to="${smtp_to:-$cur_smtp_to}"

        read -rp "SMTP username [${cur_smtp_username:-$smtp_from}]: " smtp_username
        smtp_username="${smtp_username:-${cur_smtp_username:-$smtp_from}}"

        read -rsp "SMTP password (e.g. a Gmail App Password): " smtp_password
        echo ""
    fi

    _write_env "$beem_email" "$beem_password" "$sfr_username" "$sfr_password" \
        "$smtp_username" "$smtp_password" "$smtp_from" "$smtp_to"
    echo "Credentials written to $ENV_FILE"
else
    # --- Non-interactive path: create skeleton if absent ---
    if [ ! -f "$ENV_FILE" ]; then
        _write_env "" "" "" "" "" "" "" ""
        echo "Created credential skeleton at $ENV_FILE"
        echo "Run 'dpkg-reconfigure $SERVICE' to configure credentials interactively."
    fi
fi

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

# Create default Prometheus MQTT exporter configuration if it doesn't exist.
# Lives under /etc/myhome (not /etc/prometheus) to avoid clashing with the
# upstream prometheus-mqtt-exporter package's own conffile (see #261); the
# systemd drop-in shipped alongside it points the exporter here.
if [ ! -f "$PROM_CONFIG_FILE" ]; then
    if [ -f "$PROM_EXAMPLE_CONFIG" ]; then
        echo "Creating default Prometheus MQTT exporter configuration at $PROM_CONFIG_FILE..."
        cp "$PROM_EXAMPLE_CONFIG" "$PROM_CONFIG_FILE"
        chmod 644 "$PROM_CONFIG_FILE"
    else
        echo "Warning: Example Prometheus MQTT exporter configuration not found at $PROM_EXAMPLE_CONFIG"
    fi
else
    echo "Prometheus MQTT exporter configuration already exists at $PROM_CONFIG_FILE"
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