# Complete Video Script
## "I Built a Google Workspace Replacement"

**Total Runtime:** 8-10 minutes
**Speaking Pace:** Slow and clear (non-native optimized)
**Format:** `[DIRECTION]` = what to show, `(pause)` = take a breath, `*emphasis*` = stress this word

---

# SECTION 1: HOOK
**Time: 0:00 - 0:35**

---

```
[CAMERA - Look directly at camera. Confident posture.]

IMAP. (pause)
SMTP. (pause)
CalDAV. CardDAV. (pause)
DKIM. SPF. DMARC. (pause)

Sieve filtering.
Message queues.
Circuit breakers. (pause)

Multi-domain.
Multi-user.
TLS everywhere. (pause)

[Lean slightly forward]

I built a *complete* email server. (pause)
From scratch. (pause)
In Go. (pause)

It replaces Google Workspace.
It replaces Microsoft 365. (pause)

And today... (pause)
I will show you *exactly* how it works. (pause)
And how to run it yourself.

[Small smile]

Let's get into it.

[TITLE CARD: "I Built a Google Workspace Replacement"]
```

---

# SECTION 2: THE PROBLEM
**Time: 0:35 - 1:45**

---

```
[CAMERA - Conversational tone]

First. (pause)
Let me explain *why* I built this. (pause)

[GRAPHIC: Show Google Workspace pricing page]

Google Workspace costs six to eighteen dollars. (pause)
Per user. (pause)
Per *month*. (pause)

[GRAPHIC: Show Microsoft 365 pricing page]

Microsoft 365 costs six to twenty-two dollars. (pause)
Per user. (pause)
Per month. (pause)

[GRAPHIC: Simple calculation on screen]
"10 users × $12/month × 12 months = $1,440/year"

For a small team of ten people... (pause)
You pay seven hundred to two thousand dollars. (pause)
Every year. (pause)
*Forever*. (pause)

[CAMERA - More serious tone]

But the money is not the real problem. (pause)

The real problem is this: (pause)

[GRAPHIC: Bullet points appearing one by one]

Your emails... are on *their* servers.
Your calendar... is in *their* database.
Your contacts... they control them. (pause)

You are building your business... (pause)
on rented land. (pause)

[CAMERA - Direct eye contact]

And if you want to leave? (pause)
Good luck. (pause)
That is called vendor lock-in. (pause)

[Beat - let it sink in]

I wanted something different. (pause)
So I built it.
```

---

# SECTION 3: WHAT I BUILT
**Time: 1:45 - 3:00**

---

```
[CAMERA]

Let me show you what we are working with. (pause)

[SCREEN: Show project folder structure or README]

This is a complete email server. (pause)
Written in Go. (pause)
Single binary. (pause)
Minimal dependencies. (pause)

[SCREEN: Terminal - show the binary file]

One file. (pause)
That is the entire server. (pause)

[CAMERA]

Here is what it includes: (pause)

[GRAPHIC: Feature list appearing one by one]

Number one. (pause)
*IMAP server*. (pause)

[Say slowly: "eye-map"]

RFC 3501 compliant. (pause)
With IDLE support for real push notifications. (pause)
Your phone gets new emails *instantly*. (pause)

Number two. (pause)
*SMTP server*. (pause)

[Say slowly: "S-M-T-P"]

Three modes: (pause)
Port 25 for receiving mail from other servers. (pause)
Port 587 for sending mail with STARTTLS. (pause)
Port 465 for sending mail with full TLS. (pause)

Number three. (pause)
*CalDAV server*. (pause)

[Say: "cal-dav"]

RFC 4791. (pause)
Full calendar synchronization. (pause)
Works with Apple Calendar. Thunderbird. Any standard client. (pause)

Number four. (pause)
*CardDAV server*. (pause)

[Say: "card-dav"]

RFC 6352. (pause)
Contact synchronization. (pause)
Your address book syncs across all devices. (pause)

[CAMERA - Excited energy]

Number five. (pause)
Email authentication. (pause)

DKIM signing. (pause)
SPF verification. (pause)
DMARC policies. (pause)

[Say each slowly: "D-KIM", "S-P-F", "D-MARC"]

Your emails will land in inboxes. (pause)
Not spam folders. (pause)

Number six. (pause)
*Sieve filtering*. (pause)

[Say: "siv" like the kitchen tool]

Server-side email rules. (pause)
Vacation auto-replies. (pause)
Automatic folder organization. (pause)

Number seven. (pause)
*Message queue* with Redis. (pause)
Automatic retry logic. (pause)
If delivery fails... it tries again. (pause)
For up to seven days. (pause)

Number eight. (pause)
*Admin dashboard*. (pause)
User management. Domain management. (pause)
Email queue. Logs. Everything. (pause)

[CAMERA - Pause for effect]

All of this... (pause)
runs on a five dollar VPS. (pause)

[GRAPHIC: "$60/year vs $1,440/year"]

That is sixty dollars per year. (pause)
For *unlimited* users.
```

