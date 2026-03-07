# Security Policy

## Reporting a Vulnerability

**Do not** open a public GitHub issue for security vulnerabilities.

Email **security@fenilsonani.com** with:
- Description of the vulnerability
- Steps to reproduce
- Potential impact

We'll acknowledge within 48 hours, assess within 7 days, and patch critical issues ASAP.

## Deployment Checklist

- Firewall: only expose ports 25, 587, 465, 993, 8443
- Admin panel (8080): bind to localhost, access via reverse proxy with HTTPS
- Redis: bind to localhost or use authentication
- Enable `auto_tls: true` in production
- Configure SPF, DKIM, and DMARC DNS records for all domains
- Set up fail2ban for brute force protection
- Back up `/var/lib/mailserver/` regularly and test restores

## Data Storage

| Data | Storage | Encryption |
|------|---------|------------|
| Passwords | SQLite | Argon2id hashed |
| Emails | Maildir on filesystem | Not encrypted at rest |
| Metadata | SQLite | Not encrypted at rest |
| Logs | stdout/journald | May contain email addresses and IPs |

For at-rest encryption, use full-disk encryption (LUKS, FileVault) on your server.

## Security Features

- Argon2id password hashing
- TLS 1.2+ on all encrypted connections
- DKIM signing (outbound), SPF/DMARC verification (inbound)
- Rate limiting on authentication
- Greylisting (enabled by default)
- Audit logging for admin actions
- Per-domain circuit breakers on delivery
- TLS fallback is allowed by default when `require_tls: false`
