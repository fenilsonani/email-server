"use client";

import { useEffect, useState, useCallback } from "react";
import Link from "next/link";
import { AuthGuard } from "@/components/layout/auth-guard";
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

interface ScreenerRule {
  id: number;
  sender: string;
  action: "allow" | "block";
  created_at: string;
}

function PageContent() {
  const [rules, setRules] = useState<ScreenerRule[]>([]);
  const [loading, setLoading] = useState(true);
  const [showAdd, setShowAdd] = useState(false);
  const [sender, setSender] = useState("");
  const [action, setAction] = useState<"allow" | "block">("block");
  const [submitting, setSubmitting] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<ScreenerRule | null>(null);
  const [deleting, setDeleting] = useState(false);

  const fetchRules = useCallback(async () => {
    setLoading(true);
    const res = await api.get<ScreenerRule[]>("/v1/features/screener");
    if (res.success && res.data) setRules(res.data);
    setLoading(false);
  }, []);

  useEffect(() => { fetchRules(); }, [fetchRules]);

  const handleAdd = async () => {
    if (!sender.trim()) return;
    setSubmitting(true);
    const res = await api.post("/v1/features/screener", { sender: sender.trim(), action });
    if (res.success) {
      toast.success("Screener rule added");
      setSender("");
      setAction("block");
      setShowAdd(false);
      fetchRules();
    } else {
      toast.error(res.error || "Failed to add rule");
    }
    setSubmitting(false);
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    const res = await api.delete(`/v1/features/screener/${deleteTarget.id}`);
    if (res.success) {
      toast.success("Screener rule removed");
      setDeleteTarget(null);
      fetchRules();
    } else {
      toast.error(res.error || "Failed to delete rule");
    }
    setDeleting(false);
  };

  const columns: ColumnDef<ScreenerRule>[] = [
    {
      accessorKey: "sender",
      header: "Sender",
      cell: ({ row }) => (
        <span className="font-mono text-[12px]">{row.original.sender}</span>
      ),
    },
    {
      accessorKey: "action",
      header: "Action",
      cell: ({ row }) => (
        <Badge
          variant={row.original.action === "allow" ? "default" : "outline"}
          className="text-[10px]"
        >
          {row.original.action}
        </Badge>
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
            <h1 className="text-[15px] font-semibold tracking-tight">Screener</h1>
            <p className="text-[12px] text-muted-foreground/70 mt-0.5">Allow or block senders</p>
          </div>
        </div>
      }
      actions={
        <Button size="sm" className="h-8 text-[12px] gap-1.5" onClick={() => setShowAdd(true)}>
          <Plus className="h-3.5 w-3.5" />
          Add Rule
        </Button>
      }
    >
      <DataTable
        columns={columns}
        data={rules}
        loading={loading}
        searchKey="sender"
        searchPlaceholder="Filter by sender..."
        emptyMessage="No screener rules."
      />

      <Dialog open={showAdd} onOpenChange={(open) => !open && setShowAdd(false)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="text-[15px]">Add Screener Rule</DialogTitle>
            <DialogDescription className="text-[13px]">
              Allow or block emails from a specific sender address.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3 py-2">
            <div className="space-y-1.5">
              <label className="text-[12px] font-medium">Sender Email</label>
              <Input
                value={sender}
                onChange={(e) => setSender(e.target.value)}
                placeholder="sender@example.com"
                className="h-8 text-[12px] font-mono"
              />
            </div>
            <div className="space-y-1.5">
              <label className="text-[12px] font-medium">Action</label>
              <select
                value={action}
                onChange={(e) => setAction(e.target.value as "allow" | "block")}
                className="h-8 w-full rounded-lg border border-border bg-background/50 px-2.5 text-[12px] text-foreground outline-none focus-visible:border-ring"
              >
                <option value="block">Block</option>
                <option value="allow">Allow</option>
              </select>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" size="sm" className="text-[12px]" onClick={() => setShowAdd(false)}>Cancel</Button>
            <Button size="sm" className="text-[12px]" onClick={handleAdd} disabled={submitting || !sender.trim()}>
              {submitting ? "Adding..." : "Add Rule"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={!!deleteTarget} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="text-[15px]">Delete Rule</DialogTitle>
            <DialogDescription className="text-[13px]">
              Are you sure you want to delete the rule for <strong className="font-mono">{deleteTarget?.sender}</strong>?
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
  return <AuthGuard><PageContent /></AuthGuard>;
}
