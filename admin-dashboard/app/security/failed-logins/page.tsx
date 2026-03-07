"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { DataTable } from "@/components/shared/data-table";
import { api } from "@/lib/api";
import { ArrowLeft } from "lucide-react";
import { formatDistanceToNow } from "date-fns";
import type { ColumnDef } from "@tanstack/react-table";

interface FailedLogin {
  ip: string;
  attempt_count: number;
  last_attempt: string;
}

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
];

function FailedLoginsContent() {
  const [entries, setEntries] = useState<FailedLogin[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api.get<FailedLogin[]>("/v1/security/failed-logins").then((res) => {
      if (res.success && res.data) setEntries(res.data);
      setLoading(false);
    });
  }, []);

  return (
    <PageShell title="Failed Logins" description="IP addresses with failed authentication attempts">
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
