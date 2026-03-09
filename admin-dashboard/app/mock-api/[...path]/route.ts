import { NextRequest, NextResponse } from "next/server";

// ============================================================================
// Mock API — runs when NEXT_PUBLIC_MOCK_API=true or Go backend is unavailable
// Demo credentials: admin / admin123
// ============================================================================

// In-memory state (resets on server restart)
const mockState = {
  setupComplete: false,
  authenticated: false,
  sessionToken: "",
  csrfToken: "mock-csrf-" + Math.random().toString(36).slice(2),
  adminPassword: "admin123", // default; overwritten by setup wizard

  // Data stores
  domains: [] as Record<string, unknown>[],
  users: [] as Record<string, unknown>[],
  apiKeys: [] as Record<string, unknown>[],
  webhooks: [] as Record<string, unknown>[],
  templates: [] as Record<string, unknown>[],
  sentEmails: [] as Record<string, unknown>[],
  orgs: [
    {
      id: 1, name: "Default", slug: "default", owner_user_id: 1,
      preset: "full", settings: {}, created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    },
  ] as Record<string, unknown>[],
  orgMembers: [] as Record<string, unknown>[],
  nextId: { apiKey: 1, webhook: 1, template: 1, email: 1, domain: 1, user: 1 },
};

function json(data: unknown, status = 200) {
  return NextResponse.json({ success: true, data }, { status });
}

function jsonError(msg: string, status = 400) {
  return NextResponse.json({ success: false, error: msg }, { status });
}

function isAuthed(req: NextRequest): boolean {
  const cookie = req.cookies.get("admin_session");
  return cookie?.value === mockState.sessionToken && mockState.sessionToken !== "";
}

// Seed demo data after setup
function seedDemoData() {
  const now = new Date().toISOString();
  const weekAgo = new Date(Date.now() - 7 * 86400000).toISOString();

  if (mockState.apiKeys.length === 0) {
    mockState.apiKeys.push({
      id: mockState.nextId.apiKey++, domain_id: 1, key_prefix: "ms_demo123", name: "Demo Key",
      scopes: '["send"]', is_active: true, rate_limit_per_hour: 1000,
      last_used_at: now, created_at: weekAgo, expires_at: null, domain_name: mockState.domains[0]?.name || "demo.com",
    });
  }

  if (mockState.webhooks.length === 0) {
    mockState.webhooks.push({
      id: mockState.nextId.webhook++, domain_id: 1, url: "https://example.com/webhook",
      events: '["email.sent","email.delivered","email.bounced"]', is_active: true, failure_count: 0,
      last_triggered_at: now, last_success_at: now, last_failure_at: null, last_failure_reason: null,
      created_at: weekAgo, domain_name: mockState.domains[0]?.name || "demo.com",
    });
  }

  if (mockState.templates.length === 0) {
    mockState.templates.push({
      id: mockState.nextId.template++, domain_id: 1, slug: "welcome", name: "Welcome Email",
      subject: "Welcome to {{company}}", html_body: "<h1>Welcome, {{name}}!</h1><p>Thanks for joining {{company}}.</p>",
      text_body: "Welcome, {{name}}! Thanks for joining {{company}}.", variables: '["name","company"]',
      is_active: true, created_at: weekAgo, updated_at: now, domain_name: mockState.domains[0]?.name || "demo.com",
    });
  }

  if (mockState.sentEmails.length === 0) {
    const statuses = ["delivered", "delivered", "delivered", "opened", "bounced", "queued"];
    for (let i = 0; i < 12; i++) {
      const status = statuses[i % statuses.length];
      const created = new Date(Date.now() - i * 3600000).toISOString();
      mockState.sentEmails.push({
        id: mockState.nextId.email++, domain_id: 1, api_key_id: 1,
        message_id: `<msg-${i}@demo.com>`, tracking_id: `trk-${i}`,
        from_email: `noreply@${mockState.domains[0]?.name || "demo.com"}`,
        to_email: `user${i}@example.com`, subject: `Demo email #${i + 1}`,
        template_slug: i % 3 === 0 ? "welcome" : null, tags: null,
        status, smtp_response: status === "delivered" ? "250 OK" : null,
        opened_at: status === "opened" ? created : null,
        opened_count: status === "opened" ? 2 : 0,
        clicked_at: null, clicked_count: 0,
        created_at: created,
        delivered_at: ["delivered", "opened"].includes(status) ? created : null,
        bounced_at: status === "bounced" ? created : null,
        bounce_reason: status === "bounced" ? "Mailbox not found" : null,
      });
    }
  }
}

