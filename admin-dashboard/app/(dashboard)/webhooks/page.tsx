"use client";

import { useEffect, useState } from "react";
import { PageShell } from "@/components/shared/page-shell";
import { api } from "@/lib/api";
import { Webhook, Plus, Trash2, Play, Loader2, CheckCircle2, XCircle } from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";

interface WebhookItem {
  id: number;
  domain_id: number;
  url: string;
  events: string;
  is_active: boolean;
  failure_count: number;
  last_triggered_at: string | null;
  last_success_at: string | null;
  last_failure_at: string | null;
  last_failure_reason: string | null;
  created_at: string;
  domain_name: string;
}

interface Domain {
  id: number;
  name: string;
}

const ALL_EVENTS = [
  "email.sent", "email.delivered", "email.bounced",
  "email.opened", "email.clicked", "email.complained",
];

function WebhooksContent() {
  const [webhooks, setWebhooks] = useState<WebhookItem[]>([]);
  const [domains, setDomains] = useState<Domain[]>([]);
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const [creating, setCreating] = useState(false);

  const [url, setUrl] = useState("");
  const [domainId, setDomainId] = useState(0);
  const [events, setEvents] = useState<string[]>(ALL_EVENTS);

  const load = () => {
    api.get<WebhookItem[]>("/v1/webhooks").then((res) => {
      if (res.success && Array.isArray(res.data)) setWebhooks(res.data);
      setLoading(false);
    });
  };

  useEffect(() => {
    load();
    api.get<Domain[]>("/v1/domains-list").then((res) => {
      if (res.success && Array.isArray(res.data)) {
        setDomains(res.data);
        if (res.data.length > 0) setDomainId(res.data[0].id);
      }
    });
  }, []);

  const create = async () => {
    setCreating(true);
    const res = await api.post<{ id: number; secret: string }>("/v1/webhooks", {
      url, domain_id: domainId, events,
    });
    if (res.success && res.data) {
      toast.success(`Webhook created. Secret: ${res.data.secret}`);
      load();
      setShowCreate(false);
      setUrl("");
    } else {
      toast.error(res.error || "Failed to create webhook");
    }
    setCreating(false);
  };

  const toggle = async (id: number, active: boolean) => {
    const res = await api.put(`/v1/webhooks/${id}`, { is_active: !active });
    if (res.success) {
      toast.success(active ? "Webhook disabled" : "Webhook enabled");
    } else {
      toast.error(res.error || "Failed to update webhook");
    }
    load();
  };

  const deleteWebhook = async (id: number) => {
    if (!confirm("Delete this webhook?")) return;
    const res = await api.delete(`/v1/webhooks/${id}`);
    if (res.success) {
      toast.success("Webhook deleted");
      load();
    } else {
      toast.error(res.error || "Failed to delete webhook");
    }
  };

  const test = async (id: number) => {
    const res = await api.post(`/v1/webhooks/${id}/test`);
    if (res.success) toast.success("Test event sent");
    else toast.error(res.error || "Failed to send test event");
  };

  return (
    <PageShell
      title="Webhooks"
      description="Get notified when emails are sent, delivered, opened, or bounced"
      actions={
        <button
          onClick={() => setShowCreate(!showCreate)}
          className="flex items-center gap-1.5 rounded-md bg-primary px-3 py-1.5 text-[13px] font-medium text-primary-foreground hover:bg-primary/90"
        >
          <Plus className="h-3.5 w-3.5" /> Add Webhook
        </button>
      }
    >
      {showCreate && (
        <div className="rounded-lg border border-border bg-card p-4 space-y-3">
          <div className="grid gap-3 sm:grid-cols-2">
            <div>
              <label className="text-[11px] font-medium text-muted-foreground uppercase">URL (HTTPS)</label>
              <input
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder="https://example.com/webhook"
                className="mt-1 w-full rounded-md border border-border bg-background px-3 py-1.5 text-sm"
              />
            </div>
            <div>
              <label className="text-[11px] font-medium text-muted-foreground uppercase">Domain</label>
              <select
                value={domainId}
                onChange={(e) => setDomainId(Number(e.target.value))}
                className="mt-1 w-full rounded-md border border-border bg-background px-3 py-1.5 text-sm"
              >
                {domains.map((d) => <option key={d.id} value={d.id}>{d.name}</option>)}
              </select>
            </div>
          </div>
          <div>
            <label className="text-[11px] font-medium text-muted-foreground uppercase">Events</label>
            <div className="flex flex-wrap gap-2 mt-1">
              {ALL_EVENTS.map((ev) => (
                <label key={ev} className="flex items-center gap-1.5 text-[12px]">
                  <input
                    type="checkbox"
                    checked={events.includes(ev)}
                    onChange={(e) => {
                      if (e.target.checked) setEvents([...events, ev]);
                      else setEvents(events.filter((x) => x !== ev));
                    }}
                    className="rounded"
                  />
                  {ev}
                </label>
              ))}
            </div>
          </div>
          <button
            onClick={create}
            disabled={!url.startsWith("https://") || creating}
            className="rounded-md bg-primary px-3 py-1.5 text-[13px] font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
          >
            {creating ? <Loader2 className="h-4 w-4 animate-spin" /> : "Create Webhook"}
          </button>
        </div>
      )}

      <div className="space-y-3">
        {loading ? (
          <div className="text-center py-8"><Loader2 className="h-4 w-4 animate-spin mx-auto text-muted-foreground" /></div>
        ) : webhooks.length === 0 ? (
          <div className="text-center py-8 text-muted-foreground text-[13px]">No webhooks configured</div>
        ) : webhooks.map((wh) => {
          const parsedEvents: string[] = (() => { try { return JSON.parse(wh.events); } catch { return []; } })();
          return (
            <div key={wh.id} className="rounded-lg border border-border bg-card p-4">
              <div className="flex items-start justify-between gap-4">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <p className="text-[13px] font-medium font-mono truncate">{wh.url}</p>
                    <span className={`inline-flex rounded-full px-2 py-0.5 text-[10px] font-medium ${
                      wh.is_active ? "bg-emerald-500/10 text-emerald-400" : "bg-muted text-muted-foreground"
                    }`}>
                      {wh.is_active ? "Active" : "Disabled"}
                    </span>
                  </div>
                  <div className="flex flex-wrap gap-1 mt-1.5">
                    {parsedEvents.map((ev: string) => (
                      <span key={ev} className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">{ev}</span>
                    ))}
                  </div>
                  <div className="flex items-center gap-3 mt-2 text-[11px] text-muted-foreground">
                    <span>{wh.domain_name}</span>
                    {wh.failure_count > 0 && (
                      <span className="text-amber-400">{wh.failure_count} failures</span>
                    )}
                    {wh.last_triggered_at && (
                      <span>Last triggered {formatDistanceToNow(new Date(wh.last_triggered_at), { addSuffix: true })}</span>
                    )}
                  </div>
                </div>
                <div className="flex items-center gap-1">
                  <button onClick={() => test(wh.id)} className="rounded p-1.5 hover:bg-accent text-muted-foreground" title="Send test">
                    <Play className="h-3.5 w-3.5" />
                  </button>
                  <button onClick={() => toggle(wh.id, wh.is_active)} className="rounded p-1.5 hover:bg-accent text-muted-foreground" title="Toggle">
                    {wh.is_active ? <XCircle className="h-3.5 w-3.5" /> : <CheckCircle2 className="h-3.5 w-3.5" />}
                  </button>
                  <button onClick={() => deleteWebhook(wh.id)} className="rounded p-1.5 hover:bg-accent text-muted-foreground hover:text-red-400" title="Delete">
                    <Trash2 className="h-3.5 w-3.5" />
                  </button>
                </div>
              </div>
            </div>
          );
        })}
      </div>
    </PageShell>
  );
}

export default function WebhooksPage() {
  return <WebhooksContent />;
}
