# Personal Email Server

A self-hosted email server written in Go, designed for individuals and small teams who want complete control over their email infrastructure. Features IMAP with IDLE push notifications, SMTP with intelligent delivery, CalDAV/CardDAV sync, and a web admin panel.

**Why self-host email in 2026?**
- Google Workspace prices increased 17-22% in 2025
- No corporate surveillance - your emails aren't scanned for ads or AI training
- Unlimited users for ~$72/year total
- No risk of account suspension
- Own your data on your server

## Features

### Core Email
- **IMAP Server** with IDLE support for real-time push notifications
- **SMTP Server** for sending and receiving with smart retry logic
- **POP3 Support** for legacy clients
- **DKIM Signing** for outbound email authentication
- **SPF/DMARC** verification for inbound security

### Calendar & Contacts
- **CalDAV Server** for calendar synchronization (Apple Calendar, Thunderbird, etc.)
- **CardDAV Server** for contacts synchronization
- Works seamlessly with Apple Mail, iOS, and other standards-compliant clients

### Security
- **TLS/ACME** automatic certificate management via Let's Encrypt
- **Argon2id** password hashing (OWASP recommended)
- **Greylisting** for spam prevention
- **Rate Limiting** to prevent brute force attacks
- **Audit Logging** for compliance and security monitoring
- **TLS Fallback** for servers with misconfigured certificates

> **Note on encryption**: Emails are encrypted in transit (TLS) but stored unencrypted on disk (standard Maildir format). This is similar to most email servers including Gmail. For at-rest encryption, use full-disk encryption (LUKS, FileVault, etc.) on your server.

### Administration
- **Web Admin Panel** for user/domain management
- **Prometheus Metrics** for monitoring (`/metrics` endpoint)
- **Health Endpoints** for uptime monitoring
- **Auto-discovery** for Outlook and Apple Mail automatic configuration
- **Setup Wizard** for guided installation

### Storage & Performance
- **SQLite** for metadata (lightweight, no external database needed)
- **Redis** for message queue (delivery retries, scheduling)
- **Maildir** format for email storage (standard, easy to backup)
- **User Quotas** with storage limit enforcement
- **Multi-domain** support

## Requirements

- Go 1.22 or later (for building)
- Redis server (for message queue)
- A VPS with a public IP address ($5/month is sufficient)
- A domain name with DNS control
- Ports: 25, 587, 465, 143, 993, 8080, 8443

## Quick Start

### Option 1: Setup Wizard (Recommended)

```bash
# Clone and build
git clone https://github.com/fenilsonani/email-server.git
cd email-server
go build -o mailserver ./cmd/mailserver

# Run preflight checks
./mailserver preflight

# Interactive setup wizard
./mailserver setup

# Diagnose any issues
./mailserver doctor
```

### Option 2: Manual Setup

#### 1. Build the Server

```bash
git clone https://github.com/fenilsonani/email-server.git
cd email-server
go build -o mailserver ./cmd/mailserver
```

#### 2. Initialize Configuration

```bash
# Create directories
sudo mkdir -p /etc/mailserver /var/lib/mailserver/maildir /var/lib/mailserver/queue

# Copy example config
sudo cp configs/config.example.yaml /etc/mailserver/config.yaml

# Edit configuration
sudo nano /etc/mailserver/config.yaml
```

#### 3. Install and Start Redis

```bash
# Ubuntu/Debian
sudo apt install redis-server
sudo systemctl enable redis-server
sudo systemctl start redis-server

# macOS
brew install redis
brew services start redis
```

#### 4. Configure Your Domain

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
  admin_port: 8080

tls:
  auto_tls: true
  email: admin@yourdomain.com
  cache_dir: /var/lib/mailserver/acme

storage:
  data_dir: /var/lib/mailserver
  database_path: /var/lib/mailserver/mail.db
  maildir_path: /var/lib/mailserver/maildir

redis:
  address: localhost:6379
  password: ""
  db: 0

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

# Greylisting for spam prevention
greylisting:
  enabled: true
  min_delay: 5m
  max_age: 840h  # 35 days

# Delivery settings
delivery:
  workers: 4
  require_tls: false
  verify_tls: true

logging:
  level: info
  format: json
  output: stdout
```

#### 5. Initialize Database & Add Users

```bash
# Run database migrations
./mailserver migrate --config /etc/mailserver/config.yaml

# Add your domain
./mailserver domain add yourdomain.com

# Create a user
./mailserver user add user@yourdomain.com
# You'll be prompted to enter a password
```

#### 6. Generate DKIM Key

```bash
# Create DKIM directory
sudo mkdir -p /etc/mailserver/dkim

