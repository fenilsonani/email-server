# Video Script: Personal Email Server
## "I Built a Google Workspace Replacement"

**Tone:** Technical authority, confident, educational
**Length:** 8-10 minutes
**Style:** Direct-to-camera + full terminal/screen demos

---

## FULL SCRIPT

### HOOK (0:00 - 0:30)
```
[CAMERA - confident, direct]

"IMAP. SMTP. CalDAV. CardDAV.
DKIM. SPF. DMARC.
Sieve filtering. Message queues. Circuit breakers.
Multi-domain. Multi-user.
TLS everywhere.

I built a complete email server from scratch.
It replaces Google Workspace.
It replaces Microsoft 365.

And today I'm going to show you exactly how it works,
and how to deploy it yourself.

Let's get into it."

[TITLE CARD]
```

### THE PROBLEM (0:30 - 1:30)
```
[CAMERA]

"First, let's talk about why.

Google Workspace costs $6 to $18 per user, per month.
Microsoft 365 costs $6 to $22 per user, per month.

[GRAPHIC: Cost comparison]

For a team of 10 people, you're looking at
$720 to $2,640 per year. Every year. Forever.

And what do you actually get?

- Your emails processed through their systems
- Your calendar data on their servers
- Your contacts in their databases
- Vendor lock-in that makes migration painful

[CAMERA - lean in]

But here's the real cost nobody talks about:
You don't own your infrastructure.
You don't control your data.
And you're building your business on rented land.

I wanted something different.
So I built it."
```

### ARCHITECTURE OVERVIEW (1:30 - 2:30)
```
[CAMERA]

"Let me show you what we're working with.

[SCREEN: Architecture diagram or code structure]

This is a complete email server written in Go.
Single binary. Minimal dependencies.

Here's the stack:

IMAP server - RFC 3501 compliant with IDLE support
for real push notifications to your clients.

SMTP server - Three modes:
  Port 25 for receiving mail from other servers
  Port 587 for authenticated submission with STARTTLS
  Port 465 for implicit TLS

CalDAV server - RFC 4791. Full calendar sync.
Works with Apple Calendar, Thunderbird, any standards-compliant client.

CardDAV server - RFC 6352. Contact synchronization.

[SCREEN: Show the protocol files]

Email authentication: DKIM signing, SPF verification, DMARC policies.
Your emails will actually land in inboxes, not spam folders.

Sieve filtering - The industry standard for email rules.
Vacation responses, auto-filing, spam handling.

Message queue backed by Redis with retry logic
and circuit breaker patterns for reliability.

Admin dashboard for user management, domain management,
queue monitoring, and diagnostics.

[CAMERA]

All of this runs on a $5 VPS.
That's $60 per year. For unlimited users."
```

### LIVE SETUP DEMO (2:30 - 5:30)
```
[CAMERA]

"Alright, let's deploy this thing.
I'm going to do a complete setup from scratch
so you can see exactly what's involved."

[TERMINAL - Fresh VPS]

"First, let's run the preflight checks."

$ mailserver preflight

[SHOW OUTPUT]

"This verifies your system is ready.
- Checks if required ports are available: 25, 143, 465, 587, 993, 8443
- Verifies Redis connectivity
- Checks disk space
- Validates operating system compatibility

[CAMERA]

Everything green. Let's run the setup wizard."

[TERMINAL]

$ mailserver setup

[SHOW INTERACTIVE WIZARD]

"The wizard walks you through everything:

Step 1: Domain configuration
[TYPE: example.com]

Step 2: Database initialization
[SHOW: Database setup messages]

Step 3: DKIM key generation
[SHOW: Key being generated]

Step 4: Admin user creation
[TYPE: admin@example.com, password]

[CAMERA]

That's it. The server is configured.
Now let's generate our DNS records."

[TERMINAL]

$ mailserver dns generate example.com

[SHOW OUTPUT - MX, SPF, DKIM, DMARC records]

"These are the exact DNS records you need to add:

MX record - Points mail to your server
SPF record - Authorizes your IP to send mail
DKIM record - Your public key for email signing
DMARC record - Your authentication policy

[CAMERA]

Add these to your DNS provider.
Most changes propagate in 15 minutes to an hour.

Let's verify they're set up correctly:"

[TERMINAL]

$ mailserver dns check example.com

[SHOW: All checks passing]

"All green. Now let's start the server."

[TERMINAL]

$ mailserver serve

[SHOW: Server starting, all services binding]

"IMAP listening on 143 and 993.
SMTP listening on 25, 587, and 465.
CalDAV and CardDAV on 8443.
Admin panel on 8080.

The server is live."
```

### ADMIN DASHBOARD TOUR (5:30 - 6:30)
```
[CAMERA]

"Let me show you the admin interface."

[BROWSER: Admin Dashboard]

"This is your control center.

[SCREEN: Dashboard home]
Server statistics. User count. Domain count.
Message queue status. Recent activity.

[SCREEN: User management]
Create users. Manage passwords. Set display names.
No per-seat licensing. Add as many as you want.

[SCREEN: Domain management]
Multi-domain support. Each domain gets its own DKIM keys.
Run multiple businesses on one server.

[SCREEN: Queue management]
See pending messages. Retry failed deliveries.
Delete stuck messages. Full visibility into your mail flow.

[SCREEN: Sieve scripts]
Email filtering rules. Per-user or global.
Vacation responses. Auto-filing. The works.

[SCREEN: Logs]
Authentication attempts. Delivery logs.
Full audit trail of everything.

[CAMERA]

This is the control you give up with hosted email.
Now you have it back."
```