---

# SECTION 4: LIVE SETUP DEMO
**Time: 3:00 - 6:00**

---

```
[CAMERA]

Okay. (pause)
Let me deploy this. (pause)
Live. (pause)
Right now. (pause)

I have a fresh VPS. (pause)
Nothing installed except the basics. (pause)

[TERMINAL: Fresh Linux prompt]

First... we run the preflight checks. (pause)

[TYPE: mailserver preflight]

[TERMINAL OUTPUT showing checks]

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
            PREFLIGHT CHECK
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

[CAMERA - voiceover while output appears]

This checks if the system is ready. (pause)

It verifies: (pause)
Port 25 is available. (pause)
Port 143 is available. (pause)
Port 587. Port 465. Port 993. (pause)
All the ports we need. (pause)

It checks Redis connection. (pause)
Disk space. (pause)
Memory. (pause)
Operating system. (pause)

[TERMINAL: All checks pass]

✓ All checks passed.

[CAMERA]

Everything is green. (pause)
Now we run the setup wizard. (pause)

[TERMINAL]

[TYPE: mailserver setup]

[TERMINAL OUTPUT]

MAIL SERVER SETUP WIZARD
Running preflight checks...

[1/8] Creating system user...
[2/8] Creating directories...
[3/8] Generating configuration...

[CAMERA - voiceover]

The wizard does everything. (pause)

It creates a system user for security. (pause)
It creates all the directories. (pause)
It generates the configuration file. (pause)

[TERMINAL]

[4/8] Generating DKIM keys...

[CAMERA - voiceover]

It generates your DKIM keys. (pause)
2048-bit RSA. (pause)
Industry standard. (pause)

[TERMINAL]

[5/8] Initializing database...
[6/8] Creating admin user...

[Interactive prompt appears]
Enter admin email: admin@example.com
Enter admin password: ********

[CAMERA - voiceover]

You create your admin account. (pause)

[TERMINAL]

[7/8] Installing systemd service...
[8/8] Starting service...

✓ SETUP COMPLETE!

Your mail server is now running!
Admin Panel: http://127.0.0.1:8080/admin

[CAMERA]

Done. (pause)
The server is running. (pause)

[Beat]

But we need DNS records. (pause)
Let me generate them. (pause)

[TERMINAL]

[TYPE: mailserver dns generate example.com]

[OUTPUT]

DNS Records for example.com
================================================

Type: MX
Host: @
Value: mail.example.com
Priority: 10

Type: TXT
Host: @
Value: v=spf1 mx -all

Type: TXT
Host: mail._domainkey
Value: v=DKIM1; k=rsa; p=MIGfMA0GCSq...

Type: TXT
Host: _dmarc
Value: v=DMARC1; p=quarantine; ...

[CAMERA - voiceover]

These are your DNS records. (pause)

MX record... tells the internet where to send mail. (pause)
SPF record... says your server is allowed to send. (pause)
DKIM record... your public signing key. (pause)
DMARC record... your authentication policy. (pause)

[CAMERA]

Copy these. (pause)
Add them to your DNS provider. (pause)
Cloudflare. Namecheap. GoDaddy. Whatever you use. (pause)

Wait fifteen minutes for propagation. (pause)

Then verify: (pause)

[TERMINAL]

[TYPE: mailserver dns check example.com]

[OUTPUT]

[✓] MX       PASS
[✓] SPF      PASS
[✓] DKIM     PASS
[✓] DMARC    PASS

[CAMERA - Smile]

All green. (pause)

Now let's start the server. (pause)

[TERMINAL]

[TYPE: mailserver serve]

[OUTPUT]

Mail server starting on mail.example.com
  SMTP:  25 (MX), 587 (submission), 465 (SMTPS)
  IMAP:  143, 993 (TLS)
  Admin: http://127.0.0.1:8080

Server is running. Press Ctrl+C to stop.

[CAMERA]

IMAP is listening on 143 and 993. (pause)
SMTP is listening on 25, 587, and 465. (pause)
Admin panel on 8080. (pause)

The server is *live*. (pause)

From zero to running email server... (pause)
in about five minutes.
```

---

# SECTION 5: ADMIN DASHBOARD
**Time: 6:00 - 7:00**

---

