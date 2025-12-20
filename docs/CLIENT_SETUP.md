# Email Client Setup Guide

This guide covers configuring various email clients to work with your personal email server.

## Apple Mail (macOS)

### Adding Email Account

1. Open **System Preferences** → **Internet Accounts**
2. Click **Add Account** → **Add Other Account** → **Mail Account**
3. Enter your details:
   - Name: Your Name
   - Email: user@yourdomain.com
   - Password: your password
4. Click **Sign In**
5. If prompted for manual configuration:

**Incoming Mail Server (IMAP):**
| Setting | Value |
|---------|-------|
| Account Type | IMAP |
| Mail Server | mail.yourdomain.com |
| Username | user@yourdomain.com |
| Password | your password |
| Port | 993 |
| SSL | Yes |

**Outgoing Mail Server (SMTP):**
| Setting | Value |
|---------|-------|
| SMTP Server | mail.yourdomain.com |
| Username | user@yourdomain.com |
| Password | your password |
| Port | 587 |
| SSL | STARTTLS |
| Authentication | Password |

### Adding Calendar (CalDAV)

1. Open **System Preferences** → **Internet Accounts**
2. Click **Add Account** → **Add Other Account** → **CalDAV Account**
3. Enter:
   - Account Type: Manual
   - Username: user@yourdomain.com
   - Password: your password
   - Server Address: mail.yourdomain.com:8443/caldav/

### Adding Contacts (CardDAV)

1. Open **System Preferences** → **Internet Accounts**
2. Click **Add Account** → **Add Other Account** → **CardDAV Account**
3. Enter:
   - Account Type: Manual
   - Username: user@yourdomain.com
   - Password: your password
   - Server Address: mail.yourdomain.com:8443/carddav/

---

## Apple Mail (iOS/iPadOS)

### Adding Email Account

1. Go to **Settings** → **Mail** → **Accounts** → **Add Account**
2. Tap **Other** → **Add Mail Account**
3. Enter:
   - Name: Your Name
   - Email: user@yourdomain.com
   - Password: your password
4. Tap **Next** and select **IMAP**
5. Configure servers:

**Incoming Mail Server:**
| Setting | Value |
|---------|-------|
| Host Name | mail.yourdomain.com |
| Username | user@yourdomain.com |
| Password | your password |

**Outgoing Mail Server:**
| Setting | Value |
|---------|-------|
| Host Name | mail.yourdomain.com |
| Username | user@yourdomain.com |
| Password | your password |

6. Tap **Next** and **Save**

### Adding Calendar

1. Go to **Settings** → **Calendar** → **Accounts** → **Add Account**
2. Tap **Other** → **Add CalDAV Account**
3. Enter:
   - Server: mail.yourdomain.com:8443
   - Username: user@yourdomain.com
   - Password: your password
   - Description: My Calendar

### Adding Contacts

1. Go to **Settings** → **Contacts** → **Accounts** → **Add Account**
2. Tap **Other** → **Add CardDAV Account**
3. Enter:
   - Server: mail.yourdomain.com:8443
   - Username: user@yourdomain.com
   - Password: your password
   - Description: My Contacts

---

## Thunderbird

### Adding Email Account

1. Open Thunderbird
2. Click **≡** menu → **Account Settings** → **Account Actions** → **Add Mail Account**
3. Enter:
   - Your Name: Your Name
   - Email Address: user@yourdomain.com
   - Password: your password
4. Click **Configure manually**

**Incoming Server:**
| Setting | Value |
|---------|-------|
| Protocol | IMAP |
| Server | mail.yourdomain.com |
| Port | 993 |
| SSL | SSL/TLS |
| Authentication | Normal password |

**Outgoing Server:**
| Setting | Value |
|---------|-------|
| Server | mail.yourdomain.com |
| Port | 587 |
| SSL | STARTTLS |
| Authentication | Normal password |

5. Click **Done**

### Adding Calendar (with TbSync add-on)

1. Install **TbSync** add-on from Add-ons Manager
2. Install **Provider for CalDAV & CardDAV** add-on
3. Open **TbSync** from Tools menu
4. Click **Add account** → **CalDAV & CardDAV**
5. Select **Manual configuration**
6. Enter:
   - Account name: My Calendar
   - CalDAV server: https://mail.yourdomain.com:8443/caldav/
   - Username: user@yourdomain.com
   - Password: your password

