"use client";

import { useEffect, useState } from "react";
import { PageShell } from "@/components/shared/page-shell";
import { api } from "@/lib/api";
import { Code, Key, Webhook, FileCode2, Mail, Loader2, Copy, ArrowRight } from "lucide-react";
import Link from "next/link";
import { toast } from "sonner";

interface APIStats {
  sent_today: number;
  sent_week: number;
  sent_month: number;
  active_api_keys: number;
  active_webhooks: number;
  active_templates: number;
  delivery_rate: number;
  open_rate: number;
}

function APIOverviewContent() {
  const [stats, setStats] = useState<APIStats | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api.get<APIStats>("/v1/api-stats").then((res) => {
      if (res.success && res.data) setStats(res.data);
      setLoading(false);
    });
  }, []);

  const copySnippet = (code: string) => {
    navigator.clipboard.writeText(code);
    toast.success("Copied to clipboard");
  };

  const endpoint = typeof window !== "undefined"
    ? `${window.location.protocol}//${window.location.host}/api/v1`
    : "https://your-server.com/api/v1";

  const curlExample = `curl -X POST ${endpoint}/send \\
  -H "Authorization: Bearer YOUR_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "from": "you@yourdomain.com",
    "to": "recipient@example.com",
    "subject": "Hello from MailServer",
    "html": "<h1>Hello!</h1><p>Sent via the API.</p>"
  }'`;

  const nodeExample = `const res = await fetch("${endpoint}/send", {
  method: "POST",
  headers: {
    "Authorization": "Bearer YOUR_API_KEY",
    "Content-Type": "application/json",
  },
  body: JSON.stringify({
    from: "you@yourdomain.com",
    to: "recipient@example.com",
    subject: "Hello from MailServer",
    html: "<h1>Hello!</h1>",
  }),
});`;

  const pythonExample = `import requests

requests.post("${endpoint}/send",
    headers={"Authorization": "Bearer YOUR_API_KEY"},
    json={
        "from": "you@yourdomain.com",
        "to": "recipient@example.com",
        "subject": "Hello from MailServer",
        "html": "<h1>Hello!</h1>",
    }
)`;

  const quickLinks = [
    { label: "API Keys", href: "/api-keys/", icon: Key, desc: "Create and manage API keys" },
    { label: "Webhooks", href: "/webhooks/", icon: Webhook, desc: "Set up delivery notifications" },
    { label: "Templates", href: "/templates/", icon: FileCode2, desc: "Reusable email templates" },
    { label: "Send Logs", href: "/emails/", icon: Mail, desc: "View sent email history" },
  ];

  if (loading) {
    return <PageShell title="API Overview"><div className="flex justify-center py-12"><Loader2 className="h-5 w-5 animate-spin text-muted-foreground" /></div></PageShell>;
  }

  return (
    <PageShell title="API Overview" description="Send transactional emails via REST API">
      {/* Stats */}
      {stats && (
        <div className="grid gap-3 grid-cols-2 lg:grid-cols-4">
          {[
            { label: "Sent Today", value: stats.sent_today },
            { label: "Sent This Week", value: stats.sent_week },
            { label: "Delivery Rate", value: `${stats.delivery_rate.toFixed(1)}%` },
            { label: "Open Rate", value: `${stats.open_rate.toFixed(1)}%` },
          ].map((s) => (
            <div key={s.label} className="rounded-lg border border-border bg-card p-3.5">
              <p className="text-[11px] text-muted-foreground/70 font-medium uppercase tracking-wider">{s.label}</p>
              <p className="text-xl font-semibold tracking-tight mt-1.5">{s.value}</p>
            </div>
          ))}
        </div>
      )}

      {/* Quick links */}
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        {quickLinks.map((link) => (
          <Link key={link.href} href={link.href}
            className="rounded-lg border border-border bg-card p-4 hover:border-border/80 hover:bg-accent/30 transition-colors group">
            <link.icon className="h-5 w-5 text-muted-foreground mb-2" />
            <p className="text-[13px] font-medium group-hover:text-primary">{link.label}</p>
            <p className="text-[11px] text-muted-foreground mt-0.5">{link.desc}</p>
          </Link>
        ))}
      </div>

      {/* Quick start */}
      <div className="space-y-3">
        <h2 className="text-[13px] font-semibold">Quick Start</h2>
        <div className="text-[12px] text-muted-foreground mb-2">
          API Endpoint: <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-foreground">{endpoint}</code>
        </div>

        {[
          { label: "cURL", code: curlExample },
          { label: "Node.js", code: nodeExample },
          { label: "Python", code: pythonExample },
        ].map((snippet) => (
          <div key={snippet.label} className="rounded-lg border border-border bg-card overflow-hidden">
            <div className="flex items-center justify-between border-b border-border px-3 py-1.5 bg-muted/30">
              <span className="text-[11px] font-medium text-muted-foreground">{snippet.label}</span>
              <button onClick={() => copySnippet(snippet.code)} className="flex items-center gap-1 text-[11px] text-muted-foreground hover:text-foreground">
                <Copy className="h-3 w-3" /> Copy
              </button>
            </div>
            <pre className="p-3 text-[12px] font-mono overflow-x-auto text-muted-foreground leading-relaxed">{snippet.code}</pre>
          </div>
        ))}
      </div>
    </PageShell>
  );
}

export default function APIPage() {
  return <APIOverviewContent />;
}
