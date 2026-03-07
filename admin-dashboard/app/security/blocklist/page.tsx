"use client";

import { useEffect, useState, useCallback } from "react";
import Link from "next/link";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { DataTable } from "@/components/shared/data-table";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from "@/components/ui/dialog";
import { api } from "@/lib/api";
import { Plus, Trash2, ArrowLeft } from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";
import type { ColumnDef } from "@tanstack/react-table";

interface SuppressionEntry {
  id: number;
  email: string;
  reason: string;
  domain: string;
  created_at: string;
}

const reasonColors: Record<string, string> = {
  hard_bounce: "destructive",
  complaint: "destructive",
  unsubscribe: "secondary",
  manual: "outline",
};

const columns: ColumnDef<SuppressionEntry>[] = [
  {
    accessorKey: "email",
    header: "Email",
    cell: ({ row }) => <span className="font-medium">{row.original.email}</span>,
  },
  {
    accessorKey: "reason",
    header: "Reason",
    cell: ({ row }) => (
      <Badge variant={(reasonColors[row.original.reason] as "destructive" | "secondary" | "outline") || "secondary"} className="text-[10px]">
        {row.original.reason.replace("_", " ")}
      </Badge>
    ),
  },
  {
    accessorKey: "domain",
    header: "Domain",
    cell: ({ row }) => <span className="text-muted-foreground">{row.original.domain}</span>,
  },
  {
    accessorKey: "created_at",
    header: "Added",
    cell: ({ row }) => (
      <span className="text-muted-foreground tabular-nums">
        {formatDistanceToNow(new Date(row.original.created_at), { addSuffix: true })}
      </span>
    ),
  },
];

function BlocklistContent() {
  const [entries, setEntries] = useState<SuppressionEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [page, setPage] = useState(1);
  const [totalPages, setTotalPages] = useState(1);
  const [totalCount, setTotalCount] = useState(0);
  const [showAdd, setShowAdd] = useState(false);
  const [addEmail, setAddEmail] = useState("");
  const [addReason, setAddReason] = useState("manual");
  const [adding, setAdding] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<SuppressionEntry | null>(null);
  const [deleting, setDeleting] = useState(false);

  const fetchEntries = useCallback(async () => {
    setLoading(true);
    const res = await api.get<SuppressionEntry[]>("/v1/security/suppression", {
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

  const handleAdd = async () => {
    if (!addEmail) return;
    setAdding(true);
    const res = await api.post("/v1/security/suppression", { email: addEmail, reason: addReason });
    if (res.success) {
      toast.success(`Added ${addEmail} to blocklist`);
      setShowAdd(false);
      setAddEmail("");
      setAddReason("manual");
      fetchEntries();
    } else {
      toast.error(res.error || "Failed to add entry");
    }
    setAdding(false);
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    const res = await api.delete(`/v1/security/suppression/${deleteTarget.id}`);
    if (res.success) {
      toast.success(`Removed ${deleteTarget.email} from blocklist`);
      setDeleteTarget(null);
      fetchEntries();
    } else {
      toast.error(res.error || "Failed to remove entry");
    }
    setDeleting(false);
  };

  const columnsWithActions: ColumnDef<SuppressionEntry>[] = [
    ...columns,
    {
      id: "actions",
      header: "",
      enableSorting: false,
      cell: ({ row }) => (
        <div className="flex justify-end">
          <button
            onClick={() => setDeleteTarget(row.original)}
            className="inline-flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground/50 transition-colors hover:bg-destructive/10 hover:text-destructive"
          >
            <Trash2 className="h-3.5 w-3.5" />
          </button>
        </div>
      ),
    },
  ];

  return (
    <PageShell
      title="Blocklist"
      description="Suppressed email addresses"
      actions={
        <Button size="sm" className="h-8 text-[12px] gap-1.5" onClick={() => setShowAdd(true)}>
          <Plus className="h-3.5 w-3.5" />
          Add Entry
        </Button>
      }
    >
      <Link href="/security/" className="inline-flex items-center gap-1.5 text-[12px] text-muted-foreground/60 hover:text-foreground transition-colors mb-2">
        <ArrowLeft className="h-3.5 w-3.5" />Back to Security
      </Link>

      <DataTable
        columns={columnsWithActions}
        data={entries}
        loading={loading}
        searchKey="email"
        searchPlaceholder="Filter by email..."
        emptyMessage="No blocked emails."
        serverPagination={{ page, totalPages, totalCount, onPageChange: setPage }}
      />

      {/* Add Dialog */}
      <Dialog open={showAdd} onOpenChange={setShowAdd}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="text-[15px]">Add to Blocklist</DialogTitle>
            <DialogDescription className="text-[13px]">
              Block an email address from sending or receiving mail.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3 py-2">
            <div className="space-y-1.5">
              <Label className="text-[13px]">Email Address</Label>
              <Input
                value={addEmail}
                onChange={(e) => setAddEmail(e.target.value)}
                placeholder="spam@example.com"
                className="text-[13px]"
              />
            </div>
            <div className="space-y-1.5">
              <Label className="text-[13px]">Reason</Label>
              <select
                value={addReason}
                onChange={(e) => setAddReason(e.target.value)}
                className="h-8 w-full rounded-lg border border-border bg-background/50 px-2.5 text-[13px] text-foreground outline-none focus-visible:border-ring"
              >
                <option value="manual">Manual</option>
                <option value="hard_bounce">Hard Bounce</option>
                <option value="complaint">Complaint</option>
                <option value="unsubscribe">Unsubscribe</option>
              </select>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" size="sm" className="text-[12px]" onClick={() => setShowAdd(false)}>Cancel</Button>
            <Button size="sm" className="text-[12px]" onClick={handleAdd} disabled={adding || !addEmail}>
              {adding ? "Adding..." : "Add"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete Dialog */}
      <Dialog open={!!deleteTarget} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="text-[15px]">Remove from Blocklist</DialogTitle>
            <DialogDescription className="text-[13px]">
              Remove <strong>{deleteTarget?.email}</strong> from the blocklist?
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" size="sm" className="text-[12px]" onClick={() => setDeleteTarget(null)}>Cancel</Button>
            <Button variant="destructive" size="sm" className="text-[12px]" onClick={handleDelete} disabled={deleting}>
              {deleting ? "Removing..." : "Remove"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </PageShell>
  );
}

export default function Page() {
  return <AuthGuard><BlocklistContent /></AuthGuard>;
}
