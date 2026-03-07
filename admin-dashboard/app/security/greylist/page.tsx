"use client";

import { useEffect, useState, useCallback } from "react";
import Link from "next/link";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { DataTable } from "@/components/shared/data-table";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { api } from "@/lib/api";
import { ArrowLeft, Check } from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";
import type { ColumnDef } from "@tanstack/react-table";

interface GreylistEntry {
  id: number;
  sender_ip: string;
  sender: string;
  recipient: string;
  first_seen: string;
  passed: boolean;
  pass_count: number;
  last_seen: string;
}

function GreylistContent() {
  const [entries, setEntries] = useState<GreylistEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [page, setPage] = useState(1);
  const [totalPages, setTotalPages] = useState(1);
  const [totalCount, setTotalCount] = useState(0);

  const fetchEntries = useCallback(async () => {
    setLoading(true);
    const res = await api.get<GreylistEntry[]>("/v1/security/greylist", {
      page: String(page), page_size: "20",
    });
    if (res.success && res.data) {
      setEntries(res.data);
      if (res.meta) {
        setTotalPages(res.meta.total_pages);
        setTotalCount(res.meta.total_count);
      }
    }
    setLoading(false);
  }, [page]);

  useEffect(() => { fetchEntries(); }, [fetchEntries]);

  const handleWhitelist = async (entry: GreylistEntry) => {
    const res = await api.post(`/v1/security/greylist/${entry.id}/whitelist`);
    if (res.success) {
      toast.success(`Whitelisted ${entry.sender}`);
      fetchEntries();
    } else {
      toast.error(res.error || "Failed to whitelist");
    }
  };

  const columns: ColumnDef<GreylistEntry>[] = [
    {
      accessorKey: "sender",
      header: "Sender",
      cell: ({ row }) => <span className="font-medium text-[13px]">{row.original.sender}</span>,
    },
    {
      accessorKey: "recipient",
      header: "Recipient",
      cell: ({ row }) => <span className="text-muted-foreground">{row.original.recipient}</span>,
    },
    {
      accessorKey: "sender_ip",
      header: "IP",
      cell: ({ row }) => <span className="text-muted-foreground tabular-nums">{row.original.sender_ip}</span>,
    },
    {
      accessorKey: "passed",
      header: "Status",
      cell: ({ row }) =>
        row.original.passed ? (
          <Badge variant="default" className="text-[10px]">Passed</Badge>
        ) : (
          <Badge variant="secondary" className="text-[10px]">Pending</Badge>
        ),
    },
    {
      accessorKey: "pass_count",
      header: "Passes",
      cell: ({ row }) => <span className="text-muted-foreground tabular-nums">{row.original.pass_count}</span>,
    },
    {
      accessorKey: "first_seen",
      header: "First Seen",
      cell: ({ row }) => (
        <span className="text-muted-foreground tabular-nums">
          {formatDistanceToNow(new Date(row.original.first_seen), { addSuffix: true })}
        </span>
      ),
    },
    {
      id: "actions",
      header: "",
      enableSorting: false,
      cell: ({ row }) =>
        !row.original.passed ? (
          <div className="flex justify-end">
            <Button
              variant="outline"
              size="sm"
              className="h-7 text-[11px] gap-1"
              onClick={() => handleWhitelist(row.original)}
            >
              <Check className="h-3 w-3" />
              Whitelist
            </Button>
          </div>
        ) : null,
    },
  ];

  return (
    <PageShell title="Greylist" description="Greylisted senders awaiting verification">
      <Link href="/security/" className="inline-flex items-center gap-1.5 text-[12px] text-muted-foreground/60 hover:text-foreground transition-colors mb-2">
        <ArrowLeft className="h-3.5 w-3.5" />Back to Security
      </Link>

      <DataTable
        columns={columns}
        data={entries}
        loading={loading}
        searchKey="sender"
        searchPlaceholder="Filter by sender..."
        emptyMessage="No greylist entries."
        serverPagination={{ page, totalPages, totalCount, onPageChange: setPage }}
      />
    </PageShell>
  );
}

export default function Page() {
  return <AuthGuard><GreylistContent /></AuthGuard>;
}
