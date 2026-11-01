#!/bin/bash
set -e

SERVICE="myhome"
# MYHOME_POSTINST_ROOT is a test seam (see linux/debian/postinst_test.py): it
# prefixes the state and config directories so the script can run unprivileged.
ROOT="${MYHOME_POSTINST_ROOT:-}"
STATE_DIR="$ROOT/var/lib/$SERVICE"
CONFIG_DIR="$ROOT/etc/$SERVICE"
CONFIG_FILE="$CONFIG_DIR/myhome.yaml"
EXAMPLE_CONFIG="/usr/share/$SERVICE/myhome-example.yaml"
PROM_CONFIG_FILE="$CONFIG_DIR/prometheus-mqtt-exporter.yaml"
PROM_EXAMPLE_CONFIG="/usr/share/$SERVICE/prometheus-mqtt-exporter.yaml.sample"
ENV_FILE="$STATE_DIR/.env"

# All systemd units to manage
SERVICES="${SERVICE}.service"
TIMERS="${SERVICE}-update.timer ${SERVICE}-db-backup.timer"

# Create necessary directories
mkdir -p "$STATE_DIR"
mkdir -p "$STATE_DIR/backups"
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
# Prompt for a secret without echoing it: _read_secret VAR LABEL CURRENT [optional]
# Blank keeps CURRENT (the value stored in .env), so a routine reconfigure never
# erases or re-asks a password. Loops for a non-empty answer only when there is
# no current value, unless a 4th argument "optional" is given (SMTP: a relay
# that needs no login is valid, so an empty answer is stored as empty). Only call it when the matching email/username/From is set:
# clearing that identifier is how an integration (and its secret) is disabled.
# ---------------------------------------------------------------------------
_read_secret() {
    local var="$1" label="$2" cur="$3" optional="${4:-}" val hint=""
    if [ -n "$cur" ]; then
        hint=" (blank keeps the current one)"
    fi
    while true; do
        read -rsp "${label}${hint}: " val
        echo ""
        val="${val:-$cur}"
        if [ -n "$val" ] || [ "$optional" = "optional" ]; then
            break
        fi
        echo "Password cannot be empty when it is not already set. Try again."
    done
    printf -v "$var" '%s' "$val"
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
    cur_beem_password="$(_env_get MYHOME_BEEM_PASSWORD)"
    cur_sfr_username="$(_env_get MYHOME_SFR_USERNAME)"
    cur_sfr_password="$(_env_get MYHOME_SFR_PASSWORD)"

    echo ""
    echo "=== Beem Energy credentials ==="
    echo "Set email and password to enable solar production polling."
    echo "Leave email blank to disable Beem integration (this also clears the"
    echo "password). Blank password keeps the current one."
    echo ""

    read -rp "Beem email [${cur_beem_email}]: " beem_email
    beem_email="${beem_email:-$cur_beem_email}"

    beem_password=""
    if [ -n "$beem_email" ]; then
        _read_secret beem_password "Beem password" "$cur_beem_password"
    fi

    echo ""
    echo "=== SFR box credentials (optional) ==="
    echo "Leave username blank to skip SFR authentication (this also clears the"
    echo "password). Blank password keeps the current one."
    echo ""

    read -rp "SFR username [${cur_sfr_username}]: " sfr_username
    sfr_username="${sfr_username:-$cur_sfr_username}"

    sfr_password=""
    if [ -n "$sfr_username" ]; then
        _read_secret sfr_password "SFR password" "$cur_sfr_password"
    fi

    cur_smtp_from="$(_env_get MYHOME_SMTP_FROM)"
    cur_smtp_to="$(_env_get MYHOME_SMTP_TO)"
    cur_smtp_username="$(_env_get MYHOME_SMTP_USERNAME)"
    cur_smtp_password="$(_env_get MYHOME_SMTP_PASSWORD)"

    echo ""
    echo "=== Email (SMTP) credentials — notice digest (optional) ==="
    echo "Used to email a daily summary of notice events (pool/garden plans,"
    echo "solar pump on/off, motion alerts). Leave 'From' blank to disable"
    echo "email sending entirely (this also clears the password; a blank"
    echo "password keeps the current one; leave it empty for a relay that needs no"
    echo "login). For Gmail, use an App Password:"
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

        _read_secret smtp_password "SMTP password (e.g. a Gmail App Password; empty if no login)" "$cur_smtp_password" optional
    fi

    _write_env "$beem_email" "$beem_password" "$sfr_username" "$sfr_password" \
        "$smtp_username" "$smtp_password" "$smtp_from" "$smtp_to"
    echo "Credentials written to $ENV_FILE"
    [ -z "${MYHOME_POSTINST_ENV_ONLY:-}" ] || exit 0  # test seam: stop before systemd/config steps
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

    # -------------------------------------------------------------------
    # Push the scripts bundled with this package to every Shelly device
    # the daemon already knows about, so devices end up running the exact
    # script versions shipped in this install/upgrade/downgrade.
    #
    # Script versioning is content-hash based (see
    # internal/myhome/shelly/script/ops.go: UploadWithVersion compares a
    # sha1 of the embedded script against a KVS-recorded hash on the
    # device), not sequence-number based, so re-running this on a
    # downgrade re-uploads scripts just as correctly as on an upgrade.
    #
    # `myhome ctl shelly script update '*'` only touches scripts already
    # loaded on a device (see myhome/ctl/shelly/script/update.go), so
    # devices never receive scripts they didn't already have.
    # -------------------------------------------------------------------
    MYHOME_BIN="/usr/bin/myhome"
    if [ -x "$MYHOME_BIN" ]; then
        echo "Waiting for $SERVICE to discover its known devices..."

        POLL_INTERVAL=3
        MAX_WAIT=90
        elapsed=0
        prev_count=-1
        stable_polls=0
        count=0

        while [ "$elapsed" -lt "$MAX_WAIT" ]; do
            count=$("$MYHOME_BIN" ctl --wait 5s list '*' --json 2>/dev/null | jq 'length' 2>/dev/null || echo 0)
            case "$count" in ''|*[!0-9]*) count=0 ;; esac

            # Stop as soon as the known-device count stops growing across
            # two consecutive polls (device.match returns the daemon's
            # DB-backed registry immediately, then fills in as retained
            # MQTT messages and mDNS discovery catch up).
            if [ "$count" -gt 0 ] && [ "$count" -eq "$prev_count" ]; then
                stable_polls=$((stable_polls + 1))
            else
                stable_polls=0
            fi
            prev_count=$count

            if [ "$stable_polls" -ge 2 ]; then
                break
            fi

            sleep "$POLL_INTERVAL"
            elapsed=$((elapsed + POLL_INTERVAL))
        done

        if [ "$count" -gt 0 ]; then
            echo "Updating Shelly scripts on $count known device(s) (waited ${elapsed}s)..."
            "$MYHOME_BIN" ctl --wait 180s shelly script update '*' || \
                echo "Warning: script update did not complete cleanly on all devices; this does not affect package installation. Run 'myhome ctl shelly script update *' manually to retry." >&2
        else
            echo "No known devices found after ${elapsed}s; skipping script update. Run 'myhome ctl shelly script update *' manually once devices are reachable."
        fi
    else
        echo "Warning: $MYHOME_BIN not found, skipping script update step" >&2
    fi

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