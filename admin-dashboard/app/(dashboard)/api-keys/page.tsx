"use client";

import { useEffect, useState } from "react";
import { PageShell } from "@/components/shared/page-shell";
import { api } from "@/lib/api";
import { Key, Plus, Trash2, Copy, CheckCircle2, Loader2, AlertTriangle, Clock } from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow, isPast, isBefore, addDays } from "date-fns";

interface APIKey {
  id: number;
  domain_id: number;
  key_prefix: string;
  name: string;
  scopes: string;
  is_active: boolean;
  rate_limit_per_hour: number;
  last_used_at: string | null;
  created_at: string;
  expires_at: string | null;
  domain_name: string;
}

interface Domain {
  id: number;
  name: string;
}

function APIKeysContent() {
  const [keys, setKeys] = useState<APIKey[]>([]);
  const [domains, setDomains] = useState<Domain[]>([]);
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const [creating, setCreating] = useState(false);
  const [newKey, setNewKey] = useState<string | null>(null);

  // Form
  const [name, setName] = useState("");
  const [domainId, setDomainId] = useState(0);
  const [rateLimit, setRateLimit] = useState(1000);

  const loadKeys = () => {
    api.get<APIKey[]>("/v1/api-keys").then((res) => {
      if (res.success && Array.isArray(res.data)) setKeys(res.data);
      setLoading(false);
    });
  };

  useEffect(() => {
    loadKeys();
    api.get<Domain[]>("/v1/domains-list").then((res) => {
      if (res.success && Array.isArray(res.data)) {
        setDomains(res.data);
        if (res.data.length > 0) setDomainId(res.data[0].id);
      }
    });
  }, []);

  const createKey = async () => {
    setCreating(true);
    const res = await api.post<{ id: number; key: string }>("/v1/api-keys", {
      name,
      domain_id: domainId,
      rate_limit_per_hour: rateLimit,
    });
    if (res.success && res.data) {
      setNewKey(res.data.key);
      toast.success("API key created");
      loadKeys();
      setName("");
    } else {
      toast.error(res.error || "Failed to create key");
    }
    setCreating(false);
  };

  const revokeKey = async (id: number) => {
    if (!confirm("Revoke this API key? This cannot be undone.")) return;
    const res = await api.delete(`/v1/api-keys/${id}`);
    if (res.success) {
      toast.success("API key revoked");
      loadKeys();
    } else {
      toast.error(res.error || "Failed to revoke key");
    }
  };

  const copyKey = (key: string) => {
    navigator.clipboard.writeText(key);
    toast.success("Copied to clipboard");
  };

  return (
    <PageShell
      title="API Keys"
      description="Manage API keys for sending emails via the REST API"
      actions={
        <button
          onClick={() => { setShowCreate(!showCreate); setNewKey(null); }}
          className="flex items-center gap-1.5 rounded-md bg-primary px-3 py-1.5 text-[13px] font-medium text-primary-foreground hover:bg-primary/90"
        >
          <Plus className="h-3.5 w-3.5" /> Create Key
        </button>
      }
    >
      {/* New key reveal */}
      {newKey && (
        <div className="rounded-lg border border-emerald-500/30 bg-emerald-500/10 p-4 space-y-2">
          <p className="text-[12px] font-medium text-emerald-400">New API Key (copy now, it won&apos;t be shown again):</p>
          <div className="flex items-center gap-2">
            <code className="flex-1 rounded bg-background px-3 py-2 text-[12px] font-mono text-foreground break-all">{newKey}</code>
            <button onClick={() => copyKey(newKey)} className="rounded-md p-2 hover:bg-accent">
              <Copy className="h-4 w-4" />
            </button>
          </div>
        </div>
      )}

      {/* Create form */}
      {showCreate && !newKey && (
        <div className="rounded-lg border border-border bg-card p-4 space-y-3">
          <div className="grid gap-3 sm:grid-cols-3">
            <div>
              <label className="text-[11px] font-medium text-muted-foreground uppercase">Name</label>
              <input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="My app"
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
            <div>
              <label className="text-[11px] font-medium text-muted-foreground uppercase">Rate Limit/hr</label>
              <input
                type="number"
                value={rateLimit}
                onChange={(e) => setRateLimit(Number(e.target.value))}
                className="mt-1 w-full rounded-md border border-border bg-background px-3 py-1.5 text-sm"
              />
            </div>
          </div>
          <button
            onClick={createKey}
            disabled={!name || creating}
            className="rounded-md bg-primary px-3 py-1.5 text-[13px] font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
          >
            {creating ? <Loader2 className="h-4 w-4 animate-spin" /> : "Create API Key"}
          </button>
        </div>
      )}

      {/* Keys table */}
      <div className="rounded-lg border border-border bg-card overflow-hidden">
        <table className="w-full text-[13px]">
          <thead>
            <tr className="border-b border-border bg-muted/30">
              <th className="px-4 py-2 text-left font-medium text-muted-foreground">Name</th>
              <th className="px-4 py-2 text-left font-medium text-muted-foreground">Key</th>
              <th className="px-4 py-2 text-left font-medium text-muted-foreground">Domain</th>
              <th className="px-4 py-2 text-left font-medium text-muted-foreground">Rate Limit</th>
              <th className="px-4 py-2 text-left font-medium text-muted-foreground">Last Used</th>
              <th className="px-4 py-2 text-left font-medium text-muted-foreground">Expires</th>
              <th className="px-4 py-2 text-left font-medium text-muted-foreground">Status</th>
              <th className="px-4 py-2"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {loading ? (
              <tr><td colSpan={8} className="px-4 py-8 text-center text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin mx-auto" /></td></tr>
            ) : keys.length === 0 ? (
              <tr><td colSpan={8} className="px-4 py-8 text-center text-muted-foreground">No API keys yet</td></tr>
            ) : keys.map((k) => {
              const expired = k.expires_at ? isPast(new Date(k.expires_at)) : false;
              const expiringSoon = k.expires_at && !expired ? isBefore(new Date(k.expires_at), addDays(new Date(), 7)) : false;
              return (
              <tr key={k.id} className={!k.is_active || expired ? "opacity-50" : ""}>
                <td className="px-4 py-2.5 font-medium">{k.name}</td>
                <td className="px-4 py-2.5 font-mono text-[12px] text-muted-foreground">{k.key_prefix}...</td>
                <td className="px-4 py-2.5 text-muted-foreground">{k.domain_name}</td>
                <td className="px-4 py-2.5 text-muted-foreground">{k.rate_limit_per_hour}/hr</td>
                <td className="px-4 py-2.5 text-muted-foreground">
                  {k.last_used_at ? formatDistanceToNow(new Date(k.last_used_at), { addSuffix: true }) : "Never"}
                </td>
                <td className="px-4 py-2.5">
                  {k.expires_at ? (
                    <span className={`inline-flex items-center gap-1 text-[12px] ${
                      expired ? "text-destructive" : expiringSoon ? "text-amber-500" : "text-muted-foreground"
                    }`}>
                      {expired ? <AlertTriangle className="h-3 w-3" /> : expiringSoon ? <Clock className="h-3 w-3" /> : null}
                      {expired ? "Expired" : formatDistanceToNow(new Date(k.expires_at), { addSuffix: true })}
                    </span>
                  ) : (
                    <span className="text-[12px] text-muted-foreground/50">Never</span>
                  )}
                </td>
                <td className="px-4 py-2.5">
                  <span className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-medium ${
                    expired ? "bg-red-500/10 text-red-400" :
                    k.is_active ? "bg-emerald-500/10 text-emerald-400" : "bg-red-500/10 text-red-400"
                  }`}>
                    {expired ? "Expired" : k.is_active ? "Active" : "Revoked"}
                  </span>
                </td>
                <td className="px-4 py-2.5">
                  {k.is_active && (
                    <button onClick={() => revokeKey(k.id)} className="rounded p-1 hover:bg-accent text-muted-foreground hover:text-red-400">
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  )}
                </td>
              </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </PageShell>
  );
}

export default function APIKeysPage() {
  return <APIKeysContent />;
}