export async function GET(req: NextRequest, { params }: { params: Promise<{ path: string[] }> }) {
  const { path } = await params;
  const route = "/" + path.join("/");

  // --- Public routes (no auth) ---
  if (route === "/auth/session") {
    if (isAuthed(req)) {
      return json({
        authenticated: true, username: "admin",
        email: mockState.users[0] ? `${(mockState.users[0] as { username: string }).username}@${mockState.domains[0] ? (mockState.domains[0] as { name: string }).name : "demo.com"}` : "admin@demo.com",
        user_id: 1,
      });
    }
    return json({ authenticated: false });
  }

  if (route === "/auth/csrf") {
    return json({ token: mockState.csrfToken });
  }

  if (route === "/v1/setup/status") {
    return json({
      needs_setup: !mockState.setupComplete,
      domain_count: mockState.domains.length,
      user_count: mockState.users.length,
    });
  }

  // --- Auth-required routes ---
  if (!isAuthed(req)) {
    return jsonError("Unauthorized", 401);
  }

  if (route === "/v1/stats") {
    return json({
      total_users: mockState.users.length,
      total_domains: mockState.domains.length,
      total_messages: mockState.sentEmails.length,
      queue_pending: 2, queue_failed: 0,
      total_lists: 0, total_list_members: 0, pending_moderation: 0,
      uptime_seconds: 86400, uptime_human: "1 day",
      recent_activity: [
        { time: new Date().toISOString(), type: "auth", description: "admin logged in via api from 127.0.0.1", status: "success" },
      ],
      server_hostname: mockState.domains[0] ? `mail.${(mockState.domains[0] as { name: string }).name}` : "mail.demo.com",
    });
  }

  if (route === "/v1/domains-list" || route === "/v1/domains") {
    return json(mockState.domains);
  }

  if (route === "/v1/users") {
    return json(mockState.users);
  }

  if (route === "/v1/api-keys") {
    return json(mockState.apiKeys);
  }

  if (route === "/v1/webhooks") {
    return json(mockState.webhooks);
  }

  if (route === "/v1/templates") {
    return json(mockState.templates);
  }

  if (route === "/v1/emails") {
    const page = Number(req.nextUrl.searchParams.get("page") || "1");
    const pageSize = Number(req.nextUrl.searchParams.get("page_size") || "50");
    const statusFilter = req.nextUrl.searchParams.get("status");
    let filtered = mockState.sentEmails;
    if (statusFilter) filtered = filtered.filter((e) => (e as { status: string }).status === statusFilter);
    const total = filtered.length;
    const start = (page - 1) * pageSize;
    const data = filtered.slice(start, start + pageSize);
    return NextResponse.json({
      success: true, data,
      meta: { page, page_size: pageSize, total_count: total, total_pages: Math.ceil(total / pageSize) || 1 },
    });
  }

  if (route.match(/^\/v1\/emails\/\d+$/)) {
    const id = Number(route.split("/").pop());
    const email = mockState.sentEmails.find((e) => (e as { id: number }).id === id);
    if (!email) return jsonError("Not found", 404);
    return json({
      email,
      attempts: [
        { id: 1, attempt_number: 1, attempted_at: (email as { created_at: string }).created_at, status: (email as { status: string }).status, smtp_response: "250 OK", error_message: null },
      ],
    });
  }

  if (route === "/v1/api-stats") {
    const delivered = mockState.sentEmails.filter((e) => (e as { status: string }).status === "delivered").length;
    const opened = mockState.sentEmails.filter((e) => (e as { opened_count: number }).opened_count > 0).length;
    const total = mockState.sentEmails.length;
    return json({
      sent_today: Math.min(total, 5), sent_week: Math.min(total, 10), sent_month: total,
      active_api_keys: mockState.apiKeys.filter((k) => (k as { is_active: boolean }).is_active).length,
      active_webhooks: mockState.webhooks.filter((w) => (w as { is_active: boolean }).is_active).length,
      active_templates: mockState.templates.filter((t) => (t as { is_active: boolean }).is_active).length,
      delivery_rate: total > 0 ? (delivered / total) * 100 : 0,
      open_rate: delivered > 0 ? (opened / delivered) * 100 : 0,
    });
  }

  if (route === "/v1/orgs") {
    return json(mockState.orgs);
  }

  if (route.match(/^\/v1\/orgs\/\d+$/)) {
    const id = Number(route.split("/").pop());
    const org = mockState.orgs.find((o) => (o as { id: number }).id === id);
    return org ? json(org) : jsonError("Not found", 404);
  }

  if (route.match(/^\/v1\/orgs\/\d+\/members$/)) {
    return json(mockState.orgMembers);
  }

  if (route === "/v1/presets") {
    return json({
      email_only: { label: "Email Hosting", description: "Full mailboxes with IMAP, SMTP, calendar", enabled_features: ["users", "domains", "lists", "features", "queue", "security", "logs", "sieve"] },
      api_only: { label: "Email API", description: "Send transactional emails via REST API", enabled_features: ["domains", "api_keys", "webhooks", "templates", "send_logs", "tracking", "suppression"] },
      full: { label: "Full Platform", description: "Everything: email hosting + API + lists", enabled_features: ["*"] },
    });
  }

  if (route === "/v1/system") {
    return json({
      hostname: mockState.domains[0] ? `mail.${(mockState.domains[0] as { name: string }).name}` : "mail.demo.com",
      domain: mockState.domains[0] ? (mockState.domains[0] as { name: string }).name : "demo.com",
      uptime_seconds: 86400, uptime_human: "1 day", version: "1.0.0-demo", go_version: "go1.22",
      config: { imap_port: 993, imaps_port: 993, smtp_port: 25, smtps_port: 465, admin_port: 8080, storage_path: "/var/lib/mailserver" },
    });
  }

  if (route === "/v1/features") {
    return json({ screener_count: 0, alias_count: 0, vip_count: 0, scheduled_count: 0, snoozed_count: 0 });
  }

  if (route === "/v1/lists") {
    return json([]);
  }

  if (route === "/v1/queue") {
    return json({ pending: [], failed: [] });
  }

  if (route === "/v1/analytics") {
    return json({ sent: 12, delivered: 10, bounced: 1, opened: 5, clicked: 2, period: "7d" });
  }

  if (route === "/v1/security/overview") {
    return json({ blocked_ips: 0, greylist_entries: 0, suppression_entries: 0, failed_logins_24h: 0 });
  }

  if (route === "/v1/logs/auth" || route === "/v1/logs/delivery" || route === "/v1/logs/audit") {
    return json([]);
  }

  if (route === "/v1/system/2fa/status") {
    return json({ enabled: false });
  }

  if (route === "/v1/system/backup/status") {
    return json({ last_backup: null, auto_enabled: false });
  }

  if (route === "/v1/system/backup/history") {
    return json([]);
  }

  if (route === "/v1/system/certificates") {
    return json({ certificates: [] });
  }

  if (route === "/v1/system/check-update") {
    return json({ current: "1.0.0-demo", latest: "1.0.0-demo", update_available: false });
  }

  if (route === "/v1/system/dkim-autorotate") {
    return json({ enabled: false, interval_days: 90, last_rotation: null });
  }

  if (route === "/v1/tools/doctor") {
    return json({ checks: [], status: "healthy" });
  }

  // Catch-all for features sub-routes
  if (route.startsWith("/v1/features/")) {
    return json([]);
  }

  return jsonError(`Mock: unhandled GET ${route}`, 404);
}

