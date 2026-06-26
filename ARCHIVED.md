# Archived — June 2026

short version of why this exists and why i stopped.

## why i built it

i wanted to deploy a full email server the easiest, most reliable way — run a wizard, point dns, done. own my mail, full features (imap, smtp, dkim/spf/dmarc, calendar, contacts, admin panel), no bills, nobody reading my stuff. and the code got there — the setup wizard, preflight, doctor all install clean and run.

## why i'm stopping

the hard part of email was never the code, it's deliverability — and you can't fix that in go.

- **ip blacklisting** — vps ips are distrusted by default, you hit spamhaus / microsoft blocks / gmail spam on day one, sometimes for the previous owner's sins.
- **port 25 blocked** by many vps providers + most isps, so outbound silently fails.
- **gmail/yahoo/microsoft bulk sender rules (2024+)** — strict dmarc alignment, one-click unsubscribe, complaint limits. a fresh solo ip rarely clears them.
- **forever ops job** — rdns, ip warming, blocklist watching, patching, acme, backups.

"just works" was never up to my server — it's up to gmail/microsoft/spamhaus accepting my mail, and they don't trust a new self-hosted ip. that part isn't a bug i can close. sending moved to managed senders that own trusted ips, so it doesn't make sense to keep building this.

## still want to deploy?

it still works — just know the above. and offload the part you can't win:

- **sending** → Cloudflare Email Service (send from workers, no api keys; public beta apr 2026; pair with Email Routing for inbound), or Resend / Postmark / AWS SES.
- **self-hosted mailbox, maintained** → **[Stalwart](https://github.com/stalwartlabs/mail-server)** (single rust binary, ~100mb, all-in-one — the one i'd pick now), [Maddy](https://github.com/foxcpp/maddy), [mailcow](https://mailcow.email), or [Mail-in-a-Box](https://mailinabox.email).

tl;dr — sending: Cloudflare Email Service / Resend / SES. mailbox: Stalwart. that combo is the "easiest + reliable + just works" i was chasing here.
