# Personal Email Server

A self-hosted email server written in Go. IMAP with IDLE, SMTP with retry logic, CalDAV/CardDAV, DKIM/SPF/DMARC, and a web admin panel.

## Features

- **IMAP** with IDLE push notifications
- **SMTP** with exponential backoff retries and per-domain circuit breakers
- **DKIM** signing, **SPF/DMARC** verification
- **CalDAV/CardDAV** for calendar and contacts sync
- **TLS/ACME** via Let's Encrypt
- **Greylisting**, rate limiting, audit logging
- **Argon2id** password hashing
- **Web admin panel** with user/domain/queue management
- **Prometheus metrics** and health endpoints
- **Auto-discovery** for Outlook, Apple Mail, Thunderbird
- **SQLite** metadata, **Redis** queue, **Maildir** storage
- Multi-domain, per-user quotas

## Requirements

- Go 1.25+
- Redis
- VPS with public IP and DNS control
- Ports: 25, 587, 465, 143, 993, 8080, 8443

## Quick Start

### Setup Wizard

```bash
git clone https://github.com/fenilsonani/email-server.git
cd email-server
go build -o mailserver ./cmd/mailserver

./mailserver preflight    # check prerequisites
./mailserver setup        # interactive setup
./mailserver doctor       # diagnose issues
```

### Manual Setup

**1. Build**

```bash
git clone https://github.com/fenilsonani/email-server.git
cd email-server
go build -o mailserver ./cmd/mailserver
```

**2. Configure**

```bash
sudo mkdir -p /etc/mailserver /var/lib/mailserver/maildir /var/lib/mailserver/queue
sudo cp configs/config.example.yaml /etc/mailserver/config.yaml
sudo nano /etc/mailserver/config.yaml
```

See `configs/config.example.yaml` for all available options. Key settings:

```yaml
server:
  hostname: mail.yourdomain.com

tls:
  auto_tls: true
  email: admin@yourdomain.com

storage:
  database_path: /var/lib/mailserver/mail.db
  maildir_path: /var/lib/mailserver/maildir

redis:
  address: localhost:6379

domains:
  - name: yourdomain.com
    dkim_selector: mail
```

**3. Initialize**

```bash
./mailserver migrate --config /etc/mailserver/config.yaml
./mailserver domain add yourdomain.com --config /etc/mailserver/config.yaml
./mailserver user add user@yourdomain.com --config /etc/mailserver/config.yaml
```

**4. DKIM**

```bash
./mailserver dkim generate yourdomain.com --config /etc/mailserver/config.yaml
./mailserver dkim show yourdomain.com --config /etc/mailserver/config.yaml
```

The `dkim show` command outputs the DNS TXT record to add. DKIM keys are stored in the database, not on disk.

**5. DNS Records**

| Type | Name | Value |
|------|------|-------|
| A | mail | `your.server.ip` |
| MX | @ | `mail.yourdomain.com` (priority 10) |
| TXT | @ | `v=spf1 mx a:mail.yourdomain.com -all` |
| TXT | mail._domainkey | output from `dkim show` |
| TXT | _dmarc | `v=DMARC1; p=quarantine; rua=mailto:postmaster@yourdomain.com` |

Optional auto-discovery records:

| Type | Name | Value |
|------|------|-------|
| CNAME | autodiscover | mail.yourdomain.com |
| CNAME | autoconfig | mail.yourdomain.com |
| SRV | _autodiscover._tcp | 0 0 443 mail.yourdomain.com |

**6. Start**

```bash
./mailserver serve --config /etc/mailserver/config.yaml
```

## Deployment

### systemd

```bash
sudo cp mailserver /usr/local/bin/
sudo cp deploy/mailserver.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now mailserver
journalctl -u mailserver -f
```

### Docker

```bash
cp configs/config.example.yaml config.yaml
# edit config.yaml
docker-compose up -d
```

## Admin Panel

Web-based admin at `http://localhost:8080` with dashboard, user/domain management, mail queue, delivery logs, DNS checker, mailing lists, and system diagnostics. Keyboard-navigable (vim-style shortcuts).

There's also a separate user portal where users can manage their own profile, forwarding rules, and vacation replies.

## CLI Reference