### Adding Contacts (with TbSync)

1. Use the same TbSync account as above
2. Enter CardDAV server: https://mail.yourdomain.com:8443/carddav/
3. Sync will discover address books automatically

---

## Microsoft Outlook

### Adding Email Account

1. Go to **File** → **Add Account**
2. Enter your email: user@yourdomain.com
3. Click **Advanced options** → **Let me set up my account manually**
4. Select **IMAP**
5. Enter settings:

**Incoming Mail:**
| Setting | Value |
|---------|-------|
| Server | mail.yourdomain.com |
| Port | 993 |
| Encryption | SSL/TLS |

**Outgoing Mail:**
| Setting | Value |
|---------|-------|
| Server | mail.yourdomain.com |
| Port | 587 |
| Encryption | STARTTLS |

6. Enter your password and click **Connect**

---

## Android (Gmail App or other)

### Gmail App

1. Open Gmail app
2. Tap profile icon → **Add another account** → **Other**
3. Enter email: user@yourdomain.com
4. Select **Personal (IMAP)**
5. Enter password
6. Configure incoming server:
   - Server: mail.yourdomain.com
   - Port: 993
   - Security: SSL/TLS
7. Configure outgoing server:
   - Server: mail.yourdomain.com
   - Port: 587
   - Security: STARTTLS

### K-9 Mail / FairEmail

These apps auto-discover settings or provide easy manual configuration:

1. Add new account
2. Enter email and password
3. If auto-discovery fails, enter:
   - IMAP: mail.yourdomain.com:993 (SSL)
   - SMTP: mail.yourdomain.com:587 (STARTTLS)

### DAVx⁵ for Calendar/Contacts

1. Install DAVx⁵ from F-Droid or Play Store
2. Add account with:
   - Base URL: https://mail.yourdomain.com:8443
   - Username: user@yourdomain.com
   - Password: your password
3. DAVx⁵ will discover calendars and address books

---

## Command Line (mutt/neomutt)

### ~/.muttrc

```
# Account Settings
set realname = "Your Name"
set from = "user@yourdomain.com"

# IMAP Settings
set folder = "imaps://mail.yourdomain.com:993"
set imap_user = "user@yourdomain.com"
set imap_pass = "your_password"
set spoolfile = "+INBOX"
set postponed = "+Drafts"
set record = "+Sent"
set trash = "+Trash"

# SMTP Settings
set smtp_url = "smtps://user@yourdomain.com@mail.yourdomain.com:587"
set smtp_pass = "your_password"
set ssl_starttls = yes
set ssl_force_tls = yes

# SSL/TLS
set ssl_verify_host = yes
set ssl_verify_dates = yes
```

---

## Troubleshooting

### Connection Refused

1. Check server is running:
   ```bash
   systemctl status mailserver
   ```

2. Check firewall:
   ```bash
   sudo ufw status
   ```

3. Test connection:
   ```bash
   openssl s_client -connect mail.yourdomain.com:993
   ```

### Certificate Errors

1. For self-signed certs, you may need to accept/trust the certificate
2. Ensure certificate includes mail.yourdomain.com in SAN
3. Check certificate validity:
   ```bash
   openssl s_client -connect mail.yourdomain.com:993 -showcerts
   ```

### Authentication Failed

1. Verify username is full email (user@domain.com)
2. Check password is correct
3. Verify user exists:
   ```bash
   ./mailserver user list --domain yourdomain.com
   ```

### Push Notifications Not Working

1. Ensure IMAP IDLE is enabled in client
2. Check client settings for "Push" or "Real-time" options
3. Some clients require account to be "primary" for push

### Calendar/Contacts Not Syncing

1. Verify DAV server is running on port 8443
2. Check URL format (include trailing slash for some clients)
3. Test access:
   ```bash
   curl -u user@domain.com:password https://mail.yourdomain.com:8443/caldav/
   ```

### Slow Performance

1. Check server resources (CPU, memory, disk)
2. Verify network latency
3. Consider enabling local caching in client
4. Check for large mailboxes that need archiving
