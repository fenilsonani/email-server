"use client";

import { useEffect, useState, useCallback } from "react";
import Link from "next/link";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { DataTable } from "@/components/shared/data-table";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { ArrowLeft, RefreshCw } from "lucide-react";
import { formatDistanceToNow } from "date-fns";
import type { ColumnDef } from "@tanstack/react-table";

interface SnoozedEmail {
  id: number;
  message_id: number;
  original_mailbox_id: number;
  wake_at: string;
  mark_unread: boolean;
  created_at: string;
}

function PageContent() {
  const [emails, setEmails] = useState<SnoozedEmail[]>([]);
  const [loading, setLoading] = useState(true);

  const fetchEmails = useCallback(async () => {
    setLoading(true);
    const res = await api.get<SnoozedEmail[]>("/v1/features/snoozed");
    if (res.success && res.data) setEmails(res.data);
    setLoading(false);
  }, []);

  useEffect(() => { fetchEmails(); }, [fetchEmails]);

  const columns: ColumnDef<SnoozedEmail>[] = [
    {
      accessorKey: "message_id",
      header: "Message ID",
      cell: ({ row }) => (
        <span className="font-mono text-[12px]">#{row.original.message_id}</span>
      ),
    },
    {
      accessorKey: "wake_at",
      header: "Wake At",
      cell: ({ row }) => (
        <span className="text-muted-foreground tabular-nums">
          {formatDistanceToNow(new Date(row.original.wake_at), { addSuffix: true })}
        </span>
      ),
    },
    {
      accessorKey: "mark_unread",
      header: "Mark Unread",
      cell: ({ row }) => (
        <span className="text-[12px] text-muted-foreground">{row.original.mark_unread ? "Yes" : "No"}</span>
      ),
    },
    {
      accessorKey: "created_at",
      header: "Snoozed",
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
            <h1 className="text-[15px] font-semibold tracking-tight">Snoozed Emails</h1>
            <p className="text-[12px] text-muted-foreground/70 mt-0.5">Temporarily hidden emails</p>
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
        searchKey="message_id"
        searchPlaceholder="Filter by message ID..."
        emptyMessage="No snoozed emails."
      />
    </PageShell>
  );
}

export default function Page() {
  return <AuthGuard><PageContent /></AuthGuard>;
}
