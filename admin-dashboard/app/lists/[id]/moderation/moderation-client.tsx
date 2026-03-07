"use client";

import { useEffect, useState } from "react";
import { useParams } from "next/navigation";
import Link from "next/link";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { DataTable } from "@/components/shared/data-table";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { formatDistanceToNow } from "date-fns";
import { ArrowLeft, Check, X, Loader2 } from "lucide-react";
import { toast } from "sonner";
import type { ColumnDef } from "@tanstack/react-table";

interface ModerationItem {
  id: number;
  from: string;
  subject: string;
  received_at: string;
}

function ModerationContent() {
  const params = useParams();
  const listId = params.id as string;

  const [items, setItems] = useState<ModerationItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [actionId, setActionId] = useState<number | null>(null);

  const fetchItems = () => {
    setLoading(true);
    api.get<ModerationItem[]>(`/v1/lists/${listId}/moderation`).then((res) => {
      if (res.success && res.data) setItems(res.data);
      setLoading(false);
    });
  };

  useEffect(() => {
    fetchItems();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [listId]);

  const handleApprove = async (item: ModerationItem) => {
    setActionId(item.id);
    const res = await api.post(
      `/v1/lists/${listId}/moderation/${item.id}/approve`
    );
    if (res.success) {
      toast.success(`Approved message from ${item.from}`);
      setItems((prev) => prev.filter((i) => i.id !== item.id));
    } else {
      toast.error(res.error || "Failed to approve message");
    }
    setActionId(null);
  };

  const handleReject = async (item: ModerationItem) => {
    setActionId(item.id);
    const res = await api.post(
      `/v1/lists/${listId}/moderation/${item.id}/reject`
    );
    if (res.success) {
      toast.success(`Rejected message from ${item.from}`);
      setItems((prev) => prev.filter((i) => i.id !== item.id));
    } else {
      toast.error(res.error || "Failed to reject message");
    }
    setActionId(null);
  };

  const columns: ColumnDef<ModerationItem, unknown>[] = [
    {
      accessorKey: "from",
      header: "From",
      cell: ({ row }) => (
        <span className="font-mono text-[12px]">{row.original.from}</span>
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
      accessorKey: "received_at",
      header: "Received",
      cell: ({ row }) => (
        <span className="text-[12px] text-muted-foreground/60">
          {formatDistanceToNow(new Date(row.original.received_at), {
            addSuffix: true,
          })}
        </span>
      ),
    },
    {
      id: "actions",
      enableSorting: false,
      cell: ({ row }) => (
        <div className="flex items-center gap-1">
          <Button
            variant="ghost"
            size="icon-xs"
            className="text-muted-foreground/50 hover:text-green-600"
            onClick={() => handleApprove(row.original)}
            disabled={actionId === row.original.id}
          >
            {actionId === row.original.id ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <Check className="h-3.5 w-3.5" />
            )}
          </Button>
          <Button
            variant="ghost"
            size="icon-xs"
            className="text-muted-foreground/50 hover:text-destructive"
            onClick={() => handleReject(row.original)}
            disabled={actionId === row.original.id}
          >
            <X className="h-3.5 w-3.5" />
          </Button>
        </div>
      ),
    },
  ];

  return (
    <PageShell
      title="Moderation Queue"
      description="Review and approve pending messages"
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
        data={items}
        loading={loading}
        searchKey="subject"
        searchPlaceholder="Search by subject..."
        emptyMessage="No messages pending moderation."
      />
    </PageShell>
  );
}

export default function ModerationClient() {
  return (
    <AuthGuard>
      <ModerationContent />
    </AuthGuard>
  );
}