### EMAIL CLIENT SETUP (6:30 - 7:30)
```
[CAMERA]

"Let's connect an actual email client.
I'll use Apple Mail, but this works with any IMAP client."

[SCREEN: Apple Mail setup]

"Add account. Choose 'Other Mail Account'.

[TYPE: Email and password]

IMAP server: mail.example.com
Port: 993, SSL/TLS

SMTP server: mail.example.com
Port: 587, STARTTLS

[SHOW: Account connecting]

And we're in.

[SCREEN: Show inbox, send test email]

Inbox synced. Let me send a test email.

[SEND EMAIL, SHOW IT ARRIVING]

Delivered. Let's check if DKIM passed."

[SCREEN: Show email headers]

"DKIM: pass
SPF: pass
DMARC: pass

This email is fully authenticated.
It's not going to spam."

[CAMERA]

"Now let's set up calendar sync."

[SCREEN: Apple Calendar]

"Add CalDAV account.
Server: https://mail.example.com:8443

[SHOW: Calendars appearing]

Calendar synced. Same process for contacts with CardDAV.

Your Apple devices, your Android devices,
any CalDAV/CardDAV client - all synced to YOUR server."
```

### ADVANCED FEATURES (7:30 - 8:30)
```
[CAMERA]

"Let me show you some power user features."

[TERMINAL]

"Health diagnostics when something goes wrong:"

$ mailserver doctor

[SHOW OUTPUT: Health checks]

"Checks running services, database connectivity,
port bindings, everything.

[CAMERA]

Email filtering with Sieve:"

[SCREEN: Admin panel - Sieve scripts]

"This is the industry standard for email rules.
Let me create a vacation responder."

[SHOW: Creating Sieve script]

require ["vacation"];
vacation
  :days 1
  :subject "Out of Office"
  "I'm currently away. I'll respond when I return.";

[SAVE AND ACTIVATE]

"Done. Anyone who emails me gets an auto-reply.

[CAMERA]

Docker deployment for those who prefer containers:"

[TERMINAL: Show docker-compose.yml]

$ docker-compose up -d

"Single command. Everything containerized.
Redis, mail server, all configured.

[CAMERA]

The message queue handles delivery reliability:"

[SCREEN: Admin panel - Queue]

"Messages retry automatically with exponential backoff.
15 retry attempts over 7 days.
Circuit breaker patterns prevent cascading failures.

This is production-grade infrastructure."
```

### COMPARISON & CLOSING (8:30 - 9:30)
```
[CAMERA]

"Let's do a final comparison.

[GRAPHIC: Feature comparison table]

Google Workspace:
- $6-18/user/month
- Your data on Google servers
- Limited admin control
- Ecosystem lock-in

Microsoft 365:
- $6-22/user/month
- Your data on Microsoft servers
- Limited admin control
- Ecosystem lock-in

This email server:
- $5/month total (VPS cost)
- Your data on YOUR server
- Complete infrastructure control
- Open source, no lock-in

[CAMERA - direct]

Is running your own email server more work?
Yes.

Is it worth it?
If you care about data ownership...
If you're tired of subscription costs...
If you want actual control over your infrastructure...

Then yes. Absolutely.

[SCREEN: GitHub page]

The code is open source.
Everything I showed you today is in the repo.
Star it. Fork it. Deploy it.

Stop renting your email.
Start owning it.

[CAMERA]

Thanks for watching.
Link in the description."

[END CARD]
```

---

## PRODUCTION CHECKLIST

### Terminal Recordings Needed
1. `mailserver preflight` - Full output
2. `mailserver setup` - Complete wizard walkthrough
3. `mailserver dns generate <domain>` - DNS record output
4. `mailserver dns check <domain>` - Verification
5. `mailserver serve` - Server startup
6. `mailserver doctor` - Health diagnostics
7. `docker-compose up -d` - Container deployment

### Screen Recordings Needed
1. Admin dashboard - Full tour (5+ minutes of footage)
2. Apple Mail setup - Account configuration
3. Email send/receive test
4. Email headers showing DKIM/SPF/DMARC pass
5. Apple Calendar - CalDAV setup
6. Contacts - CardDAV setup
7. Sieve script creation in admin panel

### Graphics Needed
1. Cost comparison table (Google vs Microsoft vs Self-hosted)
2. Feature comparison table
3. Architecture diagram (optional but impressive)
4. Title card with project name
5. End card with GitHub link

### B-Roll Suggestions
- Server hardware close-ups
- Data center footage (stock)
- Code scrolling on screen
- Terminal typing fast-forward

### Music
- Tech/electronic, builds energy
- Drops on key moments (server starting, "Let's get into it")
- Softer during explanation sections
- Builds to climax at closing

### Thumbnail Options
1. Terminal with protocols listed + "I BUILT THIS"
2. Split: Google/Microsoft logos vs your server
3. Your face + terminal + "Replaced Google Workspace"

---

## SCRIPT WORD COUNT
~1,500 words = ~9 minutes at natural pace

## KEY TIMESTAMPS FOR CHAPTERS
- 0:00 - Introduction
- 0:30 - The Problem with Cloud Email
- 1:30 - Architecture Overview
- 2:30 - Live Setup Demo
- 5:30 - Admin Dashboard Tour
- 6:30 - Email Client Setup
- 7:30 - Advanced Features
- 8:30 - Comparison & Final Thoughts
