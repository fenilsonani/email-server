"use client";

import { useEffect, useState, useCallback } from "react";
import Link from "next/link";
import { PageShell } from "@/components/shared/page-shell";
import { DataTable } from "@/components/shared/data-table";
import { api } from "@/lib/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from "@/components/ui/dialog";
import { ArrowLeft, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";
import type { ColumnDef } from "@tanstack/react-table";

interface Alias {
  id: number;
  alias_address: string;
  alias_local: string;
  description: string;
  is_active: boolean;
  created_at: string;
  last_used_at: string;
  email_count: number;
}

function PageContent() {
  const [aliases, setAliases] = useState<Alias[]>([]);
  const [loading, setLoading] = useState(true);
  const [showAdd, setShowAdd] = useState(false);
  const [alias, setAlias] = useState("");
  const [description, setDescription] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<Alias | null>(null);
  const [deleting, setDeleting] = useState(false);

  const fetchAliases = useCallback(async () => {
    setLoading(true);
    const res = await api.get<Alias[]>("/v1/features/aliases");
    if (res.success && Array.isArray(res.data)) setAliases(res.data);
    setLoading(false);
  }, []);

  useEffect(() => { fetchAliases(); }, [fetchAliases]);

  const handleAdd = async () => {
    if (!alias.trim()) return;
    setSubmitting(true);
    const res = await api.post("/v1/features/aliases", { alias: alias.trim(), forwards_to: description.trim() });
    if (res.success) {
      toast.success("Alias created");
      setAlias("");
      setDescription("");
      setShowAdd(false);
      fetchAliases();
    } else {
      toast.error(res.error || "Failed to create alias");
    }
    setSubmitting(false);
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    const res = await api.delete(`/v1/features/aliases/${deleteTarget.id}`);
    if (res.success) {
      toast.success("Alias deleted");
      setDeleteTarget(null);
      fetchAliases();
    } else {
      toast.error(res.error || "Failed to delete alias");
    }
    setDeleting(false);
  };

  const columns: ColumnDef<Alias>[] = [
    {
      accessorKey: "alias_address",
      header: "Alias",
      cell: ({ row }) => (
        <span className="font-mono text-[12px]">{row.original.alias_address}</span>
      ),
    },
    {
      accessorKey: "description",
      header: "Description",
      cell: ({ row }) => (
        <span className="font-mono text-[12px] text-muted-foreground">{row.original.description || "—"}</span>
      ),
    },
    {
      accessorKey: "is_active",
      header: "Status",
      cell: ({ row }) => (
        <Badge
          variant={row.original.is_active ? "default" : "outline"}
          className="text-[10px]"
        >
          {row.original.is_active ? "Active" : "Inactive"}
        </Badge>
      ),
    },
    {
      accessorKey: "email_count",
      header: "Emails",
      cell: ({ row }) => (
        <span className="text-muted-foreground tabular-nums text-[12px]">
          {row.original.email_count}
        </span>
      ),
    },
    {
      accessorKey: "created_at",
      header: "Created",
      cell: ({ row }) => (
        <span className="text-muted-foreground tabular-nums">
          {formatDistanceToNow(new Date(row.original.created_at), { addSuffix: true })}
        </span>
      ),
    },
    {
      id: "actions",
      header: "",
      enableSorting: false,
      cell: ({ row }) => (
        <div className="flex items-center justify-end">
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
      title={
        <div className="flex items-center gap-3">
          <Link href="/features/">
            <Button variant="ghost" size="icon-xs">
              <ArrowLeft className="h-4 w-4" />
            </Button>
          </Link>
          <div>
            <h1 className="text-[15px] font-semibold tracking-tight">Aliases</h1>
            <p className="text-[12px] text-muted-foreground/70 mt-0.5">Manage email address aliases</p>
          </div>
        </div>
      }
      actions={
        <Button size="sm" className="h-8 text-[12px] gap-1.5" onClick={() => setShowAdd(true)}>
          <Plus className="h-3.5 w-3.5" />
          Add Alias
        </Button>
      }
    >
      <DataTable
        columns={columns}
        data={aliases}
        loading={loading}
        searchKey="alias_address"
        searchPlaceholder="Filter by alias..."
        emptyMessage="No aliases configured."
      />

      <Dialog open={showAdd} onOpenChange={(open) => !open && setShowAdd(false)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="text-[15px]">Add Alias</DialogTitle>
            <DialogDescription className="text-[13px]">
              Create a new email alias that forwards to another address.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3 py-2">
            <div className="space-y-1.5">
              <label className="text-[12px] font-medium">Alias Address</label>
              <Input
                value={alias}
                onChange={(e) => setAlias(e.target.value)}
                placeholder="alias@example.com"
                className="h-8 text-[12px] font-mono"
              />
            </div>
            <div className="space-y-1.5">
              <label className="text-[12px] font-medium">Description</label>
              <Input
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="e.g. Shopping, Newsletters"
                className="h-8 text-[12px]"
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" size="sm" className="text-[12px]" onClick={() => setShowAdd(false)}>Cancel</Button>
            <Button size="sm" className="text-[12px]" onClick={handleAdd} disabled={submitting || !alias.trim()}>
              {submitting ? "Creating..." : "Create Alias"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={!!deleteTarget} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="text-[15px]">Delete Alias</DialogTitle>
            <DialogDescription className="text-[13px]">
              Are you sure you want to delete <strong className="font-mono">{deleteTarget?.alias_address}</strong>?
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" size="sm" className="text-[12px]" onClick={() => setDeleteTarget(null)}>Cancel</Button>
            <Button variant="destructive" size="sm" className="text-[12px]" onClick={handleDelete} disabled={deleting}>
              {deleting ? "Deleting..." : "Delete"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </PageShell>
  );
}

export default function Page() {
  return <PageContent />;
}
