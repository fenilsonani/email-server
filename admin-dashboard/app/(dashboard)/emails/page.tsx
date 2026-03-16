"use client";

import { useEffect, useState } from "react";
import { PageShell } from "@/components/shared/page-shell";
import { api } from "@/lib/api";
import { Mail, Eye, MousePointer, Loader2, ChevronLeft, ChevronRight, RotateCcw } from "lucide-react";
import { formatDistanceToNow } from "date-fns";

interface SentEmail {
  id: number;
  from_email: string;
  to_email: string;
  subject: string | null;
  status: string;
  opened_count: number;
  clicked_count: number;
  created_at: string;
  delivered_at: string | null;
  bounced_at: string | null;
  template_slug: string | null;
}

interface EmailDetail {
  email: SentEmail & {
    smtp_response: string | null;
    bounce_reason: string | null;
    message_id: string | null;
    tracking_id: string | null;
  };
  attempts: {
    id: number;
    attempt_number: number;
    attempted_at: string;
    status: string;
    smtp_response: string | null;
    error_message: string | null;
  }[];
}

const statusColors: Record<string, string> = {
  queued: "bg-blue-500/10 text-blue-400",
  sent: "bg-sky-500/10 text-sky-400",
  delivered: "bg-emerald-500/10 text-emerald-400",
  bounced: "bg-red-500/10 text-red-400",
  failed: "bg-red-500/10 text-red-400",
};