```
[CAMERA]

Let me show you the admin interface. (pause)

[BROWSER: Navigate to admin panel]

[SCREEN: Login page]

We log in with our admin account. (pause)

[SCREEN: Dashboard home]

This is your control center. (pause)

[Point to different sections as you describe]

Total users. (pause)
Total domains. (pause)
Message queue status. (pause)
Recent activity. (pause)

[SCREEN: Click on Users]

User management. (pause)

Create users. (pause)
Change passwords. (pause)
Enable or disable accounts. (pause)

No per-seat licensing. (pause)
Add as many users as you want. (pause)

[SCREEN: Click on Domains]

Domain management. (pause)

You can run multiple domains on one server. (pause)
Each domain gets its own DKIM keys. (pause)

[SCREEN: Click on Queue]

Email queue. (pause)

See pending messages. (pause)
See failed deliveries. (pause)
Retry manually if needed. (pause)
Delete stuck messages. (pause)

Full visibility into your mail flow. (pause)

[SCREEN: Click on Logs]

Authentication logs. (pause)
Who logged in. When. From where. (pause)

Delivery logs. (pause)
Every email. Success or failure. (pause)

[CAMERA]

This is the control you give up... (pause)
when you use hosted email. (pause)

Now you have it back.
```

---

# SECTION 6: CONNECTING EMAIL CLIENT
**Time: 7:00 - 8:00**

---

```
[CAMERA]

Let's connect a real email client. (pause)

I will use Apple Mail. (pause)
But this works with any IMAP client. (pause)
Thunderbird. Outlook. Whatever. (pause)

[SCREEN: Apple Mail - Add Account]

Add account. (pause)
Choose "Other Mail Account". (pause)

[SCREEN: Enter credentials]

Email: user@example.com
Password: ********

[SCREEN: Server settings]

IMAP server: mail.example.com (pause)
Port: 993 with SSL/TLS (pause)

SMTP server: mail.example.com (pause)
Port: 587 with STARTTLS (pause)

[SCREEN: Account connects, inbox appears]

And we are in. (pause)

[CAMERA - voiceover]

Inbox is synced. (pause)

Let me send a test email. (pause)

[SCREEN: Compose and send email to external address]

[SCREEN: Show email arriving]

Delivered. (pause)

Let me check the headers. (pause)

[SCREEN: Show email headers]

[Highlight each line]

DKIM: pass (pause)
SPF: pass (pause)
DMARC: pass (pause)

[CAMERA]

Fully authenticated. (pause)
This email is not going to spam. (pause)

[Beat]

Now... calendar sync. (pause)

[SCREEN: Apple Calendar - Add CalDAV account]

Add CalDAV account. (pause)
Server: mail.example.com (pause)
Port: 8443 (pause)

[SCREEN: Calendars appear]

Calendar is synced. (pause)

Same process for contacts. (pause)
CardDAV. Same server. Same port. (pause)

[SCREEN: Contacts syncing]

[CAMERA]

Your Apple devices. (pause)
Your Android devices. (pause)
Any CalDAV or CardDAV client. (pause)

All synced. (pause)
To *your* server.
```

---

# SECTION 7: ADVANCED FEATURES
**Time: 8:00 - 9:00**

---

```
[CAMERA]

Let me show you some power features. (pause)

[TERMINAL]

First... health diagnostics. (pause)

[TYPE: mailserver doctor]

[OUTPUT]

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
            HEALTH CHECK
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

✓ Mail Server Running
✓ Health Endpoint
✓ Database
✓ Redis
✓ TLS Certificates
✓ DKIM Keys
✓ DNS Records
✓ Disk Space
✓ Maildir Permissions

✓ Mail server is healthy!

[CAMERA - voiceover]

Nine health checks. (pause)
Services. Database. Redis. TLS. DNS. (pause)
If something is wrong... this tells you. (pause)

[CAMERA]

Second... email filtering with Sieve. (pause)

[SCREEN: Admin panel - Sieve scripts]

Sieve is the industry standard for email rules. (pause)

Let me create a vacation responder. (pause)

[TYPE in editor]

require ["vacation"];
vacation
  :days 1
  :subject "Out of Office"
  "I am currently away. I will respond when I return.";

[Save and activate]

Done. (pause)
Anyone who emails me... gets an automatic reply. (pause)

[CAMERA]

Third... Docker deployment. (pause)

[TERMINAL: Show docker-compose.yml]

If you prefer containers... (pause)

[TYPE: docker-compose up -d]

[OUTPUT: Containers starting]

One command. (pause)
Redis and mail server. (pause)
All containerized. (pause)

[CAMERA]

Fourth... the message queue. (pause)

[SCREEN: Admin panel - Queue]

Messages retry automatically. (pause)
Exponential backoff. (pause)
Fifteen retry attempts over seven days. (pause)

Circuit breaker patterns prevent cascading failures. (pause)

[Look at camera directly]

This is production-grade infrastructure. (pause)
Not a hobby project.
```

---

# SECTION 8: COMPARISON & CLOSING
**Time: 9:00 - 10:00**

---

