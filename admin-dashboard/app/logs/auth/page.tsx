"use client";

import { useEffect, useState, useCallback } from "react";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { DataTable } from "@/components/shared/data-table";
import { api } from "@/lib/api";
import type { AuthLog } from "@/lib/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { FileDown } from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";
import type { ColumnDef } from "@tanstack/react-table";

const columns: ColumnDef<AuthLog>[] = [
  {
    accessorKey: "username",
    header: "Username",
    cell: ({ row }) => <span className="font-medium">{row.original.username}</span>,
  },
  {
    accessorKey: "remote_ip",
    header: "Remote IP",
    cell: ({ row }) => <span className="font-mono text-[12px] text-muted-foreground">{row.original.remote_ip}</span>,
  },
  {
    accessorKey: "protocol",
    header: "Protocol",
    cell: ({ row }) => <span className="uppercase text-muted-foreground">{row.original.protocol}</span>,
  },
  {
    accessorKey: "success",
    header: "Status",
    enableSorting: false,
    cell: ({ row }) => (
      <Badge variant={row.original.success ? "default" : "destructive"} className="text-[10px]">
        {row.original.success ? "Success" : "Failed"}
      </Badge>
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
  const [logs, setLogs] = useState<AuthLog[]>([]);
  const [loading, setLoading] = useState(true);
  const [page, setPage] = useState(1);
  const [totalPages, setTotalPages] = useState(1);
  const [totalCount, setTotalCount] = useState(0);

  const fetchLogs = useCallback(async () => {
    setLoading(true);
    const res = await api.get<AuthLog[]>("/v1/logs/auth", { page: String(page) });
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
    <PageShell title="Auth Logs" description="Authentication attempts across all protocols"
      actions={
        <Button variant="outline" size="sm" className="h-8 text-[12px] gap-1.5" onClick={() => {
          const csv = ["Username,IP,Protocol,Status,Time"];
          logs.forEach(l => csv.push(`${l.username},${l.remote_ip},${l.protocol},${l.success ? "success" : "failed"},${l.created_at}`));
          const blob = new Blob([csv.join("\n")], { type: "text/csv" });
          const a = document.createElement("a"); a.href = URL.createObjectURL(blob); a.download = "auth-logs.csv"; a.click(); URL.revokeObjectURL(a.href);
          toast.success("Auth logs exported");
        }}>
          <FileDown className="h-3.5 w-3.5" />Export
        </Button>
      }
    >
      <DataTable
        columns={columns}
        data={logs}
        loading={loading}
        searchKey="username"
        searchPlaceholder="Filter by username..."
        emptyMessage="No auth logs found."
        serverPagination={{ page, totalPages, totalCount, onPageChange: setPage }}
      />
    </PageShell>
  );
}

export default function Page() {
  return <AuthGuard><PageContent /></AuthGuard>;
}
