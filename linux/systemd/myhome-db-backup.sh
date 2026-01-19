#!/bin/bash
# MyHome Database Backup Script
# Exports the device database to a timestamped JSON file

set -e

BACKUP_DIR="/var/lib/myhome/backups"
TIMESTAMP=$(date +%Y%m%d-%H%M%S)
BACKUP_FILE="${BACKUP_DIR}/devices-${TIMESTAMP}.json"
MAX_BACKUPS=30

# Create backup directory if it doesn't exist
mkdir -p "${BACKUP_DIR}"

# Export the database
echo "Exporting database to ${BACKUP_FILE}..."
/usr/bin/myhome ctl db export --pretty --output "${BACKUP_FILE}"

# Check if export was successful
if [ -f "${BACKUP_FILE}" ]; then
    echo "Backup created successfully: ${BACKUP_FILE}"
    
    # Create a symlink to the latest backup
    ln -sf "${BACKUP_FILE}" "${BACKUP_DIR}/devices-latest.json"
    
    # Remove old backups, keeping only the most recent MAX_BACKUPS
    cd "${BACKUP_DIR}"
    ls -t devices-*.json 2>/dev/null | grep -v 'devices-latest.json' | tail -n +$((MAX_BACKUPS + 1)) | xargs -r rm -f
    
    echo "Backup rotation complete. Keeping last ${MAX_BACKUPS} backups."
else
    echo "ERROR: Backup file was not created" >&2
    exit 1
fi