```
[CAMERA]

Let me give you the final comparison. (pause)

[GRAPHIC: Three-column comparison table]

GOOGLE WORKSPACE:
- Six to eighteen dollars per user per month
- Your data on Google servers
- Limited admin control
- Ecosystem lock-in

MICROSOFT 365:
- Six to twenty-two dollars per user per month
- Your data on Microsoft servers
- Limited admin control
- Ecosystem lock-in

THIS EMAIL SERVER:
- Five dollars per month total (VPS cost)
- Your data on YOUR server
- Complete infrastructure control
- Open source. No lock-in.

[CAMERA - Direct, honest tone]

Is running your own email server more work? (pause)

Yes. (pause)

It is. (pause)

[Beat]

But is it worth it? (pause)

If you care about data ownership... (pause)
If you are tired of subscription costs... (pause)
If you want real control over your infrastructure... (pause)

Then yes. (pause)
*Absolutely*. (pause)

[SCREEN: GitHub repository page]

The code is open source. (pause)

Everything I showed you today... (pause)
is in this repository. (pause)

[CAMERA - Closing energy]

Star it. (pause)
Fork it. (pause)
Deploy it. (pause)

[Beat - Look directly at camera]

Stop renting your email. (pause)

Start *owning* it. (pause)

[Small smile]

Thanks for watching. (pause)
Link is in the description.

[END CARD: GitHub link, social handles]
```

---

# PRODUCTION NOTES

## Pronunciation Guide

| Term | Say it like |
|------|-------------|
| IMAP | "EYE-map" |
| SMTP | "S-M-T-P" (spell it) |
| CalDAV | "CAL-dav" |
| CardDAV | "CARD-dav" |
| DKIM | "D-KIM" or "dee-kim" |
| SPF | "S-P-F" (spell it) |
| DMARC | "D-MARK" |
| Sieve | "SIV" (like kitchen sieve) |
| Redis | "RED-iss" |
| TLS | "T-L-S" (spell it) |
| RFC | "R-F-C" (spell it) |
| VPS | "V-P-S" (spell it) |

## Delivery Tips

1. **Pause at every (pause) marker** - These are breathing points
2. **Slow down on technical terms** - Give viewers time to absorb
3. **Emphasize words with asterisks** - These are *key* words
4. **Eye contact on direct statements** - "Stop renting your email"
5. **Smile at small wins** - When checks pass, when things work

## Recording Strategy

**Record in this order (easiest to hardest):**
1. Terminal sections (voice only, screen does the work)
2. Admin dashboard tour (pointing at things)
3. Opening hook (you've warmed up)
4. Camera-to-face sections (do these last)

**Per section:**
- Record each section 3 times minimum
- Take the best one
- It's okay to cut between takes

## What to Record

### Terminal Recordings
- [ ] `mailserver preflight` - full output
- [ ] `mailserver setup` - complete wizard
- [ ] `mailserver dns generate example.com`
- [ ] `mailserver dns check example.com`
- [ ] `mailserver serve` - startup output
- [ ] `mailserver doctor` - health check

### Screen Recordings
- [ ] Admin login and dashboard
- [ ] User management page
- [ ] Domain management page
- [ ] Queue management page
- [ ] Logs page
- [ ] Sieve script creation

### Client Setup
- [ ] Apple Mail account setup
- [ ] Sending test email
- [ ] Email headers (DKIM/SPF/DMARC pass)
- [ ] Apple Calendar CalDAV setup
- [ ] Contacts CardDAV setup

### Graphics Needed
- [ ] Pricing comparison (Google vs Microsoft vs Self-hosted)
- [ ] Feature bullet points
- [ ] Final comparison table
- [ ] Title card
- [ ] End card with GitHub link

## Timestamps for YouTube Chapters

```
0:00 - Introduction
0:35 - Why I Built This
1:45 - What's Included
3:00 - Live Setup Demo
6:00 - Admin Dashboard Tour
7:00 - Email Client Setup
8:00 - Advanced Features
9:00 - Final Comparison
```

## Thumbnail Ideas

1. **Terminal + Face Split**
   - Left: Your face looking confident
   - Right: Terminal with commands
   - Text: "REPLACED GOOGLE"

2. **Cost Comparison**
   - Red X over Google/Microsoft logos
   - Green check over terminal
   - Text: "$60/year"

3. **Technical Flex**
   - Black background
   - Green terminal text: IMAP SMTP CalDAV CardDAV
   - Text: "I BUILT THIS"

---

## Final Checklist Before Recording

- [ ] Script printed or on teleprompter
- [ ] Pronunciation guide reviewed
- [ ] Demo server ready and tested
- [ ] Screen recording software ready
- [ ] Camera and lighting set up
- [ ] Quiet environment
- [ ] Water nearby

---

**Total Word Count:** ~2,100 words
**Estimated Runtime:** 9-10 minutes at slow pace

**Remember:** Your accent is fine. Your knowledge is the value. Ship it.