export async function POST(req: NextRequest, { params }: { params: Promise<{ path: string[] }> }) {
  const { path } = await params;
  const route = "/" + path.join("/");

  // --- Auth ---
  if (route === "/auth/login") {
    const body = await req.json();
    const validUser = mockState.users.length > 0
      ? (mockState.users[0] as { username: string }).username
      : "admin";
    if (body.username === validUser && body.password === mockState.adminPassword) {
      mockState.sessionToken = "mock-session-" + Math.random().toString(36).slice(2);
      const res = json({ needs_2fa: false, username: "admin" });
      res.cookies.set("admin_session", mockState.sessionToken, {
        path: "/admin", httpOnly: true, maxAge: 86400, sameSite: "strict",
      });
      return res;
    }
    return jsonError("Invalid credentials", 401);
  }

  if (route === "/auth/logout") {
    mockState.sessionToken = "";
    const res = json({ logged_out: true });
    res.cookies.set("admin_session", "", { path: "/admin", maxAge: 0 });
    return res;
  }

  // --- Setup ---
  if (route === "/v1/setup/check-dns") {
    const body = await req.json();
    return json({
      mx: { status: "missing", expected: `mail.${body.domain}`, found: null },
      a_record: { status: "missing", host: body.mail_hostname || `mail.${body.domain}` },
      spf: { status: "missing", expected: "v=spf1 mx -all" },
      dmarc: { status: "missing", expected: `v=DMARC1; p=quarantine; rua=mailto:postmaster@${body.domain}` },
    });
  }

  if (route === "/v1/setup/preflight") {
    return json({
      port_25: { status: "available", service: "SMTP" },
      port_587: { status: "available", service: "SMTP Submission" },
      port_993: { status: "available", service: "IMAPS" },
      redis: { status: "connected" },
      database: { status: "connected" },
    });
  }

  if (route === "/v1/setup/install") {
    const body = await req.json();
    const domain = body.domain || "demo.com";
    const adminEmail = body.admin_email || `admin@${domain}`;
    const parts = adminEmail.split("@");

    // Create domain
    mockState.domains.push({
      id: mockState.nextId.domain++, name: domain, mail_hostname: body.mail_hostname || `mail.${domain}`,
      is_primary: true, user_count: 1, dkim_enabled: true, created_at: new Date().toISOString(),
    });

    // Create admin user
    mockState.users.push({
      id: mockState.nextId.user++, username: parts[0], domain, email: adminEmail,
      is_admin: true, quota_bytes: 1073741824, used_bytes: 0, created_at: new Date().toISOString(),
    });

    // Update org
    mockState.orgs[0] = {
      ...mockState.orgs[0], name: domain, slug: domain.replace(/\./g, "-"),
      preset: body.preset || "full",
    };

    // Add org member
    mockState.orgMembers.push({
      id: 1, org_id: 1, user_id: 1, role: "owner",
      username: parts[0], email: adminEmail, created_at: new Date().toISOString(),
    });

    mockState.setupComplete = true;
    mockState.adminPassword = body.admin_password || "admin123";

    // Seed demo data
    seedDemoData();

    return json({
      success: true, domain_id: 1, user_id: 1,
      admin_email: adminEmail, preset: body.preset || "full",
      mail_hostname: body.mail_hostname || `mail.${domain}`,
    });
  }

  // --- Auth-required POST routes ---
  if (!isAuthed(req)) return jsonError("Unauthorized", 401);

  if (route === "/v1/api-keys") {
    const body = await req.json();
    const key = "ms_" + Math.random().toString(36).slice(2) + Math.random().toString(36).slice(2);
    const newKey = {
      id: mockState.nextId.apiKey++, domain_id: body.domain_id || 1,
      key_prefix: key.slice(0, 10), key_hash: "mock", name: body.name,
      scopes: body.scopes || '["send"]', is_active: true,
      rate_limit_per_hour: body.rate_limit_per_hour || 1000,
      last_used_at: null, created_at: new Date().toISOString(), expires_at: null,
      domain_name: (mockState.domains.find((d) => (d as { id: number }).id === (body.domain_id || 1)) as { name: string })?.name || "demo.com",
    };
    mockState.apiKeys.push(newKey);
    return json({ id: newKey.id, key, key_prefix: newKey.key_prefix, name: body.name, message: "Copy this key now. It will not be shown again." }, 201);
  }

  if (route === "/v1/webhooks") {
    const body = await req.json();
    const secret = "whsec_" + Math.random().toString(36).slice(2) + Math.random().toString(36).slice(2);
    const wh = {
      id: mockState.nextId.webhook++, domain_id: body.domain_id || 1, url: body.url,
      events: JSON.stringify(body.events || []), secret, is_active: true, failure_count: 0,
      last_triggered_at: null, last_success_at: null, last_failure_at: null, last_failure_reason: null,
      created_at: new Date().toISOString(),
      domain_name: (mockState.domains.find((d) => (d as { id: number }).id === (body.domain_id || 1)) as { name: string })?.name || "demo.com",
    };
    mockState.webhooks.push(wh);
    return json({ id: wh.id, url: body.url, secret, events: body.events }, 201);
  }

  if (route.match(/^\/v1\/webhooks\/\d+\/test$/)) {
    return json({ test_sent: true, event_type: "test", message: "Test event queued for delivery" });
  }

  if (route === "/v1/templates") {
    const body = await req.json();
    const vars = ((body.html_body || "") + (body.text_body || "")).match(/\{\{(\w+)\}\}/g) || [];
    const uniqueVars = [...new Set(vars.map((v: string) => v.replace(/[{}]/g, "")))];
    const t = {
      id: mockState.nextId.template++, domain_id: body.domain_id || 1,
      slug: body.slug || body.name.toLowerCase().replace(/\s+/g, "-"),
      name: body.name, subject: body.subject, html_body: body.html_body || null,
      text_body: body.text_body || null, variables: JSON.stringify(uniqueVars),
      is_active: true, created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
      domain_name: (mockState.domains.find((d) => (d as { id: number }).id === (body.domain_id || 1)) as { name: string })?.name || "demo.com",
    };
    mockState.templates.push(t);
    return json({ id: t.id, slug: t.slug, name: t.name, variables: uniqueVars }, 201);
  }

  if (route === "/v1/orgs") {
    const body = await req.json();
    const org = {
      id: mockState.orgs.length + 1, name: body.name,
      slug: body.name.toLowerCase().replace(/\s+/g, "-"),
      owner_user_id: 1, preset: body.preset || "full", settings: {},
      created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
    };
    mockState.orgs.push(org);
    return json(org, 201);
  }

  if (route === "/v1/tools/test-email") {
    return json({ sent: true, message: "Test email queued (mock mode)" });
  }

  if (route === "/v1/tools/dns-check") {
    return json({ results: {} });
  }

  if (route === "/v1/sieve/validate") {
    return json({ valid: true });
  }

  return jsonError(`Mock: unhandled POST ${route}`, 404);
}

