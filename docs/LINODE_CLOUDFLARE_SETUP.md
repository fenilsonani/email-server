# Complete Setup Guide: Linode + Cloudflare

This guide walks you through setting up your personal email server on Linode with DNS managed by Cloudflare.

## Prerequisites

- A Linode account (https://linode.com)
- A Cloudflare account with your domain added (https://cloudflare.com)
- Your domain (e.g., `yourdomain.com`)
- SSH client (Terminal on Mac/Linux, or PuTTY on Windows)

---

## Part 1: Create Linode VPS

### Step 1.1: Create a New Linode

1. Log in to [Linode Cloud Manager](https://cloud.linode.com)
2. Click **Create** → **Linode**
3. Choose settings:

| Setting | Recommended Value |
|---------|-------------------|
| **Image** | Ubuntu 24.04 LTS |
| **Region** | Choose closest to you (e.g., Newark, NJ or London) |
| **Plan** | Shared CPU - Nanode 1GB ($5/mo) or Linode 2GB ($12/mo) |
| **Label** | `mail-server` |
| **Root Password** | Create a strong password |
| **SSH Keys** | Add your public SSH key (recommended) |

4. Click **Create Linode**
5. Wait for status to show **Running**
6. **Copy the IP address** (e.g., `192.0.2.123`) - you'll need this!

### Step 1.2: Set Reverse DNS (PTR Record)

This is **critical** for email deliverability!

1. In Linode Dashboard, click on your Linode
2. Go to **Network** tab
3. Find your IPv4 address
4. Click **Edit RDNS** (or the three dots → Edit RDNS)
5. Enter: `mail.yourdomain.com`
6. Click **Save**

> ⚠️ **Important**: The PTR record must match your mail server hostname exactly.

---

## Part 2: Configure Cloudflare DNS

### Step 2.1: Log into Cloudflare

1. Go to [Cloudflare Dashboard](https://dash.cloudflare.com)
2. Select your domain

### Step 2.2: Add DNS Records

Go to **DNS** → **Records** and add these records:

#### A Record (Mail Server)
| Type | Name | Content | Proxy | TTL |
|------|------|---------|-------|-----|
| A | `mail` | `192.0.2.123` (your Linode IP) | **DNS only** (gray cloud) | Auto |

> ⚠️ **IMPORTANT**: The proxy must be **OFF** (gray cloud) for mail to work!

#### MX Record (Mail Exchange)
| Type | Name | Content | Priority | Proxy | TTL |
|------|------|---------|----------|-------|-----|
| MX | `@` | `mail.yourdomain.com` | 10 | - | Auto |

#### SPF Record (Sender Policy Framework)
| Type | Name | Content | TTL |
|------|------|---------|-----|
| TXT | `@` | `v=spf1 mx a:mail.yourdomain.com -all` | Auto |

#### DMARC Record
| Type | Name | Content | TTL |
|------|------|---------|-----|
| TXT | `_dmarc` | `v=DMARC1; p=quarantine; rua=mailto:postmaster@yourdomain.com` | Auto |

> 📝 **Note**: We'll add the DKIM record later after generating the key.

### Step 2.3: Verify DNS Records

Your DNS records should look like this:

```
Type    Name        Content                                         Proxy
─────────────────────────────────────────────────────────────────────────
A       mail        192.0.2.123                                     DNS only
MX      @           mail.yourdomain.com (Priority: 10)              -
TXT     @           v=spf1 mx a:mail.yourdomain.com -all            -
TXT     _dmarc      v=DMARC1; p=quarantine; rua=mailto:postmaster@  -
```

---

## Part 3: Server Setup

### Step 3.1: Connect to Your Server

```bash
ssh root@192.0.2.123
```

Or if you added SSH key:
```bash
ssh root@mail.yourdomain.com
```

### Step 3.2: Initial Server Setup

```bash
# Update system
apt update && apt upgrade -y

# Set hostname
hostnamectl set-hostname mail.yourdomain.com

# Edit /etc/hosts
nano /etc/hosts
```

Add this line to `/etc/hosts`:
```
192.0.2.123    mail.yourdomain.com mail
```

Save and exit (Ctrl+X, Y, Enter)

```bash
# Install required packages
apt install -y git wget curl ufw

# Configure firewall
ufw allow 22/tcp    # SSH
ufw allow 25/tcp    # SMTP
ufw allow 587/tcp   # Submission
ufw allow 465/tcp   # SMTPS
ufw allow 143/tcp   # IMAP
ufw allow 993/tcp   # IMAPS
ufw allow 80/tcp    # HTTP (for Let's Encrypt)
ufw allow 443/tcp   # HTTPS
ufw allow 8443/tcp  # CalDAV/CardDAV
ufw enable

# Verify firewall
ufw status
```

### Step 3.3: Install Go

```bash
# Download Go
wget https://go.dev/dl/go1.22.0.linux-amd64.tar.gz

# Install
rm -rf /usr/local/go && tar -C /usr/local -xzf go1.22.0.linux-amd64.tar.gz

# Add to PATH
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc

# Verify
go version
```

### Step 3.4: Install the Mail Server

```bash
# Create mail server user
useradd --system --home-dir /var/lib/mailserver --shell /usr/sbin/nologin mailserver

# Create directories
mkdir -p /var/lib/mailserver/maildir
mkdir -p /var/lib/mailserver/acme
mkdir -p /etc/mailserver/dkim
mkdir -p /opt/mailserver

# Clone repository
cd /opt/mailserver
git clone https://github.com/fenilsonani/email-server.git .

# Build
go build -o /usr/local/bin/mailserver ./cmd/mailserver

# Set permissions
chown -R mailserver:mailserver /var/lib/mailserver
chmod 750 /var/lib/mailserver
```

### Step 3.5: Generate DKIM Key

```bash
# Generate 2048-bit RSA key
openssl genrsa -out /etc/mailserver/dkim/yourdomain.com.key 2048

# Extract public key for DNS
openssl rsa -in /etc/mailserver/dkim/yourdomain.com.key -pubout -outform PEM | \
  grep -v "PUBLIC KEY" | tr -d '\n'

# Set permissions
chmod 600 /etc/mailserver/dkim/yourdomain.com.key
chown mailserver:mailserver /etc/mailserver/dkim/yourdomain.com.key
```

**Copy the output** - this is your DKIM public key for DNS.

### Step 3.6: Add DKIM Record to Cloudflare

Go back to Cloudflare DNS and add:

| Type | Name | Content | TTL |
|------|------|---------|-----|
| TXT | `mail._domainkey` | `v=DKIM1; k=rsa; p=YOUR_PUBLIC_KEY_HERE` | Auto |

Replace `YOUR_PUBLIC_KEY_HERE` with the key from the previous step (the long base64 string).

### Step 3.7: Create Configuration

```bash
nano /etc/mailserver/config.yaml
```

Paste this configuration (replace `yourdomain.com` with your actual domain):

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
  max_message_size: 26214400

logging:
  level: info
  format: json
  output: stdout
```

Set permissions:
```bash
chmod 640 /etc/mailserver/config.yaml
chown root:mailserver /etc/mailserver/config.yaml
```

### Step 3.8: Initialize Database

```bash
/usr/local/bin/mailserver migrate --config /etc/mailserver/config.yaml
```

### Step 3.9: Add Domain and User

```bash
# Add your domain
/usr/local/bin/mailserver domain add yourdomain.com --config /etc/mailserver/config.yaml

# Add your first user (you'll be prompted for password)
/usr/local/bin/mailserver user add you@yourdomain.com --config /etc/mailserver/config.yaml
```

### Step 3.10: Install systemd Service

```bash
# Copy service file
cat > /etc/systemd/system/mailserver.service << 'EOF'
[Unit]
Description=Personal Mail Server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=mailserver
Group=mailserver
WorkingDirectory=/var/lib/mailserver
ExecStart=/usr/local/bin/mailserver serve --config /etc/mailserver/config.yaml
Restart=on-failure
RestartSec=5

# Security
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=/var/lib/mailserver
ReadOnlyPaths=/etc/mailserver

# Allow binding to privileged ports
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
EOF

# Reload systemd
systemctl daemon-reload

# Start and enable
systemctl start mailserver
systemctl enable mailserver

# Check status
systemctl status mailserver
```

### Step 3.11: Verify Server is Running

```bash
# Check logs
journalctl -u mailserver -f

# Test IMAP port
nc -zv localhost 993

# Test SMTP port
nc -zv localhost 25
```

---

## Part 4: Verify DNS Configuration

### Step 4.1: Check DNS Propagation

Wait 5-10 minutes for DNS to propagate, then verify:

```bash
# Check MX record
dig MX yourdomain.com +short
# Should show: 10 mail.yourdomain.com.

# Check A record
dig A mail.yourdomain.com +short
# Should show: 192.0.2.123

# Check SPF
dig TXT yourdomain.com +short
# Should show: "v=spf1 mx a:mail.yourdomain.com -all"

# Check DKIM
dig TXT mail._domainkey.yourdomain.com +short
# Should show: "v=DKIM1; k=rsa; p=..."

# Check DMARC
dig TXT _dmarc.yourdomain.com +short
# Should show: "v=DMARC1; p=quarantine; ..."

# Check reverse DNS
dig -x 192.0.2.123 +short
# Should show: mail.yourdomain.com.
```

### Step 4.2: Test Email Connectivity

Use online tools to verify:

1. **MX Toolbox**: https://mxtoolbox.com/
   - Enter `yourdomain.com`
   - Check MX, SPF, DKIM, DMARC

2. **Mail Tester**: https://www.mail-tester.com/
   - Send a test email to the provided address
   - Check your score (aim for 9-10/10)

---

## Part 5: Configure Email Client

### Apple Mail (Mac)

1. Open **System Settings** → **Internet Accounts**
2. Click **Add Account** → **Add Other Account** → **Mail Account**
3. Enter:
   - Name: Your Name
   - Email: you@yourdomain.com
   - Password: your password
4. Configure servers:

**Incoming (IMAP):**
- Server: `mail.yourdomain.com`
- Port: `993`
- SSL: Yes

**Outgoing (SMTP):**
- Server: `mail.yourdomain.com`
- Port: `587`
- SSL: STARTTLS
- Authentication: Password

### Apple Mail (iPhone/iPad)

1. **Settings** → **Mail** → **Accounts** → **Add Account**
2. Tap **Other** → **Add Mail Account**
3. Enter your email and password
4. Configure same servers as above

### Calendar & Contacts (Mac)

**Calendar:**
1. **System Settings** → **Internet Accounts**
2. **Add Account** → **Add Other Account** → **CalDAV Account**
3. Enter:
   - Account Type: Manual
   - Username: you@yourdomain.com
   - Password: your password
   - Server: `mail.yourdomain.com:8443/caldav/`

**Contacts:**
1. **System Settings** → **Internet Accounts**
2. **Add Account** → **Add Other Account** → **CardDAV Account**
3. Enter:
   - Account Type: Manual
   - Username: you@yourdomain.com
   - Password: your password
   - Server: `mail.yourdomain.com:8443/carddav/`

---

## Part 6: Testing

### Send Test Email

From your new email account, send an email to a Gmail or other provider:

```
To: yourother@gmail.com
Subject: Test from my mail server

This is a test email from my personal mail server!
```

### Check Email Headers

In Gmail, open the received email:
1. Click three dots → **Show original**
2. Look for:
   - `DKIM: PASS`
   - `SPF: PASS`
   - `DMARC: PASS`

---

## Part 7: Maintenance

### View Logs

```bash
# Live logs
journalctl -u mailserver -f

# Last 100 lines
journalctl -u mailserver -n 100

# Errors only
journalctl -u mailserver -p err
```

### Restart Server

```bash
systemctl restart mailserver
```

### Add More Users

```bash
/usr/local/bin/mailserver user add another@yourdomain.com --config /etc/mailserver/config.yaml
```

### Backup

```bash
# Backup database
cp /var/lib/mailserver/mail.db /backup/mail.db.$(date +%Y%m%d)

# Backup maildir
tar -czf /backup/maildir.$(date +%Y%m%d).tar.gz /var/lib/mailserver/maildir

# Backup DKIM keys
cp -r /etc/mailserver/dkim /backup/dkim.$(date +%Y%m%d)
```

### Update Server

```bash
cd /opt/mailserver
git pull
go build -o /usr/local/bin/mailserver ./cmd/mailserver
systemctl restart mailserver
```

---

## Troubleshooting

### Email Not Sending

```bash
# Check if SMTP is listening
ss -tlnp | grep 25

# Check firewall
ufw status

# Check logs
journalctl -u mailserver | grep -i error
```

### Certificate Issues

```bash
# Check certificate files
ls -la /var/lib/mailserver/acme/

# Force certificate renewal
systemctl restart mailserver
```

### Can't Receive Email

1. Verify MX record: `dig MX yourdomain.com`
2. Check port 25 is open: `nc -zv mail.yourdomain.com 25`
3. Check Cloudflare proxy is OFF for mail subdomain

### DKIM Failing

```bash
# Verify DKIM key format
cat /etc/mailserver/dkim/yourdomain.com.key

# Check DNS record
dig TXT mail._domainkey.yourdomain.com
```

---

## Security Checklist

- [ ] Firewall enabled with only required ports
- [ ] SSH key authentication (disable password login)
- [ ] Strong passwords for email accounts
- [ ] Reverse DNS (PTR) record set
- [ ] SPF record configured
- [ ] DKIM signing enabled
- [ ] DMARC policy configured
- [ ] TLS certificates working
- [ ] Regular backups scheduled

---

## Quick Reference

| Service | Port | Protocol |
|---------|------|----------|
| SMTP (receive) | 25 | TCP |
| Submission (send) | 587 | TCP |
| SMTPS | 465 | TCP |
| IMAP | 143 | TCP |
| IMAPS | 993 | TCP |
| CalDAV/CardDAV | 8443 | TCP |

| DNS Record | Name | Purpose |
|------------|------|---------|
| A | mail | Points to server IP |
| MX | @ | Mail exchange |
| TXT | @ | SPF policy |
| TXT | mail._domainkey | DKIM public key |
| TXT | _dmarc | DMARC policy |
| PTR | (IP) | Reverse DNS |
