# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 1.x.x   | :white_check_mark: |
| < 1.0   | :x:                |

## Reporting a Vulnerability

We take security seriously. If you discover a security vulnerability, please report it responsibly.

### How to Report

**DO NOT** open a public GitHub issue for security vulnerabilities.

Instead, please email: **security@fenilsonani.com**

Include:
- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Any suggested fixes (optional)

### What to Expect

1. **Acknowledgment**: We'll respond within 48 hours
2. **Assessment**: We'll investigate and assess severity within 7 days
3. **Fix Timeline**: Critical issues will be patched ASAP, others within 30 days
4. **Disclosure**: We'll coordinate disclosure timing with you

### Security Best Practices

When deploying this email server, please follow these guidelines:

#### Network Security

- **Firewall**: Only expose necessary ports (25, 587, 465, 993, 8443)
- **Admin Panel**: Never expose port 8080 directly; use a reverse proxy with HTTPS
- **Redis**: Bind to localhost or use authentication if exposed
- **VPS**: Use a reputable provider with DDoS protection

#### TLS Configuration

- Always enable `auto_tls: true` in production
- Use `require_tls: true` for submission ports
- Regularly update certificates

#### Authentication

- Use strong passwords (minimum 12 characters recommended)
- Consider implementing fail2ban for brute force protection
- Review audit logs regularly

#### Updates

- Keep the server updated
- Subscribe to release notifications
- Test updates in staging before production

#### Backups

- Regular backups of `/var/lib/mailserver/`
- Test backup restoration periodically
- Store backups securely off-server

#### Monitoring

- Enable Prometheus metrics
- Set up alerts for:
  - High queue depth
  - Authentication failures
  - Delivery errors
  - Resource usage

## Known Security Considerations

### Email-Specific

1. **Greylisting**: Enabled by default for spam prevention
2. **SPF/DKIM/DMARC**: Should be configured for all domains
3. **Rate Limiting**: Implemented on admin panel
4. **TLS Fallback**: For compatibility, non-TLS delivery is allowed by default when `require_tls: false`

### Data Storage

1. **Passwords**: Hashed with Argon2id (OWASP recommended)
2. **Emails**: Stored in Maildir format on filesystem (not encrypted at rest)
3. **Metadata**: SQLite database with standard permissions (not encrypted)
4. **Logs**: May contain email addresses and IPs; rotate and secure accordingly

> **At-rest encryption**: This server does not encrypt data at rest. For sensitive deployments, use full-disk encryption (LUKS on Linux, FileVault on macOS) on your server.

## Security Features

- Argon2id password hashing
- TLS 1.2+ for all encrypted connections
- DKIM signing for outbound mail
- SPF and DMARC verification for inbound
- Rate limiting on authentication
- Audit logging for administrative actions
- Circuit breakers for delivery

## Acknowledgments

We appreciate security researchers who responsibly disclose vulnerabilities. Contributors will be acknowledged (with permission) in release notes.
