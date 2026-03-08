"use client";

import { useEffect, useState, useCallback } from "react";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { DataTable } from "@/components/shared/data-table";
import { api } from "@/lib/api";
import type { AuthLog } from "@/lib/types";
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
  const [dateRange, setDateRange] = useState<DateRange>("all");
  const [customFrom, setCustomFrom] = useState("");
  const [customTo, setCustomTo] = useState("");

  const fetchLogs = useCallback(async () => {
    setLoading(true);
    const dateParams = getDateRange(dateRange, customFrom, customTo);
    const res = await api.get<AuthLog[]>("/v1/logs/auth", { page: String(page), ...dateParams });
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
