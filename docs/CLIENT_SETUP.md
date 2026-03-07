# Client Setup

All clients use the same server settings:

| Protocol | Server | Port | Security |
|----------|--------|------|----------|
| IMAP | mail.yourdomain.com | 993 | SSL/TLS |
| SMTP | mail.yourdomain.com | 587 | STARTTLS |
| CalDAV | mail.yourdomain.com:8443 | — | HTTPS |
| CardDAV | mail.yourdomain.com:8443 | — | HTTPS |

Username is always the full email address (e.g., `user@yourdomain.com`).

If you've set up autodiscover DNS records, most clients will configure themselves automatically.

---

## Apple Mail (macOS)

![Apple Mail IMAP Setup](images/apple-mail-setup.png)

1. **System Settings** → **Internet Accounts** → **Add Account** → **Other** → **Mail Account**
2. Enter name, email, password
3. If prompted for manual config, use the settings table above

**CalDAV:** Same path → **CalDAV Account** → Manual → Server: `mail.yourdomain.com:8443/caldav/`

**CardDAV:** Same path → **CardDAV Account** → Manual → Server: `mail.yourdomain.com:8443/carddav/`

## Apple Mail (iOS)

![iOS Mail Setup](images/ios-mail-setup.png)

1. **Settings** → **Mail** → **Accounts** → **Add Account** → **Other** → **Add Mail Account**
2. Enter email and password, select IMAP
3. Host name for both incoming/outgoing: `mail.yourdomain.com`

**CalDAV:** **Settings** → **Calendar** → **Accounts** → **Other** → **CalDAV** → Server: `mail.yourdomain.com:8443`

**CardDAV:** **Settings** → **Contacts** → **Accounts** → **Other** → **CardDAV** → Server: `mail.yourdomain.com:8443`

## Thunderbird

![Thunderbird Setup](images/thunderbird-setup.png)

1. **Account Settings** → **Account Actions** → **Add Mail Account**
2. Enter name, email, password → **Configure manually**
3. Incoming: IMAP, `mail.yourdomain.com`, 993, SSL/TLS
4. Outgoing: SMTP, `mail.yourdomain.com`, 587, STARTTLS

**CalDAV/CardDAV:** Install the **TbSync** and **Provider for CalDAV & CardDAV** add-ons. Add account with server `https://mail.yourdomain.com:8443/caldav/` (or `/carddav/`).

## Outlook

1. **File** → **Add Account** → enter email → **Advanced options** → **Let me set up my account manually** → IMAP
2. Incoming: `mail.yourdomain.com`, 993, SSL/TLS
3. Outgoing: `mail.yourdomain.com`, 587, STARTTLS

## Android

**Gmail app:** Add account → Other → IMAP → use settings table above.

**K-9 Mail / FairEmail:** Enter email and password. Auto-discovery usually works; if not, enter servers manually.

**CalDAV/CardDAV:** Install [DAVx5](https://www.davx5.com/) → Add account → Base URL: `https://mail.yourdomain.com:8443` → enter credentials.

## mutt / neomutt

```
set realname = "Your Name"
set from = "user@yourdomain.com"
set folder = "imaps://mail.yourdomain.com:993"
set imap_user = "user@yourdomain.com"
set imap_pass = "your_password"
set spoolfile = "+INBOX"
set postponed = "+Drafts"
set record = "+Sent"
set trash = "+Trash"
set smtp_url = "smtps://user@yourdomain.com@mail.yourdomain.com:587"
set smtp_pass = "your_password"
set ssl_starttls = yes
set ssl_force_tls = yes
```

---

## Troubleshooting

**Connection refused:** Check `systemctl status mailserver`, firewall rules, and test with `openssl s_client -connect mail.yourdomain.com:993`.

**Auth failed:** Username must be the full email address. Verify the user exists with `mailserver user list`.

**Push notifications not working:** Ensure IMAP IDLE is enabled in your client settings.

**CalDAV/CardDAV not syncing:** Verify port 8443 is open. Some clients need a trailing slash on the URL. Test with `curl -u user@domain.com:password https://mail.yourdomain.com:8443/caldav/`.
