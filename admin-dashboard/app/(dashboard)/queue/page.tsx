"use client";

import { useEffect, useState, useCallback, useMemo } from "react";
import { PageShell } from "@/components/shared/page-shell";
import { DataTable } from "@/components/shared/data-table";
import { api } from "@/lib/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { formatDistanceToNow } from "date-fns";
import { Inbox, RefreshCw, RotateCcw, Trash2, FileDown, Timer } from "lucide-react";
import { toast } from "sonner";
import type { ColumnDef } from "@tanstack/react-table";

interface QueueMessage {
  id: string;
  sender: string;
  recipients: string[];
  message_path: string;
  size: number;
  attempts: number;
  max_attempts: number;
  last_attempt: string;
  next_attempt: string;
  last_error: string;
  status: string;
  created_at: string;
  domain: string;
  priority: string;
}

type Tab = "all" | "pending" | "failed";

function PageContent() {
  const [messages, setMessages] = useState<QueueMessage[]>([]);
  const [loading, setLoading] = useState(true);
  const [autoRefresh, setAutoRefresh] = useState(false);
  const [tab, setTab] = useState<Tab>("all");

  const fetchQueue = useCallback(async () => {
    setLoading(true);
    const res = await api.get<QueueMessage[]>("/v1/queue");
    if (res.success && Array.isArray(res.data)) setMessages(res.data);
    setLoading(false);
  }, []);

  useEffect(() => { fetchQueue(); }, [fetchQueue]);

  useEffect(() => {
    if (!autoRefresh) return;
    const interval = setInterval(fetchQueue, 10000);
    return () => clearInterval(interval);
  }, [autoRefresh, fetchQueue]);

  const counts = useMemo(() => {
    const pending = messages.filter(m => m.status !== "failed").length;
    const failed = messages.filter(m => m.status === "failed").length;
    return { all: messages.length, pending, failed };
  }, [messages]);

  const filtered = useMemo(() => {
    if (tab === "pending") return messages.filter(m => m.status !== "failed");
    if (tab === "failed") return messages.filter(m => m.status === "failed");
    return messages;
  }, [messages, tab]);

  const handleRetry = async (id: string) => {
    const res = await api.post(`/v1/queue/${id}/retry`);
    if (res.success) { toast.success("Queued for retry"); fetchQueue(); }
    else toast.error(res.error || "Failed to retry");
  };

  const handleDelete = async (id: string) => {
    const res = await api.delete(`/v1/queue/${id}`);
    if (res.success) { toast.success("Removed from queue"); fetchQueue(); }
    else toast.error(res.error || "Failed to delete");
  };

  const handleRetryAll = async () => {
    const targets = tab === "failed" ? filtered : messages;
    let ok = 0;
    for (const m of targets) {
      const res = await api.post(`/v1/queue/${m.id}/retry`);
      if (res.success) ok++;
    }
    toast.success(`Retried ${ok} of ${targets.length} messages`);
    fetchQueue();
  };

  const exportQueue = () => {
    const csv = ["ID,Sender,Recipients,Domain,Status,Attempts,Error,Created"];
    filtered.forEach(m => csv.push(`${m.id},"${m.sender}","${(m.recipients||[]).join(";")}",${m.domain},${m.status},${m.attempts}/${m.max_attempts},"${(m.last_error||"").replace(/"/g,'""')}",${m.created_at}`));
    const blob = new Blob([csv.join("\n")], { type: "text/csv" });
    const a = document.createElement("a"); a.href = URL.createObjectURL(blob); a.download = "queue.csv"; a.click(); URL.revokeObjectURL(a.href);
    toast.success("Queue exported");
  };

  const columns: ColumnDef<QueueMessage>[] = [
    {
      accessorKey: "sender",
      header: "Sender",
      cell: ({ row }) => <span className="font-mono text-[12px]">{row.original.sender}</span>,
    },
    {
      accessorKey: "recipients",
      header: "Recipients",
      cell: ({ row }) => (
        <span className="font-mono text-[12px] text-muted-foreground max-w-[200px] truncate block">
          {(row.original.recipients || []).join(", ")}
        </span>
      ),
    },
    {
      accessorKey: "domain",
      header: "Domain",
      cell: ({ row }) => <span className="text-[12px] text-muted-foreground">{row.original.domain}</span>,
    },
    {
      accessorKey: "status",
      header: "Status",
      cell: ({ row }) => (
        <Badge variant={row.original.status === "failed" ? "destructive" : "outline"} className="text-[10px]">
          {row.original.status}
        </Badge>
      ),
    },
    {
      accessorKey: "attempts",
      header: "Tries",
      cell: ({ row }) => (
        <span className="text-muted-foreground tabular-nums text-[12px]">
          {row.original.attempts}/{row.original.max_attempts}
        </span>
      ),
    },
    {
      accessorKey: "next_attempt",
      header: "Next Attempt",
      cell: ({ row }) => (
        <span className="text-muted-foreground tabular-nums text-[12px]">
          {row.original.next_attempt ? formatDistanceToNow(new Date(row.original.next_attempt), { addSuffix: true }) : "—"}
        </span>
      ),
    },
    {
      accessorKey: "last_error",
      header: "Error",
      enableSorting: false,
      cell: ({ row }) => (
        <span className="text-[12px] text-destructive/80 max-w-[200px] truncate block">
          {row.original.last_error || "—"}
        </span>
      ),
    },
    {
      id: "actions",
      header: "",
      enableSorting: false,
      cell: ({ row }) => (
        <div className="flex items-center justify-end gap-0.5">
          <button
            onClick={() => handleRetry(row.original.id)}
            title="Retry"
            className="inline-flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground/50 transition-colors hover:bg-accent hover:text-foreground"
          >
            <RotateCcw className="h-3.5 w-3.5" />
          </button>
          <button
            onClick={() => handleDelete(row.original.id)}
            title="Delete"
            className="inline-flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground/50 transition-colors hover:bg-destructive/10 hover:text-destructive"
          >
            <Trash2 className="h-3.5 w-3.5" />
          </button>
        </div>
      ),
    },
  ];

  const tabs: { key: Tab; label: string; count: number }[] = [
    { key: "all", label: "All", count: counts.all },
    { key: "pending", label: "Pending", count: counts.pending },
    { key: "failed", label: "Failed", count: counts.failed },
  ];

  return (
    <PageShell
      title="Message Queue"
      actions={
        <div className="flex items-center gap-2">
          <button
            onClick={() => setAutoRefresh(!autoRefresh)}
            className={`inline-flex items-center gap-1.5 rounded-md border px-3 py-1.5 text-[12px] transition-colors ${
              autoRefresh ? "border-emerald-500/30 bg-emerald-500/10 text-emerald-500" : "border-border text-muted-foreground hover:text-foreground"
            }`}
          >
            <Timer className="h-3.5 w-3.5" />
            {autoRefresh ? "Auto: On" : "Auto: Off"}
          </button>
          <Button variant="outline" size="sm" className="h-8 text-[12px] gap-1.5" onClick={exportQueue}>
            <FileDown className="h-3.5 w-3.5" />Export
          </Button>
          {(tab === "all" || tab === "failed") && filtered.length > 0 && (
            <Button variant="outline" size="sm" className="h-8 text-[12px] gap-1.5" onClick={handleRetryAll}>
              <RotateCcw className="h-3.5 w-3.5" />Retry {tab === "failed" ? "All Failed" : "All"}
            </Button>
          )}
          <Button variant="outline" size="sm" onClick={fetchQueue} className="h-8 text-[12px] gap-1.5">
            <RefreshCw className="h-3.5 w-3.5" />Refresh
          </Button>
        </div>
      }
    >
      {/* Tabs */}
      <div className="flex items-center gap-1 border-b border-border">
        {tabs.map((t) => (
          <button
            key={t.key}
            onClick={() => setTab(t.key)}
            className={`relative px-3 py-2 text-[13px] font-medium transition-colors ${
              tab === t.key
                ? "text-foreground"
                : "text-muted-foreground hover:text-foreground"
            }`}
          >
            {t.label}
            {t.count > 0 && (
              <span className={`ml-1.5 inline-flex items-center justify-center rounded-full px-1.5 py-0.5 text-[10px] font-medium tabular-nums ${
                t.key === "failed" && t.count > 0
                  ? "bg-destructive/10 text-destructive"
                  : "bg-muted text-muted-foreground"
              }`}>
                {t.count}
              </span>
            )}
            {tab === t.key && (
              <span className="absolute bottom-0 left-0 right-0 h-0.5 bg-primary rounded-full" />
            )}
          </button>
        ))}
      </div>

      {!loading && filtered.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-16 text-center">
          <Inbox className="h-10 w-10 text-muted-foreground/20 mb-3" strokeWidth={1} />
          <p className="text-[13px] text-muted-foreground/50">
            {tab === "failed" ? "No failed messages" : tab === "pending" ? "No pending messages" : "Queue is empty"}
          </p>
        </div>
      ) : (
        <DataTable
          columns={columns}
          data={filtered}
          loading={loading}
          emptyMessage="Queue is empty."
          tableMinWidth="950px"
        />
      )}
    </PageShell>
  );
}

export default function Page() {
  return <PageContent />;
}
