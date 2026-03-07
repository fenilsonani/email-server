"use client";

import { useEffect, useState, useCallback } from "react";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { DataTable } from "@/components/shared/data-table";
import { api } from "@/lib/api";
import type { AuditLogEntry } from "@/lib/types";
import { formatDistanceToNow } from "date-fns";
import type { ColumnDef } from "@tanstack/react-table";

const columns: ColumnDef<AuditLogEntry>[] = [
  {
    accessorKey: "username",
    header: "User",
    cell: ({ row }) => <span className="font-medium">{row.original.username}</span>,
  },
  {
    accessorKey: "event",
    header: "Event",
    cell: ({ row }) => <span className="font-mono text-[12px]">{row.original.event}</span>,
  },
  {
    accessorKey: "target",
    header: "Target",
    cell: ({ row }) => <span className="text-muted-foreground">{row.original.target}</span>,
  },
  {
    accessorKey: "details",
    header: "Details",
    enableSorting: false,
    cell: ({ row }) => (
      <span className="text-muted-foreground max-w-[200px] truncate block" title={row.original.details}>
        {row.original.details}
      </span>
    ),
  },
  {
    accessorKey: "ip_address",
    header: "IP",
    cell: ({ row }) => <span className="font-mono text-[12px] text-muted-foreground">{row.original.ip_address}</span>,
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
  const [logs, setLogs] = useState<AuditLogEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [page, setPage] = useState(1);
  const [totalPages, setTotalPages] = useState(1);
  const [totalCount, setTotalCount] = useState(0);

  const fetchLogs = useCallback(async () => {
    setLoading(true);
    const res = await api.get<AuditLogEntry[]>("/v1/logs/audit", { page: String(page) });
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
    <PageShell title="Audit Logs" description="Administrative actions and changes">
      <DataTable
        columns={columns}
        data={logs}
        loading={loading}
        searchKey="username"
        searchPlaceholder="Filter by user..."
        emptyMessage="No audit logs found."
        serverPagination={{ page, totalPages, totalCount, onPageChange: setPage }}
      />
    </PageShell>
  );
}

export default function Page() {
  return <AuthGuard><PageContent /></AuthGuard>;
}
