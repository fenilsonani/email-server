# Configuration Reference

Default config path: `/etc/mailserver/config.yaml`. Override with `--config` flag or `MAILSERVER_CONFIG` env var.

## Full Reference

```yaml
server:
  hostname: mail.example.com
  smtp_port: 25
  submission_port: 587
  smtps_port: 465
  imap_port: 143
  imaps_port: 993
  dav_port: 8443

tls:
  # Let's Encrypt (requires port 443 accessible + valid DNS)
  auto_tls: true
  email: admin@example.com
  cache_dir: /var/lib/mailserver/acme

  # Manual certs (used when auto_tls: false)
  cert_file: /etc/mailserver/certs/fullchain.pem
  key_file: /etc/mailserver/certs/privkey.pem

storage:
  data_dir: /var/lib/mailserver
  database_path: /var/lib/mailserver/mail.db
  maildir_path: /var/lib/mailserver/maildir

domains:
  - name: example.com
    dkim_selector: mail
    dkim_key_file: /etc/mailserver/dkim/example.com.key

security:
  require_tls: true
  verify_spf: true
  verify_dkim: true
  verify_dmarc: true
  sign_outbound: true
  max_message_size: 26214400  # 25MB

logging:
  level: info      # debug, info, warn, error
  format: json     # json, text
  output: stdout   # stdout, stderr, or file path
```

## Environment Variables

| Variable | Description |
|----------|-------------|
| `MAILSERVER_CONFIG` | Config file path |
| `MAILSERVER_HOSTNAME` | Server hostname |
| `MAILSERVER_DATA_DIR` | Data directory |
| `MAILSERVER_LOG_LEVEL` | Log level |

## TLS

**Auto (Let's Encrypt):** Set `auto_tls: true`, provide an email, and ensure port 443 is reachable.

**Manual certs:** Set `auto_tls: false` and provide `cert_file`/`key_file`. Cert must include the full chain. SAN should cover `mail.yourdomain.com`.

**Self-signed (dev only):**
```bash
openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout privkey.pem -out fullchain.pem -subj "/CN=mail.localhost"
```

## DKIM

Use the CLI to manage DKIM keys (stored in the database):

```bash
mailserver dkim generate --domain example.com
mailserver dkim dns --domain example.com
```

The `dkim dns` command outputs the TXT record to add at `mail._domainkey.example.com`.

## Multi-Domain

Add multiple entries under `domains:` in config. Each domain needs its own DKIM key and DNS records.

```bash
mailserver domain add primary.com
mailserver domain add secondary.com
mailserver dkim generate --domain primary.com
mailserver dkim generate --domain secondary.com
```

## Aliases

```bash
mailserver alias add sales@example.com admin@example.com
mailserver alias add external@example.com someone@gmail.com
```

## Storage

Maildir layout:
```
/var/lib/mailserver/maildir/
└── user_1/
    ├── INBOX/{cur,new,tmp}
    ├── Sent/
    ├── Drafts/
    ├── Trash/
    └── Archive/
```

**Backups:**
```bash
sqlite3 /var/lib/mailserver/mail.db ".backup /backup/mail.db"
rsync -avz /var/lib/mailserver/maildir/ /backup/maildir/
```

**Quotas:**
```bash
mailserver user quota user@example.com 1073741824  # 1GB
```

## Firewall

```bash
# UFW
ufw allow 25/tcp 587/tcp 465/tcp 993/tcp 8443/tcp

# iptables
for port in 25 587 465 993 8443; do
  iptables -A INPUT -p tcp --dport $port -j ACCEPT
done
```

## Log Rotation

If logging to a file, add `/etc/logrotate.d/mailserver`:

```
/var/log/mailserver/*.log {
    daily
    rotate 14
    compress
    delaycompress
    missingok
    notifempty
}
```

When using `output: stdout` with systemd, journald handles rotation automatically.
