"use client";

import { useEffect, useState, useCallback } from "react";
import { PageShell } from "@/components/shared/page-shell";
import { DataTable } from "@/components/shared/data-table";
import { api } from "@/lib/api";
import type { DeliveryLog } from "@/lib/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { FileDown, Calendar } from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow, subDays, format } from "date-fns";
import type { ColumnDef } from "@tanstack/react-table";

type DateRange = "all" | "today" | "7d" | "30d" | "custom";

function getDateRange(range: DateRange, customFrom: string, customTo: string) {
  const params: Record<string, string> = {};
  const now = new Date();
  switch (range) {
    case "today":
      params.from = format(new Date(now.getFullYear(), now.getMonth(), now.getDate()), "yyyy-MM-dd");
      break;
    case "7d":
      params.from = format(subDays(now, 7), "yyyy-MM-dd");
      break;
    case "30d":
      params.from = format(subDays(now, 30), "yyyy-MM-dd");
      break;
    case "custom":
      if (customFrom) params.from = customFrom;
      if (customTo) params.to = customTo;
      break;
  }
  return params;
}

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
  const [dateRange, setDateRange] = useState<DateRange>("all");
  const [customFrom, setCustomFrom] = useState("");
  const [customTo, setCustomTo] = useState("");

  const fetchLogs = useCallback(async () => {
    setLoading(true);
    const dateParams = getDateRange(dateRange, customFrom, customTo);
    const res = await api.get<DeliveryLog[]>("/v1/logs/delivery", { page: String(page), ...dateParams });
    if (res.success && Array.isArray(res.data)) {
      setLogs(res.data);
      if (res.meta) {
        setTotalPages(res.meta.total_pages);
        setTotalCount(res.meta.total_count);
      }
    }
    setLoading(false);
  }, [page, dateRange, customFrom, customTo]);

  useEffect(() => { fetchLogs(); }, [fetchLogs]);

  const handleRangeChange = (r: DateRange) => {
    setDateRange(r);
    setPage(1);
  };

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
      {/* Date Range Filter */}
      <div className="flex items-center gap-2 flex-wrap">
        <div className="flex items-center gap-1 rounded-lg border border-border bg-background/50 p-0.5">
          {([["all", "All"], ["today", "Today"], ["7d", "7 Days"], ["30d", "30 Days"], ["custom", "Custom"]] as const).map(([value, label]) => (
            <button
              key={value}
              onClick={() => handleRangeChange(value as DateRange)}
              className={`px-2.5 py-1 text-[12px] rounded-md transition-colors ${
                dateRange === value
                  ? "bg-accent text-foreground font-medium"
                  : "text-muted-foreground hover:text-foreground"
              }`}
            >
              {label}
            </button>
          ))}
        </div>
        {dateRange === "custom" && (
          <div className="flex items-center gap-1.5">
            <Calendar className="h-3.5 w-3.5 text-muted-foreground/50" />
            <input
              type="date"
              value={customFrom}
              onChange={(e) => { setCustomFrom(e.target.value); setPage(1); }}
              className="h-8 px-2 text-[12px] rounded-md border border-border bg-background/50"
            />
            <span className="text-[11px] text-muted-foreground/50">to</span>
            <input
              type="date"
              value={customTo}
              onChange={(e) => { setCustomTo(e.target.value); setPage(1); }}
              className="h-8 px-2 text-[12px] rounded-md border border-border bg-background/50"
            />
          </div>
        )}
      </div>

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
  return <PageContent />;
}
