"use client";

import { useEffect, useState, useCallback } from "react";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { DataTable } from "@/components/shared/data-table";
import { api } from "@/lib/api";
import type { DeliveryLog } from "@/lib/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { FileDown } from "lucide-react";
import { toast } from "sonner";
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
    <PageShell title="Delivery Logs" description="Outbound email delivery status"
      actions={
        <Button variant="outline" size="sm" className="h-8 text-[12px] gap-1.5" onClick={() => {
          const csv = ["Sender,Recipient,Status,Message,Time"];
          logs.forEach(l => csv.push(`"${l.sender}","${l.recipient}","${l.status}","${(l.message || "").replace(/"/g, '""')}","${l.created_at}"`));
          const blob = new Blob([csv.join("\n")], { type: "text/csv" });
          const a = document.createElement("a"); a.href = URL.createObjectURL(blob); a.download = "delivery-logs.csv"; a.click(); URL.revokeObjectURL(a.href);
          toast.success("Delivery logs exported");
        }}>
          <FileDown className="h-3.5 w-3.5" />Export
        </Button>
      }
    >
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
