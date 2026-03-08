"use client";

import { useEffect, useState, useCallback } from "react";
import Link from "next/link";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { DataTable } from "@/components/shared/data-table";
import { api } from "@/lib/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ArrowLeft, RefreshCw } from "lucide-react";
import { formatDistanceToNow } from "date-fns";
import type { ColumnDef } from "@tanstack/react-table";

interface ScheduledEmail {
  id: number;
  from_address: string;
  recipients: string[];
  subject: string;
  send_at: string;
  status: "pending" | "sending" | "sent" | "cancelled" | "failed";
  created_at: string;
  error: string;
}

const statusVariant: Record<string, "default" | "outline" | "destructive"> = {
  pending: "outline",
  sending: "outline",
  sent: "default",
  cancelled: "outline",
  failed: "destructive",
};

function PageContent() {
  const [emails, setEmails] = useState<ScheduledEmail[]>([]);
  const [loading, setLoading] = useState(true);

  const fetchEmails = useCallback(async () => {
    setLoading(true);
    const res = await api.get<ScheduledEmail[]>("/v1/features/scheduled");
    if (res.success && Array.isArray(res.data)) setEmails(res.data);
    setLoading(false);
  }, []);

  useEffect(() => { fetchEmails(); }, [fetchEmails]);

  const columns: ColumnDef<ScheduledEmail>[] = [
    {
      accessorKey: "recipients",
      header: "To",
      cell: ({ row }) => (
        <span className="font-mono text-[12px]">{(row.original.recipients || []).join(", ")}</span>
      ),
    },
    {
      accessorKey: "subject",
      header: "Subject",
      cell: ({ row }) => (
        <span className="text-[13px] max-w-[200px] truncate block">{row.original.subject}</span>
      ),
    },
    {
      accessorKey: "send_at",
      header: "Scheduled",
      cell: ({ row }) => (
        <span className="text-muted-foreground tabular-nums">
          {formatDistanceToNow(new Date(row.original.send_at), { addSuffix: true })}
        </span>
      ),
    },
    {
      accessorKey: "status",
      header: "Status",
      cell: ({ row }) => (
        <Badge
          variant={statusVariant[row.original.status]}
          className="text-[10px]"
        >
          {row.original.status}
        </Badge>
      ),
    },
    {
      accessorKey: "created_at",
      header: "Created",
      cell: ({ row }) => (
        <span className="text-muted-foreground tabular-nums">
          {formatDistanceToNow(new Date(row.original.created_at), { addSuffix: true })}
        </span>
      ),
    },
  ];

  return (
    <PageShell
      title={
        <div className="flex items-center gap-3">
          <Link href="/features/">
            <Button variant="ghost" size="icon-xs">
              <ArrowLeft className="h-4 w-4" />
            </Button>
          </Link>
          <div>
            <h1 className="text-[15px] font-semibold tracking-tight">Scheduled Emails</h1>
            <p className="text-[12px] text-muted-foreground/70 mt-0.5">Delayed send emails</p>
          </div>
        </div>
      }
      actions={
        <Button variant="outline" size="sm" onClick={fetchEmails} className="h-8 text-[12px] gap-1.5">
          <RefreshCw className="h-3.5 w-3.5" />
          Refresh
        </Button>
      }
    >
      <DataTable
        columns={columns}
        data={emails}
        loading={loading}
        searchKey="subject"
        searchPlaceholder="Filter by subject..."
        emptyMessage="No scheduled emails."
      />
    </PageShell>
  );
}

export default function Page() {
  return <AuthGuard><PageContent /></AuthGuard>;
}
