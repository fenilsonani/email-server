"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { DataTable } from "@/components/shared/data-table";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { ArrowLeft, ShieldBan, Copy, Check, FileDown } from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";
import type { ColumnDef } from "@tanstack/react-table";

interface FailedLogin {
  ip: string;
  attempt_count: number;
  last_attempt: string;
}

function FailedLoginsContent() {
  const [entries, setEntries] = useState<FailedLogin[]>([]);
  const [loading, setLoading] = useState(true);
  const [copiedIp, setCopiedIp] = useState<string | null>(null);

  useEffect(() => {
    api.get<FailedLogin[]>("/v1/security/failed-logins").then((res) => {
      if (res.success && res.data) setEntries(res.data);
      setLoading(false);
    });
  }, []);

  const blockIp = async (ip: string) => {
    const res = await api.post("/v1/security/suppression", { email: ip, reason: "manual" });
    if (res.success) toast.success(`Added ${ip} to blocklist`);
    else toast.error(res.error || "Failed to block IP");
  };

  const copyIp = (ip: string) => {
    navigator.clipboard.writeText(ip);
    setCopiedIp(ip);
    toast.success("IP copied");
    setTimeout(() => setCopiedIp(null), 2000);
  };

  const columns: ColumnDef<FailedLogin>[] = [
    {
      accessorKey: "ip",
      header: "IP Address",
      cell: ({ row }) => <span className="font-medium tabular-nums">{row.original.ip}</span>,
    },
    {
      accessorKey: "attempt_count",
      header: "Attempts",
      cell: ({ row }) => (
        <span className={`tabular-nums font-medium ${row.original.attempt_count > 10 ? "text-destructive" : row.original.attempt_count > 5 ? "text-amber-500" : "text-muted-foreground"}`}>
          {row.original.attempt_count}
        </span>
      ),
    },
    {
      accessorKey: "last_attempt",
      header: "Last Attempt",
      cell: ({ row }) => (
        <span className="text-muted-foreground tabular-nums">
          {formatDistanceToNow(new Date(row.original.last_attempt), { addSuffix: true })}
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
            onClick={() => copyIp(row.original.ip)}
            title="Copy IP"
            className="inline-flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground/50 transition-colors hover:bg-accent hover:text-foreground"
          >
            {copiedIp === row.original.ip ? <Check className="h-3.5 w-3.5 text-green-500" /> : <Copy className="h-3.5 w-3.5" />}
          </button>
          <button
            onClick={() => blockIp(row.original.ip)}
            title="Add to blocklist"
            className="inline-flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground/50 transition-colors hover:bg-destructive/10 hover:text-destructive"
          >
            <ShieldBan className="h-3.5 w-3.5" />
          </button>
        </div>
      ),
    },
  ];

  return (
    <PageShell title="Failed Logins" description="IP addresses with failed authentication attempts"
      actions={
        <Button variant="outline" size="sm" className="h-8 text-[12px] gap-1.5" onClick={() => {
          const csv = ["IP,Attempts,Last Attempt"];
          entries.forEach(e => csv.push(`${e.ip},${e.attempt_count},${e.last_attempt}`));
          const blob = new Blob([csv.join("\n")], { type: "text/csv" });
          const a = document.createElement("a"); a.href = URL.createObjectURL(blob); a.download = "failed-logins.csv"; a.click(); URL.revokeObjectURL(a.href);
          toast.success("Failed logins exported");
        }}>
          <FileDown className="h-3.5 w-3.5" />Export
        </Button>
      }
    >
      <Link href="/security/" className="inline-flex items-center gap-1.5 text-[12px] text-muted-foreground/60 hover:text-foreground transition-colors mb-2">
        <ArrowLeft className="h-3.5 w-3.5" />Back to Security
      </Link>

      <DataTable
        columns={columns}
        data={entries}
        loading={loading}
        searchKey="ip"
        searchPlaceholder="Filter by IP..."
        emptyMessage="No failed login attempts."
      />
    </PageShell>
  );
}

export default function Page() {
  return <AuthGuard><FailedLoginsContent /></AuthGuard>;
}