function SendLogsContent() {
  const [emails, setEmails] = useState<SentEmail[]>([]);
  const [loading, setLoading] = useState(true);
  const [page, setPage] = useState(1);
  const [totalPages, setTotalPages] = useState(1);
  const [statusFilter, setStatusFilter] = useState("");
  const [detail, setDetail] = useState<EmailDetail | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [resending, setResending] = useState(false);
  const [resendResult, setResendResult] = useState<{ success: boolean; message: string } | null>(null);

  const load = (p: number = page) => {
    setLoading(true);
    const params: Record<string, string> = { page: String(p), page_size: "50" };
    if (statusFilter) params.status = statusFilter;
    api.get<SentEmail[]>("/v1/emails", params).then((res) => {
      if (res.success && Array.isArray(res.data)) {
        setEmails(res.data);
        if (res.meta) setTotalPages(res.meta.total_pages);
      }
      setLoading(false);
    });
  };

  useEffect(() => { load(1); setPage(1); }, [statusFilter]);
  useEffect(() => { load(); }, [page]);

  const showDetail = async (id: number) => {
    setDetailLoading(true);
    const res = await api.get<EmailDetail>(`/v1/emails/${id}`);
    if (res.success && res.data) setDetail(res.data);
    setDetailLoading(false);
  };

  const handleResend = async (emailId: number) => {
    setResending(true);
    setResendResult(null);
    try {
      const res = await api.post<{ message_id: string }>(`/v1/emails/${emailId}/resend`, {});
      if (res.success && res.data) {
        setResendResult({ success: true, message: `Resent! New Message ID: ${res.data.message_id}` });
      } else {
        setResendResult({ success: false, message: res.error || "Failed to resend" });
      }
    } catch {
      setResendResult({ success: false, message: "Failed to resend email" });
    }
    setResending(false);
  };

  if (detail) {
    const e = detail.email;
    const canResend = ["bounced", "failed", "delivered", "sent"].includes(e.status);
    return (
      <PageShell
        title={
          <button onClick={() => setDetail(null)} className="flex items-center gap-1 text-[15px] font-semibold hover:text-primary">
            <ChevronLeft className="h-4 w-4" /> Email Detail
          </button>
        }
      >
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="rounded-lg border border-border bg-card p-4 space-y-3">
            <h3 className="text-[12px] font-medium text-muted-foreground uppercase">Details</h3>
            <div className="space-y-2 text-[13px]">
              <div className="flex justify-between"><span className="text-muted-foreground">From</span><span>{e.from_email}</span></div>
              <div className="flex justify-between"><span className="text-muted-foreground">To</span><span>{e.to_email}</span></div>
              <div className="flex justify-between"><span className="text-muted-foreground">Subject</span><span className="truncate max-w-[200px]">{e.subject || "(no subject)"}</span></div>
              <div className="flex justify-between"><span className="text-muted-foreground">Status</span>
                <span className={`rounded-full px-2 py-0.5 text-[11px] font-medium ${statusColors[e.status] || "bg-muted"}`}>{e.status}</span>
              </div>
              {e.template_slug && <div className="flex justify-between"><span className="text-muted-foreground">Template</span><span className="font-mono text-[12px]">{e.template_slug}</span></div>}
              {e.message_id && <div className="flex justify-between"><span className="text-muted-foreground">Message-ID</span><span className="font-mono text-[11px] truncate max-w-[200px]">{e.message_id}</span></div>}
            </div>
            {canResend && (
              <div className="pt-2 border-t border-border">
                <button
                  onClick={() => handleResend(e.id)}
                  disabled={resending}
                  className="flex items-center gap-1.5 rounded-md bg-primary px-3 py-1.5 text-[12px] font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
                >
                  {resending ? <Loader2 className="h-3 w-3 animate-spin" /> : <RotateCcw className="h-3 w-3" />}
                  Resend
                </button>
                {resendResult && (
                  <p className={`mt-2 text-[12px] ${resendResult.success ? "text-emerald-400" : "text-red-400"}`}>
                    {resendResult.message}
                  </p>
                )}
              </div>
            )}
          </div>
          <div className="rounded-lg border border-border bg-card p-4 space-y-3">
            <h3 className="text-[12px] font-medium text-muted-foreground uppercase">Tracking</h3>
            <div className="grid grid-cols-2 gap-4">
              <div className="text-center">
                <div className="flex items-center justify-center gap-1 text-muted-foreground"><Eye className="h-3.5 w-3.5" /></div>
                <p className="text-xl font-semibold mt-1">{e.opened_count}</p>
                <p className="text-[11px] text-muted-foreground">Opens</p>
              </div>
              <div className="text-center">
                <div className="flex items-center justify-center gap-1 text-muted-foreground"><MousePointer className="h-3.5 w-3.5" /></div>
                <p className="text-xl font-semibold mt-1">{e.clicked_count}</p>
                <p className="text-[11px] text-muted-foreground">Clicks</p>
              </div>
            </div>
            {e.bounce_reason && (
              <div className="rounded bg-red-500/10 p-2 text-[12px] text-red-400">
                Bounce: {e.bounce_reason}
              </div>
            )}
          </div>
        </div>

        {/* Timeline */}
        <div className="rounded-lg border border-border bg-card p-4">
          <h3 className="text-[12px] font-medium text-muted-foreground uppercase mb-3">Delivery Timeline</h3>
          <div className="space-y-2">
            <div className="flex items-center gap-3 text-[12px]">
              <span className="w-20 text-muted-foreground shrink-0">Queued</span>
              <span>{new Date(e.created_at).toLocaleString()}</span>
            </div>
            {detail.attempts.map((a) => (
              <div key={a.id} className="flex items-center gap-3 text-[12px]">
                <span className="w-20 text-muted-foreground shrink-0">Attempt {a.attempt_number}</span>
                <span className={`rounded-full px-2 py-0.5 text-[10px] font-medium ${statusColors[a.status] || "bg-muted"}`}>{a.status}</span>
                <span className="text-muted-foreground">{new Date(a.attempted_at).toLocaleString()}</span>
                {a.error_message && <span className="text-red-400">{a.error_message}</span>}
              </div>
            ))}
            {e.delivered_at && (
              <div className="flex items-center gap-3 text-[12px]">
                <span className="w-20 text-muted-foreground shrink-0">Delivered</span>
                <span>{new Date(e.delivered_at).toLocaleString()}</span>
              </div>
            )}
          </div>
        </div>
      </PageShell>
    );
  }

  return (
    <PageShell
      title="Send Logs"
      description="View all emails sent via the API"
      actions={
        <select
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value)}
          className="rounded-md border border-border bg-background px-3 py-1.5 text-[13px]"
        >
          <option value="">All statuses</option>
          <option value="queued">Queued</option>
          <option value="sent">Sent</option>
          <option value="delivered">Delivered</option>
          <option value="bounced">Bounced</option>
          <option value="failed">Failed</option>
        </select>
      }
    >
      <div className="rounded-lg border border-border bg-card overflow-x-auto">
        <table className="w-full text-[13px] min-w-[600px]">
          <thead>
            <tr className="border-b border-border bg-muted/30">
              <th className="px-4 py-2 text-left font-medium text-muted-foreground">To</th>
              <th className="px-4 py-2 text-left font-medium text-muted-foreground">Subject</th>
              <th className="px-4 py-2 text-left font-medium text-muted-foreground">Status</th>
              <th className="px-4 py-2 text-left font-medium text-muted-foreground">Opens</th>
              <th className="px-4 py-2 text-left font-medium text-muted-foreground">Clicks</th>
              <th className="px-4 py-2 text-left font-medium text-muted-foreground">Sent</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {loading ? (
              <tr><td colSpan={6} className="px-4 py-8 text-center"><Loader2 className="h-4 w-4 animate-spin mx-auto text-muted-foreground" /></td></tr>
            ) : emails.length === 0 ? (
              <tr><td colSpan={6} className="px-4 py-8 text-center text-muted-foreground">No emails sent yet</td></tr>
            ) : emails.map((e) => (
              <tr key={e.id} className="cursor-pointer hover:bg-accent/30" onClick={() => showDetail(e.id)}>
                <td className="px-4 py-2.5 truncate max-w-[180px]">{e.to_email}</td>
                <td className="px-4 py-2.5 text-muted-foreground truncate max-w-[200px]">{e.subject || "(no subject)"}</td>
                <td className="px-4 py-2.5">
                  <span className={`rounded-full px-2 py-0.5 text-[11px] font-medium ${statusColors[e.status] || "bg-muted"}`}>{e.status}</span>
                </td>
                <td className="px-4 py-2.5 text-muted-foreground">{e.opened_count}</td>
                <td className="px-4 py-2.5 text-muted-foreground">{e.clicked_count}</td>
                <td className="px-4 py-2.5 text-muted-foreground">{formatDistanceToNow(new Date(e.created_at), { addSuffix: true })}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Pagination */}
      {totalPages > 1 && (
        <div className="flex items-center justify-center gap-2">
          <button onClick={() => setPage(Math.max(1, page - 1))} disabled={page === 1}
            className="rounded-md p-1.5 hover:bg-accent disabled:opacity-30"><ChevronLeft className="h-4 w-4" /></button>
          <span className="text-[12px] text-muted-foreground">Page {page} of {totalPages}</span>
          <button onClick={() => setPage(Math.min(totalPages, page + 1))} disabled={page === totalPages}
            className="rounded-md p-1.5 hover:bg-accent disabled:opacity-30"><ChevronRight className="h-4 w-4" /></button>
        </div>
      )}
    </PageShell>
  );
}

export default function EmailsPage() {
  return <SendLogsContent />;
}
