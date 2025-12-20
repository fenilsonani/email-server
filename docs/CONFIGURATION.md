# Configuration Guide

This guide covers all configuration options for the personal email server.

## Configuration File

The server uses a YAML configuration file. By default, it looks for `/etc/mailserver/config.yaml`.

### Full Configuration Reference

```yaml
# Server configuration
server:
  # Hostname for the mail server (used in HELO/EHLO and certificates)
  hostname: mail.example.com

  # SMTP port for receiving mail from other servers (MX)
  smtp_port: 25

  # Submission port for authenticated clients to send mail
  submission_port: 587

  # SMTPS port for implicit TLS connections
  smtps_port: 465

  # IMAP port for STARTTLS connections
  imap_port: 143

  # IMAPS port for implicit TLS connections
  imaps_port: 993

  # DAV port for CalDAV/CardDAV (HTTPS)
  dav_port: 8443

# TLS/Certificate configuration
tls:
  # Enable automatic certificate management via Let's Encrypt
  auto_tls: true

  # Email for Let's Encrypt account (required if auto_tls is true)
  email: admin@example.com

  # Directory to store ACME certificates
  cache_dir: /var/lib/mailserver/acme

  # Manual certificate paths (used if auto_tls is false)
  cert_file: /etc/mailserver/certs/fullchain.pem
  key_file: /etc/mailserver/certs/privkey.pem

# Storage configuration
storage:
  # Base directory for all data
  data_dir: /var/lib/mailserver

  # SQLite database path
  database_path: /var/lib/mailserver/mail.db

  # Maildir storage path
  maildir_path: /var/lib/mailserver/maildir

# Domain configuration (list of managed domains)
domains:
  - name: example.com
    # DKIM selector (used in DNS record: selector._domainkey.domain)
    dkim_selector: mail
    # Path to DKIM private key
    dkim_key_file: /etc/mailserver/dkim/example.com.key

  - name: example.org
    dkim_selector: default
    dkim_key_file: /etc/mailserver/dkim/example.org.key

# Security configuration
security:
  # Require TLS for all connections (recommended: true)
  require_tls: true

  # Verify SPF records on incoming mail
  verify_spf: true

  # Verify DKIM signatures on incoming mail
  verify_dkim: true

  # Verify DMARC policies on incoming mail
  verify_dmarc: true

  # Sign outgoing mail with DKIM
  sign_outbound: true

  # Maximum message size in bytes (25MB = 26214400)
  max_message_size: 26214400

# Logging configuration
logging:
  # Log level: debug, info, warn, error
  level: info

  # Log format: json, text
  format: json

  # Log output: stdout, stderr, or file path
  output: stdout
```

## Environment Variables

Configuration values can be overridden with environment variables:

| Variable | Description |
|----------|-------------|
| `MAILSERVER_CONFIG` | Path to configuration file |
| `MAILSERVER_HOSTNAME` | Server hostname |
| `MAILSERVER_DATA_DIR` | Data directory path |
| `MAILSERVER_LOG_LEVEL` | Logging level |

## TLS Configuration

### Automatic TLS (Let's Encrypt)

The easiest way to configure TLS is using automatic certificate management:

```yaml
tls:
  auto_tls: true
  email: admin@example.com
  cache_dir: /var/lib/mailserver/acme
```

Requirements:
- Port 443 must be accessible for ACME challenge
- Valid DNS pointing to your server
- Email address for Let's Encrypt notifications

### Manual TLS Certificates

If you have existing certificates (e.g., from Cloudflare):

```yaml
tls:
  auto_tls: false
  cert_file: /etc/mailserver/certs/fullchain.pem
  key_file: /etc/mailserver/certs/privkey.pem
```

Certificate requirements:
- Full certificate chain (including intermediates)
- RSA or ECDSA private key
- SAN (Subject Alternative Names) should include:
  - mail.yourdomain.com
  - yourdomain.com (for DAV)

### Self-Signed Certificates (Development Only)

For testing, generate a self-signed certificate:

```bash
openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout /etc/mailserver/certs/privkey.pem \
  -out /etc/mailserver/certs/fullchain.pem \
  -subj "/CN=mail.localhost"
```

## DKIM Configuration

### Generating DKIM Keys

```bash
# Create directory
mkdir -p /etc/mailserver/dkim

# Generate 2048-bit RSA key
openssl genrsa -out /etc/mailserver/dkim/example.com.key 2048

# Extract public key
openssl rsa -in /etc/mailserver/dkim/example.com.key \
  -pubout -out /etc/mailserver/dkim/example.com.pub

# Set permissions
chmod 600 /etc/mailserver/dkim/example.com.key
chown mailserver:mailserver /etc/mailserver/dkim/example.com.key
```

### DNS Record Format

The DNS TXT record for DKIM should be:

```
mail._domainkey.example.com. IN TXT "v=DKIM1; k=rsa; p=<base64-encoded-public-key>"
```