# Generate DKIM key pair
openssl genrsa -out /etc/mailserver/dkim/yourdomain.com.key 2048
openssl rsa -in /etc/mailserver/dkim/yourdomain.com.key -pubout > /etc/mailserver/dkim/yourdomain.com.pub

# Get DNS record
./mailserver dkim dns --domain yourdomain.com
```

#### 7. Configure DNS Records

Add these DNS records to your domain:

| Type | Name | Value |
|------|------|-------|
| A | mail | your.server.ip |
| MX | @ | mail.yourdomain.com (priority 10) |
| TXT | @ | `v=spf1 mx a:mail.yourdomain.com -all` |
| TXT | mail._domainkey | (output from dkim dns command) |
| TXT | _dmarc | `v=DMARC1; p=quarantine; rua=mailto:postmaster@yourdomain.com` |

**For Auto-discovery (optional but recommended):**

| Type | Name | Value |
|------|------|-------|
| CNAME | autodiscover | mail.yourdomain.com |
| CNAME | autoconfig | mail.yourdomain.com |
| SRV | _autodiscover._tcp | 0 0 443 mail.yourdomain.com |

#### 8. Start the Server

```bash
./mailserver serve --config /etc/mailserver/config.yaml
```

## Deployment

### Using systemd (Recommended for Production)

```bash
# Copy binary to system location
sudo cp mailserver /usr/local/bin/

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
cp configs/config.example.yaml config.yaml
# Edit config.yaml with your settings
docker-compose up -d
```

## Admin Panel

Access the web admin panel at `http://localhost:8080` (or behind your reverse proxy).

### Features:
- Dashboard with server statistics
- User management (create, edit, delete, disable)
- Domain management
- Mail queue monitoring (view, retry, delete messages)
- Audit logs
- Real-time metrics

### Default Login:
Set admin credentials in your config or create via CLI:
```bash
./mailserver user add admin@yourdomain.com --admin
```

## Monitoring

### Prometheus Metrics

Metrics are exposed at `/metrics` on the admin port:

```bash
curl http://localhost:8080/metrics
```

**Available Metrics:**
| Metric | Type | Description |
|--------|------|-------------|
| `mailserver_messages_received_total` | Counter | Total inbound messages |
| `mailserver_messages_sent_total` | Counter | Successful deliveries |
| `mailserver_messages_rejected_total` | Counter | Rejected messages (by reason) |
| `mailserver_messages_bounced_total` | Counter | Bounced messages |
| `mailserver_delivery_duration_seconds` | Histogram | Delivery latency |
| `mailserver_queue_depth` | Gauge | Current queue size |
| `mailserver_auth_attempts_total` | Counter | Auth attempts (by result/protocol) |
| `mailserver_quota_exceeded_total` | Counter | Quota rejections |
| `mailserver_active_connections` | Gauge | Active connections (by protocol) |

### Health Endpoints

```bash
# Basic health check
curl http://localhost:8080/health

# Detailed health with component status
curl http://localhost:8080/health/detailed
```

### Grafana Dashboard

Import the included Grafana dashboard from `deploy/grafana-dashboard.json` for visualization.

## Client Configuration

### Auto-Discovery

Most modern email clients (Outlook, Apple Mail, Thunderbird) will automatically configure themselves if you've set up the autodiscover DNS records.

### Manual Configuration

#### Apple Mail (macOS/iOS)

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

#### Calendar (CalDAV)

- Server URL: `https://mail.yourdomain.com:8443/caldav/`
- Or use: `https://mail.yourdomain.com:8443/.well-known/caldav`
- Username: user@yourdomain.com
- Password: your password

#### Contacts (CardDAV)

- Server URL: `https://mail.yourdomain.com:8443/carddav/`
- Or use: `https://mail.yourdomain.com:8443/.well-known/carddav`
- Username: user@yourdomain.com
- Password: your password

## Architecture

