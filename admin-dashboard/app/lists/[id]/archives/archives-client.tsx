"use client";

import { useEffect, useState } from "react";
import { useRouteId } from "@/lib/use-route-id";
import Link from "next/link";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { DataTable } from "@/components/shared/data-table";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { formatDistanceToNow } from "date-fns";
import { ArrowLeft } from "lucide-react";
import type { ColumnDef } from "@tanstack/react-table";

interface ArchiveMessage {
  id: number;
  sender_email: string;
  sender_name: string;
  subject: string;
  sent_at: string;
  body_preview: string;
}

const columns: ColumnDef<ArchiveMessage, unknown>[] = [
  {
    accessorKey: "sender_email",
    header: "From",
    cell: ({ row }) => (
      <span className="font-mono text-[12px]">
        {row.original.sender_name ? `${row.original.sender_name} <${row.original.sender_email}>` : row.original.sender_email}
      </span>
    ),
  },
  {
    accessorKey: "subject",
    header: "Subject",
    cell: ({ row }) => (
      <span className="text-[13px]">{row.original.subject}</span>
    ),
  },
  {
    accessorKey: "sent_at",
    header: "Sent",
    cell: ({ row }) => (
      <span className="text-[12px] text-muted-foreground/60">
        {formatDistanceToNow(new Date(row.original.sent_at), {
          addSuffix: true,
        })}
      </span>
    ),
  },
];

function ArchivesContent() {
  const listId = useRouteId("lists");

  const [messages, setMessages] = useState<ArchiveMessage[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api.get<ArchiveMessage[]>(`/v1/lists/${listId}/archives`).then((res) => {
      if (res.success && res.data) setMessages(res.data);
      setLoading(false);
    });
  }, [listId]);

  return (
    <PageShell
      title="Archives"
      description="Browse archived messages for this list"
      actions={
        <Link href={`/lists/${listId}/`}>
          <Button variant="ghost" size="icon-xs">
            <ArrowLeft className="h-4 w-4" />
          </Button>
        </Link>
      }
    >
      <DataTable
        columns={columns}
        data={messages}
        loading={loading}
        searchKey="subject"
        searchPlaceholder="Search archives..."
        emptyMessage="No archived messages found."
      />
    </PageShell>
  );
}

export default function ArchivesClient() {
  return (
    <AuthGuard>
      <ArchivesContent />
    </AuthGuard>
  );
}
