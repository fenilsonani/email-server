"use client";

import { useEffect, useState, useCallback } from "react";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { DataTable } from "@/components/shared/data-table";
import { api } from "@/lib/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { formatDistanceToNow } from "date-fns";
import { Inbox, RefreshCw, RotateCcw, Trash2 } from "lucide-react";
import { toast } from "sonner";
import type { ColumnDef } from "@tanstack/react-table";

interface QueueMessage {
  id: string;
  sender: string;
  recipient: string;
  subject: string;
  status: string;
  attempts: number;
  next_retry: string;
  created_at: string;
}

function PageContent() {
  const [messages, setMessages] = useState<QueueMessage[]>([]);
  const [loading, setLoading] = useState(true);

  const fetchQueue = useCallback(async () => {
    setLoading(true);
    const res = await api.get<QueueMessage[]>("/v1/queue");
    if (res.success && res.data) setMessages(res.data);
    setLoading(false);
  }, []);

  useEffect(() => { fetchQueue(); }, [fetchQueue]);

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

  const columns: ColumnDef<QueueMessage>[] = [
    {
      accessorKey: "sender",
      header: "Sender",
      cell: ({ row }) => <span className="font-medium">{row.original.sender}</span>,
    },
    {
      accessorKey: "recipient",
      header: "Recipient",
      cell: ({ row }) => <span className="text-muted-foreground">{row.original.recipient}</span>,
    },
    {
      accessorKey: "subject",
      header: "Subject",
      enableSorting: false,
      cell: ({ row }) => <span className="max-w-[180px] truncate block">{row.original.subject}</span>,
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
      cell: ({ row }) => <span className="text-muted-foreground tabular-nums">{row.original.attempts}</span>,
    },
    {
      accessorKey: "next_retry",
      header: "Next Retry",
      cell: ({ row }) => (
        <span className="text-muted-foreground tabular-nums">
          {row.original.next_retry ? formatDistanceToNow(new Date(row.original.next_retry), { addSuffix: true }) : "-"}
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

  if (!loading && messages.length === 0) {
    return (
      <PageShell
        title="Message Queue"
        actions={
          <Button variant="outline" size="sm" onClick={fetchQueue} className="h-8 text-[12px] gap-1.5">
            <RefreshCw className="h-3.5 w-3.5" />Refresh
          </Button>
        }
      >
        <div className="flex flex-col items-center justify-center py-16 text-center">
          <Inbox className="h-10 w-10 text-muted-foreground/20 mb-3" strokeWidth={1} />
          <p className="text-[13px] text-muted-foreground/50">Queue is empty</p>
        </div>
      </PageShell>
    );
  }

  return (
    <PageShell
      title="Message Queue"
      actions={
        <Button variant="outline" size="sm" onClick={fetchQueue} className="h-8 text-[12px] gap-1.5">
          <RefreshCw className="h-3.5 w-3.5" />Refresh
        </Button>
      }
    >
      <DataTable
        columns={columns}
        data={messages}
        loading={loading}
        emptyMessage="Queue is empty."
      />
    </PageShell>
  );
}

export default function Page() {
  return <AuthGuard><PageContent /></AuthGuard>;
}