```
┌───────────────────────────────────────────────────────────────────────┐
│                             Mail Server                               │
├───────────┬───────────┬───────────┬───────────┬───────────┬───────────┤
│  SMTP(25) │  Sub(587) │ IMAP(993) │ DAV(8443) │Admin(8080)│ AutoDisc  │
├───────────┴───────────┴───────────┴───────────┴───────────┴───────────┤
│                           Security Layer                              │
│  ┌──────────────┐ ┌──────────────┐ ┌──────────────┐ ┌──────────────┐  │
│  │  Rate Limit  │ │  Greylisting │ │    Audit     │ │   TLS/ACME   │  │
│  └──────────────┘ └──────────────┘ └──────────────┘ └──────────────┘  │
├───────────────────────────────────────────────────────────────────────┤
│                        Authentication Layer                           │
│  ┌───────────────────┐ ┌───────────────────┐ ┌───────────────────┐    │
│  │     Argon2id      │ │    User Quotas    │ │    Multi-Domain   │    │
│  └───────────────────┘ └───────────────────┘ └───────────────────┘    │
├───────────────────────────────────────────────────────────────────────┤
│                           Delivery Engine                             │
│  ┌──────────────┐ ┌──────────────┐ ┌──────────────┐ ┌──────────────┐  │
│  │    Redis     │ │   Circuit    │ │   Retry w/   │ │     TLS      │  │
│  │    Queue     │ │   Breakers   │ │   Backoff    │ │   Fallback   │  │
│  └──────────────┘ └──────────────┘ └──────────────┘ └──────────────┘  │
├───────────────────────────────────────────────────────────────────────┤
│                            Storage Layer                              │
│  ┌───────────────────┐ ┌───────────────────┐ ┌───────────────────┐    │
│  │      SQLite       │ │      Maildir      │ │       DKIM        │    │
│  │    (metadata)     │ │     (emails)      │ │     (signing)     │    │
│  └───────────────────┘ └───────────────────┘ └───────────────────┘    │
├───────────────────────────────────────────────────────────────────────┤
│                            Observability                              │
│  ┌───────────────────┐ ┌───────────────────┐ ┌───────────────────┐    │
│  │    Prometheus     │ │      Audit        │ │      Health       │    │
│  │     Metrics       │ │       Logs        │ │      Checks       │    │
│  └───────────────────┘ └───────────────────┘ └───────────────────┘    │
└───────────────────────────────────────────────────────────────────────┘
```

## CLI Commands

### Server Commands

```bash
# Start the server
mailserver serve --config /etc/mailserver/config.yaml

# Run database migrations
mailserver migrate --config /etc/mailserver/config.yaml

# Pre-flight checks (before setup)
mailserver preflight

# Interactive setup wizard
mailserver setup

# Diagnose issues
mailserver doctor
```

### Domain Management

```bash
# Add a domain
mailserver domain add example.com

# List domains
mailserver domain list

# Remove a domain
mailserver domain remove example.com
```

### User Management

```bash
# Add a user (interactive password prompt)
mailserver user add user@example.com

# Add a user with password
mailserver user add user@example.com --password "secretpassword"

# Add an admin user
mailserver user add admin@example.com --admin

# List users
mailserver user list

# List users for specific domain
mailserver user list --domain example.com

# Set user quota (in bytes, 0 = unlimited)
mailserver user quota user@example.com 1073741824  # 1GB

# Disable a user
mailserver user disable user@example.com

# Enable a user
mailserver user enable user@example.com
```

### DKIM Management

```bash
# Generate DKIM key for a domain
mailserver dkim generate --domain example.com

# Show DNS record for DKIM
mailserver dkim dns --domain example.com
```

### Queue Management

```bash
# List queued messages
mailserver queue list

# Retry a specific message
mailserver queue retry <message-id>

# Retry all failed messages
mailserver queue retry-all

# Delete a message from queue
mailserver queue delete <message-id>

# Show queue statistics
mailserver queue stats
```

## Spam Prevention

### Greylisting

Greylisting temporarily rejects emails from unknown senders. Legitimate servers retry; spammers usually don't.

**How it works:**
1. First email from new sender → "try again in 5 minutes"
2. Sender retries after 5+ minutes → accepted, remembered for 35 days
3. Future emails from same sender → accepted immediately

**Configuration:**
```yaml
greylisting:
  enabled: true
  min_delay: 5m      # Minimum wait time
  max_age: 840h      # Remember senders for 35 days
```

### Additional Recommendations

For production use, consider adding:
- **Rspamd** or **SpamAssassin** for content filtering
- **Fail2ban** for additional brute force protection
- **ClamAV** for virus scanning

## Delivery Engine

### Smart Retry Logic

Failed deliveries use exponential backoff with jitter:

```
Attempt 1:  5 minutes
Attempt 2:  15 minutes
Attempt 3:  30 minutes
Attempt 4:  1 hour
Attempt 5:  2 hours
Attempt 6:  4 hours
Attempt 7:  8 hours
Attempt 8:  16 hours
Attempt 9+: 24 hours (capped)
```

- Maximum 15 retry attempts
- Messages expire after 7 days
- ±10% jitter prevents thundering herd

