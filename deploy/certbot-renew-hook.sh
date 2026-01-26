#!/bin/bash
# Certbot renewal hook for mailserver certificate reload
# This script is called by certbot after successfully renewing a certificate
# It signals the mailserver to reload the new certificate without restarting

set -e

# Log file for hook execution
LOG_FILE="${LOG_FILE:-/var/log/mailserver-certbot-hook.log}"

# Function to log messages
log_message() {
    echo "[$(date +'%Y-%m-%d %H:%M:%S')] $*" >> "$LOG_FILE"
}

log_message "Certbot hook started"

# Signal mailserver to reload certificates using SIGHUP
# This assumes the mailserver runs as a systemd service named 'mailserver'
if systemctl is-active --quiet mailserver; then
    log_message "Signaling mailserver to reload certificates"
    if systemctl reload mailserver; then
        log_message "Successfully signaled mailserver to reload certificates"
        exit 0
    else
        log_message "ERROR: Failed to signal mailserver"
        exit 1
    fi
else
    log_message "ERROR: mailserver service is not active"
    exit 1
fi
