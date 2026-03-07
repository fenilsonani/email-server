# Setup Guide: Linode + Cloudflare

Step-by-step deployment on a Linode VPS with Cloudflare DNS.

## Prerequisites

- Linode account, Cloudflare account with your domain added
- SSH access

## 1. Create Linode

1. **Create** → **Linode** → Ubuntu 24.04 LTS, Nanode 1GB ($5/mo) or Linode 2GB ($12/mo)
2. Add your SSH key, create, wait for **Running**
3. Copy the IP address

**Set reverse DNS (critical for deliverability):**

In Linode Dashboard → your Linode → **Network** → **Edit RDNS** on your IPv4 → set to `mail.yourdomain.com`.

![Linode Reverse DNS](images/linode-rdns.png)

The PTR record must match your mail server hostname exactly.

## 2. Cloudflare DNS

Add these records (all with proxy **OFF** / DNS only):

| Type | Name | Content | Priority |
|------|------|---------|----------|
| A | mail | your Linode IP | — |
| MX | @ | mail.yourdomain.com | 10 |
| TXT | @ | `v=spf1 mx a:mail.yourdomain.com -all` | — |
| TXT | _dmarc | `v=DMARC1; p=quarantine; rua=mailto:postmaster@yourdomain.com` | — |

DKIM record will be added after key generation in step 4.

**The A record for `mail` must have the orange proxy cloud OFF.** Cloudflare proxy breaks mail protocols.

![Cloudflare DNS Records](images/cloudflare-dns.png)

## 3. Server Setup

```bash
ssh root@YOUR_LINODE_IP

# System
apt update && apt upgrade -y
hostnamectl set-hostname mail.yourdomain.com
echo "YOUR_LINODE_IP    mail.yourdomain.com mail" >> /etc/hosts

# Firewall
apt install -y ufw
ufw allow 22/tcp 25/tcp 587/tcp 465/tcp 143/tcp 993/tcp 80/tcp 443/tcp 8443/tcp
ufw enable

# Install Go (check https://go.dev/dl/ for latest version)
wget https://go.dev/dl/go1.25.3.linux-amd64.tar.gz
rm -rf /usr/local/go && tar -C /usr/local -xzf go1.25.3.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc

# Create system user and directories
useradd --system --home-dir /var/lib/mailserver --shell /usr/sbin/nologin mailserver
mkdir -p /var/lib/mailserver/{maildir,acme} /etc/mailserver

# Build
cd /opt
git clone https://github.com/fenilsonani/email-server.git
cd email-server
CGO_ENABLED=1 go build -o /usr/local/bin/mailserver ./cmd/mailserver

# Permissions
chown -R mailserver:mailserver /var/lib/mailserver
chmod 750 /var/lib/mailserver
```

## 4. Configure and Initialize

```bash
cp configs/config.example.yaml /etc/mailserver/config.yaml
nano /etc/mailserver/config.yaml
# Set hostname, domain, TLS email — see docs/CONFIGURATION.md for full reference

chmod 640 /etc/mailserver/config.yaml
chown root:mailserver /etc/mailserver/config.yaml

# Initialize
mailserver migrate --config /etc/mailserver/config.yaml
mailserver domain add yourdomain.com --config /etc/mailserver/config.yaml
mailserver user add you@yourdomain.com --config /etc/mailserver/config.yaml

# DKIM
mailserver dkim generate yourdomain.com --config /etc/mailserver/config.yaml
mailserver dkim show yourdomain.com --config /etc/mailserver/config.yaml
```

Copy the DKIM output and add it as a TXT record in Cloudflare:

| Type | Name | Content |
|------|------|---------|
| TXT | mail._domainkey | `v=DKIM1; k=rsa; p=...` (from dkim show output) |

## 5. systemd Service

```bash
cp deploy/mailserver.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now mailserver
journalctl -u mailserver -f
```

## 6. Verify

```bash
# DNS
dig MX yourdomain.com +short              # → 10 mail.yourdomain.com.
dig A mail.yourdomain.com +short           # → your IP
dig TXT yourdomain.com +short              # → SPF record
dig TXT mail._domainkey.yourdomain.com +short  # → DKIM record
dig -x YOUR_LINODE_IP +short               # → mail.yourdomain.com.

# Ports
nc -zv mail.yourdomain.com 993
nc -zv mail.yourdomain.com 25
```

Send a test email and check headers for `DKIM: PASS`, `SPF: PASS`, `DMARC: PASS`. Use [mail-tester.com](https://www.mail-tester.com/) for a full deliverability check.

## 7. Maintenance

```bash
# Logs
journalctl -u mailserver -f
journalctl -u mailserver -p err

# Update
cd /opt/email-server
git pull
CGO_ENABLED=1 go build -o /usr/local/bin/mailserver ./cmd/mailserver
systemctl restart mailserver

# Backup
sqlite3 /var/lib/mailserver/mail.db ".backup /backup/mail.db.$(date +%Y%m%d)"
tar -czf /backup/maildir.$(date +%Y%m%d).tar.gz /var/lib/mailserver/maildir
```

## Troubleshooting

**Can't receive email:** Check MX record (`dig MX yourdomain.com`), port 25 (`nc -zv mail.yourdomain.com 25`), and that Cloudflare proxy is OFF for the `mail` A record.

**Certificate issues:** Ensure port 443 is open for Let's Encrypt. Check `/var/lib/mailserver/acme/`.

**DKIM failing:** Verify the DNS record matches `mailserver dkim show` output. Check `dig TXT mail._domainkey.yourdomain.com`.