### Per-Domain Circuit Breakers

If a destination domain is having issues, the circuit breaker prevents wasting resources:

- **5 failures** → circuit opens (stop trying)
- **5 minutes** → circuit half-opens (try one)
- **2 successes** → circuit closes (normal operation)

### TLS Handling

The server intelligently handles TLS:
- Attempts STARTTLS first
- If certificate verification fails and `require_tls: false`, reconnects without TLS
- Logs warning but delivers the message
- ~15% of mail servers have misconfigured certificates

## Security Considerations

1. **Firewall**: Only open required ports (25, 587, 465, 143, 993, 8443)
2. **Admin Panel**: Put behind reverse proxy with HTTPS, restrict access
3. **TLS**: Always use `auto_tls: true` in production
4. **Passwords**: Use strong passwords (Argon2id hashed automatically)
5. **DKIM**: Always sign outbound emails
6. **SPF/DMARC**: Configure proper DNS records
7. **Updates**: Keep the server and dependencies updated
8. **Backups**: Regular backups of `/var/lib/mailserver/`
9. **Redis**: Secure Redis if exposed (use password, bind to localhost)

## Troubleshooting

### Pre-flight Checks

```bash
./mailserver preflight
```

This checks:
- Port availability
- DNS configuration
- Redis connectivity
- Directory permissions
- TLS certificate status

### Doctor Command

```bash
./mailserver doctor
```

Diagnoses common issues:
- Service connectivity
- DNS record validation
- Deliverability testing
- Storage health

### Connection Refused

Check if ports are open:
```bash
sudo ss -tlnp | grep mailserver
```

Check firewall:
```bash
sudo ufw status
```

### TLS Certificate Issues

Check certificate status:
```bash
ls -la /var/lib/mailserver/acme/
openssl s_client -connect mail.yourdomain.com:993 -servername mail.yourdomain.com
```

### Email Not Delivered

Check DNS records:
```bash
dig MX yourdomain.com
dig TXT yourdomain.com
dig TXT mail._domainkey.yourdomain.com
dig TXT _dmarc.yourdomain.com
```

Test deliverability:
```bash
# Use mail-tester.com for comprehensive test
# Send an email to their test address and check score
```

### View Logs

```bash
# systemd
journalctl -u mailserver -f

# Docker
docker-compose logs -f

# Filter by component
journalctl -u mailserver | grep delivery
journalctl -u mailserver | grep error
```

### Queue Issues

```bash
# Check queue depth
redis-cli ZCARD mail:queue:pending

# View message details
redis-cli GET mail:message:<message-id>

# Check delivery stats
redis-cli HGETALL mail:stats
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
go test -v ./internal/metrics/...
go test -v ./internal/audit/...
go test -v ./internal/greylist/...
go test -v ./internal/smtp/delivery/...
```

## Resource Requirements

**Minimum (personal use):**
- 1 vCPU
- 1GB RAM
- 20GB storage
- Cost: ~$5/month

**Recommended (small team):**
- 2 vCPU
- 2GB RAM
- 50GB storage
- Cost: ~$10-15/month

**Actual Usage:**
- ~50MB RAM idle
- ~100MB RAM under load
- CPU: minimal except during delivery spikes

## Cost Comparison (2026)

| Solution | Cost (10 users) | Per-User | Privacy |
|----------|-----------------|----------|---------|
| Google Workspace | $1,680/year | $168/year | Low |
| Microsoft 365 | $1,500/year | $150/year | Low |
| ProtonMail Business | $900/year | $90/year | High |
| **Self-Hosted** | **$72/year** | **$7.20/year** | **Maximum** |

## License

MIT License - see LICENSE file for details.

## Contributing

Contributions are welcome! Please:

1. Open an issue to discuss major changes
2. Fork the repository
3. Create a feature branch
4. Add tests for new functionality
5. Submit a pull request

## Support

- **Issues**: [GitHub Issues](https://github.com/fenilsonani/email-server/issues)
- **Discussions**: [GitHub Discussions](https://github.com/fenilsonani/email-server/discussions)

## Acknowledgments

Built with:
- [go-imap](https://github.com/emersion/go-imap) - IMAP library
- [go-smtp](https://github.com/emersion/go-smtp) - SMTP library
- [go-msgauth](https://github.com/emersion/go-msgauth) - DKIM/SPF
- [go-redis](https://github.com/redis/go-redis) - Redis client
- [mattn/go-sqlite3](https://github.com/mattn/go-sqlite3) - SQLite driver
- [prometheus/client_golang](https://github.com/prometheus/client_golang) - Metrics