Get the formatted record:
```bash
./mailserver dkim dns --domain example.com
```

### Testing DKIM

After configuring, test with:
```bash
# Send test email
echo "Test" | mail -s "DKIM Test" test@gmail.com

# Check headers in received email for DKIM-Signature
```

## Multi-Domain Setup

### Adding Multiple Domains

```yaml
domains:
  - name: primary.com
    dkim_selector: mail
    dkim_key_file: /etc/mailserver/dkim/primary.com.key

  - name: secondary.com
    dkim_selector: mail
    dkim_key_file: /etc/mailserver/dkim/secondary.com.key

  - name: alias.com
    dkim_selector: default
    dkim_key_file: /etc/mailserver/dkim/alias.com.key
```

### Adding Users for Each Domain

```bash
# Primary domain users
./mailserver user add admin@primary.com
./mailserver user add info@primary.com

# Secondary domain users
./mailserver user add contact@secondary.com
```

### Setting Up Aliases

Aliases allow forwarding mail to another user:

```bash
# Forward sales@primary.com to admin@primary.com
./mailserver alias add sales@primary.com admin@primary.com

# Forward to external address
./mailserver alias add external@primary.com someone@gmail.com
```

## Storage Configuration

### Maildir Structure

The server uses Maildir format for email storage:

```
/var/lib/mailserver/maildir/
└── user_1/
    ├── INBOX/
    │   ├── cur/      # Read messages
    │   ├── new/      # Unread messages
    │   └── tmp/      # Temp during delivery
    ├── Sent/
    ├── Drafts/
    ├── Trash/
    └── Archive/
```

### Backup Recommendations

```bash
# Backup database
sqlite3 /var/lib/mailserver/mail.db ".backup /backup/mail.db"

# Backup maildir (preserving permissions)
rsync -avz /var/lib/mailserver/maildir/ /backup/maildir/

# Backup DKIM keys
cp -r /etc/mailserver/dkim/ /backup/dkim/
```

### Storage Quotas

User quotas are configured per-user:

```bash
# Set 1GB quota for user
./mailserver user quota user@example.com 1073741824

# Check quota usage
./mailserver user info user@example.com
```

## Security Hardening

### Firewall Rules

```bash
# UFW (Ubuntu)
ufw allow 25/tcp    # SMTP
ufw allow 587/tcp   # Submission
ufw allow 465/tcp   # SMTPS
ufw allow 993/tcp   # IMAPS
ufw allow 8443/tcp  # DAV

# iptables
iptables -A INPUT -p tcp --dport 25 -j ACCEPT
iptables -A INPUT -p tcp --dport 587 -j ACCEPT
iptables -A INPUT -p tcp --dport 465 -j ACCEPT
iptables -A INPUT -p tcp --dport 993 -j ACCEPT
iptables -A INPUT -p tcp --dport 8443 -j ACCEPT
```

### Fail2Ban Configuration

Create `/etc/fail2ban/jail.d/mailserver.conf`:

```ini
[mailserver-imap]
enabled = true
port = 993
filter = mailserver-imap
logpath = /var/log/mailserver/imap.log
maxretry = 5
bantime = 3600

[mailserver-smtp]
enabled = true
port = 25,587,465
filter = mailserver-smtp
logpath = /var/log/mailserver/smtp.log
maxretry = 5
bantime = 3600
```

### Rate Limiting

```yaml
security:
  # Max connections per IP per minute
  max_connections_per_ip: 30

  # Max authentication failures before lockout
  max_auth_failures: 5

  # Lockout duration in seconds
  auth_lockout_duration: 3600
```

## Performance Tuning

### For High Load

```yaml
server:
  # Number of worker threads
  workers: auto  # Uses CPU count

  # Connection timeouts
  read_timeout: 300
  write_timeout: 300
  idle_timeout: 600

  # Max concurrent connections
  max_connections: 1000
```

### Memory Optimization

```yaml
storage:
  # SQLite cache size (pages)
  db_cache_size: 10000

  # Message body cache size (MB)
  body_cache_size: 256
```

## Logging

### Log Levels

- `debug`: All operations (verbose)
- `info`: Normal operations
- `warn`: Warnings and recoverable errors
- `error`: Errors only

### JSON Log Format

```json
{
  "timestamp": "2024-12-19T10:30:00Z",
  "level": "info",
  "component": "smtp",
  "message": "Message delivered",
  "from": "sender@example.com",
  "to": "recipient@example.com",
  "size": 1234
}
```

### Log Rotation

Using logrotate (`/etc/logrotate.d/mailserver`):

```
/var/log/mailserver/*.log {
    daily
    rotate 14
    compress
    delaycompress
    missingok
    notifempty
    create 0640 mailserver mailserver
    postrotate
        systemctl reload mailserver
    endscript
}
```
