"use client";

import { useEffect, useState, useCallback } from "react";
import Link from "next/link";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { DataTable } from "@/components/shared/data-table";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from "@/components/ui/dialog";
import { ArrowLeft, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";
import type { ColumnDef } from "@tanstack/react-table";

interface VipSender {
  id: number;
  email: string;
  name: string;
  created_at: string;
}

function PageContent() {
  const [vips, setVips] = useState<VipSender[]>([]);
  const [loading, setLoading] = useState(true);
  const [showAdd, setShowAdd] = useState(false);
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<VipSender | null>(null);
  const [deleting, setDeleting] = useState(false);

  const fetchVips = useCallback(async () => {
    setLoading(true);
    const res = await api.get<VipSender[]>("/v1/features/vip");
    if (res.success && res.data) setVips(res.data);
    setLoading(false);
  }, []);

  useEffect(() => { fetchVips(); }, [fetchVips]);

  const handleAdd = async () => {
    if (!email.trim()) return;
    setSubmitting(true);
    const res = await api.post("/v1/features/vip", { sender: email.trim(), label: name.trim() });
    if (res.success) {
      toast.success("VIP sender added");
      setEmail("");
      setName("");
      setShowAdd(false);
      fetchVips();
    } else {
      toast.error(res.error || "Failed to add VIP sender");
    }
    setSubmitting(false);
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    const res = await api.delete(`/v1/features/vip/${deleteTarget.id}`);
    if (res.success) {
      toast.success("VIP sender removed");
      setDeleteTarget(null);
      fetchVips();
    } else {
      toast.error(res.error || "Failed to delete VIP sender");
    }
    setDeleting(false);
  };

  const columns: ColumnDef<VipSender>[] = [
    {
      accessorKey: "email",
      header: "Sender",
      cell: ({ row }) => (
        <span className="font-mono text-[12px]">{row.original.email}</span>
      ),
    },
    {
      accessorKey: "name",
      header: "Name",
      cell: ({ row }) => (
        <span className="text-[13px]">{row.original.name || "—"}</span>
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
            <h1 className="text-[15px] font-semibold tracking-tight">VIP Senders</h1>
            <p className="text-[12px] text-muted-foreground/70 mt-0.5">Priority sender rules</p>
          </div>
        </div>
      }
      actions={
        <Button size="sm" className="h-8 text-[12px] gap-1.5" onClick={() => setShowAdd(true)}>
          <Plus className="h-3.5 w-3.5" />
          Add VIP
        </Button>
      }
    >
      <DataTable
        columns={columns}
        data={vips}
        loading={loading}
        searchKey="email"
        searchPlaceholder="Filter by sender..."
        emptyMessage="No VIP senders."
      />

      <Dialog open={showAdd} onOpenChange={(open) => !open && setShowAdd(false)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="text-[15px]">Add VIP Sender</DialogTitle>
            <DialogDescription className="text-[13px]">
              Mark a sender as VIP to prioritize their emails.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3 py-2">
            <div className="space-y-1.5">
              <label className="text-[12px] font-medium">Sender Email</label>
              <Input
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder="sender@example.com"
                className="h-8 text-[12px] font-mono"
              />
            </div>
            <div className="space-y-1.5">
              <label className="text-[12px] font-medium">Name</label>
              <Input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g. CEO, Important Client"
                className="h-8 text-[12px]"
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" size="sm" className="text-[12px]" onClick={() => setShowAdd(false)}>Cancel</Button>
            <Button size="sm" className="text-[12px]" onClick={handleAdd} disabled={submitting || !email.trim()}>
              {submitting ? "Adding..." : "Add VIP"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={!!deleteTarget} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="text-[15px]">Remove VIP</DialogTitle>
            <DialogDescription className="text-[13px]">
              Are you sure you want to remove <strong className="font-mono">{deleteTarget?.email}</strong> from VIP?
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
  return <AuthGuard><PageContent /></AuthGuard>;
}