```bash
# Server
mailserver serve --config /etc/mailserver/config.yaml
mailserver migrate --config /etc/mailserver/config.yaml
mailserver preflight
mailserver setup
mailserver doctor

# Domains
mailserver domain add example.com
mailserver domain list
mailserver domain remove example.com

# Users
mailserver user add user@example.com
mailserver user add admin@example.com --admin
mailserver user list [--domain example.com]
mailserver user quota user@example.com 1073741824  # 1GB
mailserver user disable user@example.com
mailserver user enable user@example.com

# DKIM
mailserver dkim generate --domain example.com
mailserver dkim dns --domain example.com

# Queue
mailserver queue list
mailserver queue retry <message-id>
mailserver queue retry-all
mailserver queue delete <message-id>
mailserver queue stats
```

## Client Configuration

Most clients auto-configure via autodiscover DNS records. Manual settings:

| Protocol | Server | Port | Security |
|----------|--------|------|----------|
| IMAP | mail.yourdomain.com | 993 | SSL/TLS |
| SMTP | mail.yourdomain.com | 587 | STARTTLS |
| CalDAV | mail.yourdomain.com | 8443 | HTTPS (`/caldav/`) |
| CardDAV | mail.yourdomain.com | 8443 | HTTPS (`/carddav/`) |

Username is always the full email address.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                          Mail Server                            │
├──────────┬──────────┬──────────┬──────────┬──────────┬──────────┤
│ SMTP(25) │ Sub(587) │IMAP(993) │DAV(8443) │Admin(8080│ AutoDisc │
├──────────┴──────────┴──────────┴──────────┴──────────┴──────────┤
│  Rate Limiting · Greylisting · Audit Logging · TLS/ACME         │
├─────────────────────────────────────────────────────────────────┤
│  Argon2id Auth · User Quotas · Multi-Domain                     │
├─────────────────────────────────────────────────────────────────┤
│  Redis Queue · Circuit Breakers · Retry w/ Backoff · TLS Fallbk │
├─────────────────────────────────────────────────────────────────┤
│  SQLite (metadata) · Maildir (emails) · DKIM (signing)          │
├─────────────────────────────────────────────────────────────────┤
│  Prometheus Metrics · Audit Logs · Health Checks                │
└─────────────────────────────────────────────────────────────────┘
```

## Delivery Engine

Failed deliveries retry with exponential backoff (5min → 24hr cap), up to 15 attempts over 7 days, with ±10% jitter.

Per-domain circuit breakers open after 5 failures, half-open after 5 minutes, close after 2 successes.

TLS is attempted first. If verification fails and `require_tls: false`, delivery falls back to plaintext with a warning.

## Monitoring

```bash
curl http://localhost:8080/metrics          # Prometheus
curl http://localhost:8080/health           # basic
curl http://localhost:8080/health/detailed  # per-component
```

Key metrics: `mailserver_messages_{received,sent,rejected,bounced}_total`, `mailserver_delivery_duration_seconds`, `mailserver_queue_depth`, `mailserver_active_connections`, `mailserver_auth_attempts_total`.

## Troubleshooting

```bash
./mailserver preflight                     # check ports, DNS, Redis, TLS
./mailserver doctor                        # diagnose connectivity & deliverability

sudo ss -tlnp | grep mailserver           # check listening ports
dig MX yourdomain.com                     # verify DNS
openssl s_client -connect mail.yourdomain.com:993  # test TLS

journalctl -u mailserver -f               # logs (systemd)
docker-compose logs -f                     # logs (Docker)
```

## Testing

```bash
go test ./...
go test -v ./internal/auth/...
go test -v ./internal/smtp/delivery/...
```

## Security Notes

- Put the admin panel behind a reverse proxy with restricted access
- Use `auto_tls: true` in production
- Secure Redis (password, bind to localhost)
- Emails are encrypted in transit (TLS) but stored unencrypted in Maildir format — use full-disk encryption for at-rest protection

## License

MIT — see [LICENSE](LICENSE).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## Acknowledgments

Built with [go-imap](https://github.com/emersion/go-imap), [go-smtp](https://github.com/emersion/go-smtp), [go-msgauth](https://github.com/emersion/go-msgauth), [go-redis](https://github.com/redis/go-redis), [go-sqlite3](https://github.com/mattn/go-sqlite3), [prometheus client](https://github.com/prometheus/client_golang).
