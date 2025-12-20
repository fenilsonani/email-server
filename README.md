# Personal Email Server

A self-hosted email server written in Go, designed for personal use with Apple Mail and other standard email clients. Features IMAP with IDLE push notifications, SMTP, CalDAV calendar sync, and CardDAV contacts sync.

## Features

- **IMAP Server** with IDLE support for push notifications
- **SMTP Server** for sending and receiving emails
- **CalDAV Server** for calendar synchronization
- **CardDAV Server** for contacts synchronization
- **DKIM Signing** for outbound email authentication
- **TLS/ACME** automatic certificate management via Let's Encrypt
- **SQLite** for metadata storage (lightweight, no external database needed)
- **Maildir** format for email storage (standard, easy to backup)
- **Multi-domain** support
- **Argon2id** password hashing for security

## Requirements

- Go 1.21 or later (for building)
- A VPS with a public IP address
- A domain name with DNS control
- Ports 25, 587, 465, 993, and 8443 open

## Quick Start

### 1. Build the Server

```bash
git clone https://github.com/fenilsonani/email-server.git
cd email-server
go build -o mailserver ./cmd/mailserver
```

### 2. Initialize Configuration

```bash
# Create directories
sudo mkdir -p /etc/mailserver /var/lib/mailserver/maildir

# Copy example config
sudo cp configs/config.example.yaml /etc/mailserver/config.yaml

# Edit configuration
sudo nano /etc/mailserver/config.yaml
```

### 3. Configure Your Domain

Edit `/etc/mailserver/config.yaml`:

```yaml
server:
  hostname: mail.yourdomain.com
  smtp_port: 25
  submission_port: 587
  smtps_port: 465
  imap_port: 143
  imaps_port: 993
  dav_port: 8443

tls:
  auto_tls: true
  email: admin@yourdomain.com
  cache_dir: /var/lib/mailserver/acme

storage:
  data_dir: /var/lib/mailserver
  database_path: /var/lib/mailserver/mail.db
  maildir_path: /var/lib/mailserver/maildir

domains:
  - name: yourdomain.com
    dkim_selector: mail
    dkim_key_file: /etc/mailserver/dkim/yourdomain.com.key

security:
  require_tls: true
  verify_spf: true
  verify_dkim: true
  verify_dmarc: true
  sign_outbound: true
  max_message_size: 26214400  # 25MB

logging:
  level: info
  format: json
  output: stdout
```

### 4. Initialize Database

```bash
./mailserver migrate --config /etc/mailserver/config.yaml
```

### 5. Add Domain and User

```bash
# Add your domain
./mailserver domain add yourdomain.com

# Create a user
./mailserver user add user@yourdomain.com
# You'll be prompted to enter a password
```

### 6. Generate DKIM Key

```bash
# Generate DKIM key pair
openssl genrsa -out /etc/mailserver/dkim/yourdomain.com.key 2048
openssl rsa -in /etc/mailserver/dkim/yourdomain.com.key -pubout > /etc/mailserver/dkim/yourdomain.com.pub

# Get DNS record
./mailserver dkim dns --domain yourdomain.com
```

### 7. Configure DNS Records

Add these DNS records to your domain:

| Type | Name | Value |
|------|------|-------|
| A | mail | your.server.ip |
| MX | @ | mail.yourdomain.com (priority 10) |
| TXT | @ | `v=spf1 mx a:mail.yourdomain.com -all` |
| TXT | mail._domainkey | (output from dkim dns command) |
| TXT | _dmarc | `v=DMARC1; p=quarantine; rua=mailto:postmaster@yourdomain.com` |

### 8. Start the Server

```bash
./mailserver serve --config /etc/mailserver/config.yaml
```

## Deployment Options

### Using systemd

```bash
# Install service
sudo cp deploy/mailserver.service /etc/systemd/system/
sudo systemctl daemon-reload

# Start and enable
sudo systemctl start mailserver
sudo systemctl enable mailserver

# View logs
sudo journalctl -u mailserver -f
```

### Using Docker

```bash
# Build image
docker build -t mailserver .

# Run with docker-compose
cp config.example.yaml config.yaml
# Edit config.yaml with your settings
docker-compose up -d
```

## Client Configuration

### Apple Mail (macOS/iOS)

**IMAP Settings:**
- Server: mail.yourdomain.com
- Port: 993
- SSL: Yes
- Username: user@yourdomain.com

**SMTP Settings:**
- Server: mail.yourdomain.com
- Port: 587
- SSL: STARTTLS
- Authentication: Password
- Username: user@yourdomain.com

