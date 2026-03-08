"use client";

import { useEffect, useState } from "react";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { api } from "@/lib/api";
import { FileCode2, Plus, Trash2, Edit, Eye, Loader2, Send } from "lucide-react";
import { toast } from "sonner";

interface Template {
  id: number;
  domain_id: number;
  slug: string;
  name: string;
  subject: string;
  html_body: string | null;
  text_body: string | null;
  variables: string | null;
  is_active: boolean;
  created_at: string;
  updated_at: string;
  domain_name: string;
}

interface Domain {
  id: number;
  name: string;
}

function TemplatesContent() {
  const [templates, setTemplates] = useState<Template[]>([]);
  const [domains, setDomains] = useState<Domain[]>([]);
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState<Template | null>(null);
  const [showCreate, setShowCreate] = useState(false);
  const [preview, setPreview] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [testTarget, setTestTarget] = useState<Template | null>(null);
  const [testTo, setTestTo] = useState("");
  const [testVars, setTestVars] = useState<Record<string, string>>({});
  const [sending, setSending] = useState(false);

  // Form state
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [subject, setSubject] = useState("");
  const [htmlBody, setHtmlBody] = useState("");
  const [textBody, setTextBody] = useState("");
  const [domainId, setDomainId] = useState(0);

  const load = () => {
    api.get<Template[]>("/v1/templates").then((res) => {
      if (res.success && res.data) setTemplates(res.data);
      setLoading(false);
    });
  };

  useEffect(() => {
    load();
    api.get<Domain[]>("/v1/domains-list").then((res) => {
      if (res.success && res.data) {
        setDomains(res.data);
        if (res.data.length > 0) setDomainId(res.data[0].id);
      }
    });
  }, []);

  const resetForm = () => {
    setName(""); setSlug(""); setSubject(""); setHtmlBody(""); setTextBody("");
    setEditing(null); setShowCreate(false);
  };

  const startEdit = (t: Template) => {
    setEditing(t);
    setName(t.name);
    setSlug(t.slug);
    setSubject(t.subject);
    setHtmlBody(t.html_body || "");
    setTextBody(t.text_body || "");
    setDomainId(t.domain_id);
    setShowCreate(true);
  };

  const save = async () => {
    setSaving(true);
    if (editing) {
      const res = await api.put(`/v1/templates/${editing.id}`, {
        name, subject, html_body: htmlBody, text_body: textBody,
      });
      if (res.success) { toast.success("Template updated"); resetForm(); load(); }
      else toast.error(res.error || "Failed to update");
    } else {
      const res = await api.post("/v1/templates", {
        name, slug: slug || name.toLowerCase().replace(/\s+/g, "-"),
        subject, html_body: htmlBody, text_body: textBody, domain_id: domainId,
      });
      if (res.success) { toast.success("Template created"); resetForm(); load(); }
      else toast.error(res.error || "Failed to create");
    }
    setSaving(false);
  };

  const deleteTemplate = async (id: number) => {
    if (!confirm("Delete this template?")) return;
    await api.delete(`/v1/templates/${id}`);
    toast.success("Template deleted");
    load();
  };

  const extractVars = (body: string): string[] => {
    const matches = body.match(/\{\{(\w+)\}\}/g) || [];
    return [...new Set(matches.map((m) => m.replace(/[{}]/g, "")))];
  };

  const startTestSend = (t: Template) => {
    setTestTarget(t);
    setTestTo("");
    const vars: string[] = (() => { try { return JSON.parse(t.variables || "[]"); } catch { return []; } })();
    const defaults: Record<string, string> = {};
    vars.forEach((v) => { defaults[v] = `test_${v}`; });
    setTestVars(defaults);
  };

  const sendTest = async () => {
    if (!testTarget || !testTo) return;
    setSending(true);
    // Replace variables in subject and body
    let subj = testTarget.subject;
    let body = testTarget.html_body || testTarget.text_body || "";
    Object.entries(testVars).forEach(([k, v]) => {
      const re = new RegExp(`\\{\\{${k}\\}\\}`, "g");
      subj = subj.replace(re, v);
      body = body.replace(re, v);
    });
    const res = await api.post("/v1/tools/test-email", { to: testTo, subject: subj, body });
    if (res.success) {
      toast.success(`Test email sent to ${testTo}`);
      setTestTarget(null);
    } else {
      toast.error(res.error || "Failed to send test email");
    }
    setSending(false);
  };

  return (
    <PageShell
      title="Email Templates"
      description="Create reusable email templates with variable substitution"
      actions={
        <button
          onClick={() => { resetForm(); setShowCreate(true); }}
          className="flex items-center gap-1.5 rounded-md bg-primary px-3 py-1.5 text-[13px] font-medium text-primary-foreground hover:bg-primary/90"
        >
          <Plus className="h-3.5 w-3.5" /> New Template
        </button>
      }
    >
      {/* Editor */}
      {showCreate && (
        <div className="rounded-lg border border-border bg-card p-4 space-y-3">
          <h3 className="text-[13px] font-medium">{editing ? "Edit Template" : "New Template"}</h3>
          <div className="grid gap-3 sm:grid-cols-3">
            <div>
              <label className="text-[11px] font-medium text-muted-foreground uppercase">Name</label>
              <input value={name} onChange={(e) => setName(e.target.value)} className="mt-1 w-full rounded-md border border-border bg-background px-3 py-1.5 text-sm" />
            </div>
            <div>
              <label className="text-[11px] font-medium text-muted-foreground uppercase">Slug</label>
              <input value={slug} onChange={(e) => setSlug(e.target.value)} placeholder={name.toLowerCase().replace(/\s+/g, "-")} className="mt-1 w-full rounded-md border border-border bg-background px-3 py-1.5 text-sm" />
            </div>
            <div>
              <label className="text-[11px] font-medium text-muted-foreground uppercase">Domain</label>
              <select value={domainId} onChange={(e) => setDomainId(Number(e.target.value))} className="mt-1 w-full rounded-md border border-border bg-background px-3 py-1.5 text-sm">
                {domains.map((d) => <option key={d.id} value={d.id}>{d.name}</option>)}
              </select>
            </div>
          </div>
          <div>
            <label className="text-[11px] font-medium text-muted-foreground uppercase">Subject</label>
            <input value={subject} onChange={(e) => setSubject(e.target.value)} className="mt-1 w-full rounded-md border border-border bg-background px-3 py-1.5 text-sm" />
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <div>
              <label className="text-[11px] font-medium text-muted-foreground uppercase">HTML Body</label>
              <textarea value={htmlBody} onChange={(e) => setHtmlBody(e.target.value)} rows={10}
                className="mt-1 w-full rounded-md border border-border bg-background px-3 py-2 text-sm font-mono" />
            </div>
            <div>
              <div className="flex items-center justify-between">
                <label className="text-[11px] font-medium text-muted-foreground uppercase">Preview</label>
                <button onClick={() => setPreview(preview ? null : htmlBody)} className="text-[11px] text-primary">
                  <Eye className="h-3 w-3 inline mr-1" />{preview ? "Hide" : "Show"}
                </button>
              </div>
              {preview ? (
                <div className="mt-1 rounded-md border border-border bg-white p-3 h-[240px] overflow-auto"
                  dangerouslySetInnerHTML={{ __html: preview }} />
              ) : (
                <>
                  <label className="text-[11px] font-medium text-muted-foreground uppercase">Text Body</label>
                  <textarea value={textBody} onChange={(e) => setTextBody(e.target.value)} rows={10}
                    className="mt-1 w-full rounded-md border border-border bg-background px-3 py-2 text-sm font-mono" />
                </>
              )}
            </div>
          </div>
          {(htmlBody || textBody) && (
            <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
              Variables: {extractVars(htmlBody + textBody).map((v) => (
                <span key={v} className="rounded bg-muted px-1.5 py-0.5 font-mono">{`{{${v}}}`}</span>
              ))}
              {extractVars(htmlBody + textBody).length === 0 && <span>none detected</span>}
            </div>
          )}
          <div className="flex gap-2">
            <button onClick={save} disabled={!name || !subject || saving}
              className="rounded-md bg-primary px-3 py-1.5 text-[13px] font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50">
              {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : editing ? "Update" : "Create"}
            </button>
            <button onClick={resetForm} className="rounded-md px-3 py-1.5 text-[13px] text-muted-foreground hover:text-foreground">Cancel</button>
          </div>
        </div>
      )}

      {/* Templates list */}
      <div className="rounded-lg border border-border bg-card overflow-hidden">
        <table className="w-full text-[13px]">
          <thead>
            <tr className="border-b border-border bg-muted/30">
              <th className="px-4 py-2 text-left font-medium text-muted-foreground">Name</th>
              <th className="px-4 py-2 text-left font-medium text-muted-foreground">Slug</th>
              <th className="px-4 py-2 text-left font-medium text-muted-foreground">Subject</th>
              <th className="px-4 py-2 text-left font-medium text-muted-foreground">Domain</th>
              <th className="px-4 py-2 text-left font-medium text-muted-foreground">Variables</th>
              <th className="px-4 py-2"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {loading ? (
              <tr><td colSpan={6} className="px-4 py-8 text-center"><Loader2 className="h-4 w-4 animate-spin mx-auto text-muted-foreground" /></td></tr>
            ) : templates.length === 0 ? (
              <tr><td colSpan={6} className="px-4 py-8 text-center text-muted-foreground">No templates yet</td></tr>
            ) : templates.map((t) => {
              const vars: string[] = (() => { try { return JSON.parse(t.variables || "[]"); } catch { return []; } })();
              return (
                <tr key={t.id}>
                  <td className="px-4 py-2.5 font-medium">{t.name}</td>
                  <td className="px-4 py-2.5 font-mono text-[12px] text-muted-foreground">{t.slug}</td>
                  <td className="px-4 py-2.5 text-muted-foreground truncate max-w-[200px]">{t.subject}</td>
                  <td className="px-4 py-2.5 text-muted-foreground">{t.domain_name}</td>
                  <td className="px-4 py-2.5">
                    <div className="flex flex-wrap gap-1">
                      {vars.map((v: string) => (
                        <span key={v} className="rounded bg-muted px-1 py-0.5 text-[10px] font-mono">{v}</span>
                      ))}
                    </div>
                  </td>
                  <td className="px-4 py-2.5">
                    <div className="flex items-center gap-1">
                      <button onClick={() => startTestSend(t)} title="Send test email" className="rounded p-1 hover:bg-accent text-muted-foreground hover:text-primary"><Send className="h-3.5 w-3.5" /></button>
                      <button onClick={() => startEdit(t)} className="rounded p-1 hover:bg-accent text-muted-foreground"><Edit className="h-3.5 w-3.5" /></button>
                      <button onClick={() => deleteTemplate(t.id)} className="rounded p-1 hover:bg-accent text-muted-foreground hover:text-red-400"><Trash2 className="h-3.5 w-3.5" /></button>
                    </div>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      {/* Test Send Dialog */}
      {testTarget && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={() => setTestTarget(null)}>
          <div className="rounded-lg border border-border bg-card p-5 w-full max-w-md space-y-4 shadow-lg" onClick={(e) => e.stopPropagation()}>
            <h3 className="text-[14px] font-medium">Send Test: {testTarget.name}</h3>
            <div>
              <label className="text-[11px] font-medium text-muted-foreground uppercase">Recipient Email</label>
              <input value={testTo} onChange={(e) => setTestTo(e.target.value)} placeholder="test@example.com"
                className="mt-1 w-full rounded-md border border-border bg-background px-3 py-1.5 text-sm" />
            </div>
            {Object.keys(testVars).length > 0 && (
              <div className="space-y-2">
                <label className="text-[11px] font-medium text-muted-foreground uppercase">Template Variables</label>
                {Object.entries(testVars).map(([k, v]) => (
                  <div key={k} className="flex items-center gap-2">
                    <span className="text-[12px] font-mono text-muted-foreground w-24 shrink-0">{`{{${k}}}`}</span>
                    <input value={v} onChange={(e) => setTestVars(prev => ({ ...prev, [k]: e.target.value }))}
                      className="flex-1 rounded-md border border-border bg-background px-2 py-1 text-sm" />
                  </div>
                ))}
              </div>
            )}
            <div className="text-[12px] text-muted-foreground">
              Subject: <span className="font-medium text-foreground">{(() => {
                let s = testTarget.subject;
                Object.entries(testVars).forEach(([k, v]) => { s = s.replace(new RegExp(`\\{\\{${k}\\}\\}`, "g"), v); });
                return s;
              })()}</span>
            </div>
            <div className="flex gap-2 justify-end">
              <button onClick={() => setTestTarget(null)} className="rounded-md px-3 py-1.5 text-[13px] text-muted-foreground hover:text-foreground">Cancel</button>
              <button onClick={sendTest} disabled={!testTo || sending}
                className="flex items-center gap-1.5 rounded-md bg-primary px-3 py-1.5 text-[13px] font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50">
                {sending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Send className="h-3.5 w-3.5" />}
                Send Test
              </button>
            </div>
          </div>
        </div>
      )}
    </PageShell>
  );
}

export default function TemplatesPage() {
  return <AuthGuard><TemplatesContent /></AuthGuard>;
}