export async function PUT(req: NextRequest, { params }: { params: Promise<{ path: string[] }> }) {
  const { path } = await params;
  const route = "/" + path.join("/");

  if (!isAuthed(req)) return jsonError("Unauthorized", 401);

  if (route.match(/^\/v1\/webhooks\/\d+$/)) {
    const id = Number(route.split("/").pop());
    const body = await req.json();
    const idx = mockState.webhooks.findIndex((w) => (w as { id: number }).id === id);
    if (idx >= 0) {
      mockState.webhooks[idx] = { ...mockState.webhooks[idx], ...body };
    }
    return json({ updated: true });
  }

  if (route.match(/^\/v1\/templates\/\d+$/)) {
    const id = Number(route.split("/").pop());
    const body = await req.json();
    const idx = mockState.templates.findIndex((t) => (t as { id: number }).id === id);
    if (idx >= 0) {
      mockState.templates[idx] = { ...mockState.templates[idx], ...body, updated_at: new Date().toISOString() };
    }
    return json({ updated: true });
  }

  if (route.match(/^\/v1\/orgs\/\d+$/)) {
    const id = Number(route.split("/").pop());
    const body = await req.json();
    const idx = mockState.orgs.findIndex((o) => (o as { id: number }).id === id);
    if (idx >= 0) {
      mockState.orgs[idx] = { ...mockState.orgs[idx], ...body, updated_at: new Date().toISOString() };
    }
    return json({ updated: true });
  }

  return jsonError(`Mock: unhandled PUT ${route}`, 404);
}

export async function DELETE(req: NextRequest, { params }: { params: Promise<{ path: string[] }> }) {
  const { path } = await params;
  const route = "/" + path.join("/");

  if (!isAuthed(req)) return jsonError("Unauthorized", 401);

  if (route.match(/^\/v1\/api-keys\/\d+$/)) {
    const id = Number(route.split("/").pop());
    const idx = mockState.apiKeys.findIndex((k) => (k as { id: number }).id === id);
    if (idx >= 0) (mockState.apiKeys[idx] as { is_active: boolean }).is_active = false;
    return json({ revoked: true });
  }

  if (route.match(/^\/v1\/webhooks\/\d+$/)) {
    const id = Number(route.split("/").pop());
    mockState.webhooks = mockState.webhooks.filter((w) => (w as { id: number }).id !== id);
    return json({ deleted: true });
  }

  if (route.match(/^\/v1\/templates\/\d+$/)) {
    const id = Number(route.split("/").pop());
    mockState.templates = mockState.templates.filter((t) => (t as { id: number }).id !== id);
    return json({ deleted: true });
  }

  return jsonError(`Mock: unhandled DELETE ${route}`, 404);
}