### Calendar (CalDAV)

- Server URL: https://mail.yourdomain.com:8443/caldav/
- Username: user@yourdomain.com
- Password: your password

### Contacts (CardDAV)

- Server URL: https://mail.yourdomain.com:8443/carddav/
- Username: user@yourdomain.com
- Password: your password

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    Mail Server                          │
├─────────────┬─────────────┬─────────────┬──────────────┤
│  SMTP (25)  │ Submit(587) │ IMAP (993)  │  DAV (8443)  │
├─────────────┴─────────────┴─────────────┴──────────────┤
│                  Authentication Layer                   │
├────────────────────────────────────────────────────────┤
│                    Storage Layer                        │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │
│  │   SQLite     │  │   Maildir    │  │    DKIM      │  │
│  │  (metadata)  │  │   (emails)   │  │   (signing)  │  │
│  └──────────────┘  └──────────────┘  └──────────────┘  │
└────────────────────────────────────────────────────────┘
```

## Directory Structure

```
/var/lib/mailserver/
├── mail.db              # SQLite database
├── maildir/             # Email storage
│   └── user_1/          # Per-user maildir
│       ├── INBOX/
│       │   ├── cur/     # Read messages
│       │   ├── new/     # New messages
│       │   └── tmp/     # Temp storage
│       ├── Sent/
│       └── Drafts/
└── acme/                # Let's Encrypt certificates

/etc/mailserver/
├── config.yaml          # Main configuration
└── dkim/                # DKIM keys
    ├── yourdomain.com.key
    └── yourdomain.com.pub
```

## CLI Commands

### Server Commands

```bash
# Start the server
mailserver serve --config /etc/mailserver/config.yaml

# Run database migrations
mailserver migrate --config /etc/mailserver/config.yaml
```

### Domain Management

```bash
# Add a domain
mailserver domain add example.com

# List domains
mailserver domain list
```

### User Management

```bash
# Add a user (interactive password prompt)
mailserver user add user@example.com

# Add a user with password
mailserver user add user@example.com --password "secretpassword"

# List users
mailserver user list

# List users for specific domain
mailserver user list --domain example.com
```

### DKIM Management

```bash
# Generate DKIM key for a domain
mailserver dkim generate --domain example.com

# Show DNS record for DKIM
mailserver dkim dns --domain example.com
```

## Security Considerations

1. **Firewall**: Only open required ports (25, 587, 465, 993, 8443)
2. **TLS**: Always use TLS in production (auto_tls: true)
3. **Passwords**: Use strong passwords (Argon2id hashed)
4. **DKIM**: Always sign outbound emails
5. **SPF/DMARC**: Configure proper DNS records
6. **Updates**: Keep the server updated

## Troubleshooting

### Connection Refused

Check if ports are open:
```bash
sudo ss -tlnp | grep mailserver
```

### TLS Certificate Issues

Check certificate status:
```bash
ls -la /var/lib/mailserver/acme/
```

### Email Not Delivered

Check DNS records:
```bash
dig MX yourdomain.com
dig TXT yourdomain.com
dig TXT mail._domainkey.yourdomain.com
```

### View Logs

```bash
# systemd
journalctl -u mailserver -f

# Docker
docker-compose logs -f
```

## Testing

Run all tests:
```bash
go test ./...
```

Run with verbose output:
```bash
go test -v ./...
```

Run specific package tests:
```bash
go test -v ./internal/auth/...
go test -v ./internal/storage/maildir/...
go test -v ./internal/dav/...
go test -v ./internal/security/...
go test -v ./tests/...
```

## API Reference

### IMAP (RFC 3501)
- Standard IMAP4rev1 protocol
- IDLE extension for push notifications
- Mailbox operations: SELECT, CREATE, DELETE, RENAME
- Message operations: FETCH, STORE, COPY, MOVE, EXPUNGE

### SMTP (RFC 5321)
- Standard SMTP protocol
- AUTH PLAIN for authenticated submission
- STARTTLS for encryption
- Size limits configurable

### CalDAV (RFC 4791)
- Calendar discovery via .well-known/caldav
- PROPFIND for listing calendars
- REPORT for calendar queries
- PUT/DELETE for events
- iCalendar format (RFC 5545)

### CardDAV (RFC 6352)
- Addressbook discovery via .well-known/carddav
- PROPFIND for listing address books
- REPORT for contact queries
- PUT/DELETE for contacts
- vCard format (RFC 6350)

## License

MIT License - see LICENSE file for details.

## Contributing

Contributions are welcome! Please open an issue or submit a pull request.
