"use client";

import { useEffect, useState, useCallback } from "react";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { DataTable } from "@/components/shared/data-table";
import { api } from "@/lib/api";
import type { DeliveryLog } from "@/lib/types";
import { Badge } from "@/components/ui/badge";
import { formatDistanceToNow } from "date-fns";
import type { ColumnDef } from "@tanstack/react-table";

function statusVariant(status: string) {
  switch (status.toLowerCase()) {
    case "delivered": return "default" as const;
    case "failed": return "destructive" as const;
    case "deferred": return "outline" as const;
    default: return "secondary" as const;
  }
}

const columns: ColumnDef<DeliveryLog>[] = [
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
    accessorKey: "status",
    header: "Status",
    enableSorting: false,
    cell: ({ row }) => (
      <Badge variant={statusVariant(row.original.status)} className="text-[10px]">
        {row.original.status}
      </Badge>
    ),
  },
  {
    accessorKey: "message",
    header: "Message",
    enableSorting: false,
    cell: ({ row }) => (
      <span className="text-muted-foreground font-mono text-[12px] max-w-[240px] truncate block">
        {row.original.message}
      </span>
    ),
  },
  {
    accessorKey: "created_at",
    header: "Time",
    cell: ({ row }) => (
      <span className="text-muted-foreground tabular-nums">
        {formatDistanceToNow(new Date(row.original.created_at), { addSuffix: true })}
      </span>
    ),
  },
];

function PageContent() {
  const [logs, setLogs] = useState<DeliveryLog[]>([]);
  const [loading, setLoading] = useState(true);
  const [page, setPage] = useState(1);
  const [totalPages, setTotalPages] = useState(1);
  const [totalCount, setTotalCount] = useState(0);

  const fetchLogs = useCallback(async () => {
    setLoading(true);
    const res = await api.get<DeliveryLog[]>("/v1/logs/delivery", { page: String(page) });
    if (res.success && res.data) {
      setLogs(res.data);
      if (res.meta) {
        setTotalPages(res.meta.total_pages);
        setTotalCount(res.meta.total_count);
      }
    }
    setLoading(false);
  }, [page]);

  useEffect(() => { fetchLogs(); }, [fetchLogs]);

  return (
    <PageShell title="Delivery Logs" description="Outbound email delivery status">
      <DataTable
        columns={columns}
        data={logs}
        loading={loading}
        searchKey="sender"
        searchPlaceholder="Filter by sender..."
        emptyMessage="No delivery logs found."
        serverPagination={{ page, totalPages, totalCount, onPageChange: setPage }}
      />
    </PageShell>
  );
}

export default function Page() {
  return <AuthGuard><PageContent /></AuthGuard>;
}
