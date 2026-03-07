"use client";

import { useEffect, useState, useCallback } from "react";
import Link from "next/link";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { DataTable } from "@/components/shared/data-table";
import { api } from "@/lib/api";
import type { Domain } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from "@/components/ui/dialog";
import { Plus, Trash2, ExternalLink, Shield, KeyRound, Users } from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";
import type { ColumnDef } from "@tanstack/react-table";

function DomainsContent() {
  const [domains, setDomains] = useState<Domain[]>([]);
  const [loading, setLoading] = useState(true);
  const [deleteTarget, setDeleteTarget] = useState<Domain | null>(null);
  const [deleting, setDeleting] = useState(false);

  const fetchDomains = useCallback(async () => {
    setLoading(true);
    const res = await api.get<Domain[]>("/v1/domains");
    if (res.success && res.data) setDomains(res.data);
    setLoading(false);
  }, []);

  useEffect(() => { fetchDomains(); }, [fetchDomains]);

  const handleDelete = async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    const res = await api.delete(`/v1/domains/${deleteTarget.id}`);
    if (res.success) {
      toast.success(`Domain ${deleteTarget.name} deleted`);
      setDeleteTarget(null);
      fetchDomains();
    } else {
      toast.error(res.error || "Failed to delete domain");
    }
    setDeleting(false);
  };

  const columns: ColumnDef<Domain>[] = [
    {
      accessorKey: "name",
      header: "Domain",
      cell: ({ row }) => (
        <Link href={`/domains/${row.original.id}/`} className="font-medium hover:underline">
          {row.original.name}
        </Link>
      ),
    },
    {
      accessorKey: "mail_hostname",
      header: "Mail Host",
      cell: ({ row }) => <span className="text-muted-foreground font-mono text-[12px]">{row.original.mail_hostname}</span>,
    },
    {
      accessorKey: "is_primary",
      header: "Status",
      enableSorting: false,
      cell: ({ row }) =>
        row.original.is_primary ? (
          <Badge variant="default" className="text-[10px] gap-1"><Shield className="h-3 w-3" />Primary</Badge>
        ) : (
          <Badge variant="secondary" className="text-[10px]">Secondary</Badge>
        ),
    },
    {
      accessorKey: "user_count",
      header: "Users",
      cell: ({ row }) => (
        <span className="flex items-center gap-1 text-muted-foreground tabular-nums">
          <Users className="h-3 w-3" />{row.original.user_count}
        </span>
      ),
    },
    {
      accessorKey: "dkim_enabled",
      header: "DKIM",
      enableSorting: false,
      cell: ({ row }) =>
        row.original.dkim_enabled ? (
          <Badge variant="outline" className="text-[10px] text-emerald-500 border-emerald-500/30 gap-1">
            <KeyRound className="h-3 w-3" />Enabled
          </Badge>
        ) : (
          <Badge variant="outline" className="text-[10px] text-muted-foreground">Off</Badge>
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
        <div className="flex items-center justify-end gap-0.5">
          <Link href={`/domains/${row.original.id}/`}>
            <button className="inline-flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground/50 transition-colors hover:bg-accent hover:text-foreground">
              <ExternalLink className="h-3.5 w-3.5" />
            </button>
          </Link>
          <button
            onClick={() => setDeleteTarget(row.original)}
            disabled={row.original.is_primary}
            className="inline-flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground/50 transition-colors hover:bg-destructive/10 hover:text-destructive disabled:opacity-30 disabled:pointer-events-none"
          >
            <Trash2 className="h-3.5 w-3.5" />
          </button>
        </div>
      ),
    },
  ];

  return (
    <PageShell
      title="Domains"
      description="Manage mail domains and DNS configuration"
      actions={
        <Link href="/domains/new/">
          <Button size="sm" className="h-8 text-[12px] gap-1.5">
            <Plus className="h-3.5 w-3.5" />Add Domain
          </Button>
        </Link>
      }
    >
      <DataTable
        columns={columns}
        data={domains}
        loading={loading}
        searchKey="name"
        searchPlaceholder="Filter domains..."
        emptyMessage="No domains configured."
      />

      <Dialog open={!!deleteTarget} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="text-[15px]">Delete Domain</DialogTitle>
            <DialogDescription className="text-[13px]">
              Delete <strong>{deleteTarget?.name}</strong>?
              {deleteTarget && deleteTarget.user_count > 0 && (
                <span className="block mt-2 text-destructive">
                  This domain has {deleteTarget.user_count} user{deleteTarget.user_count !== 1 ? "s" : ""}. Delete or migrate them first.
                </span>
              )}
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
  return <AuthGuard><DomainsContent /></AuthGuard>;
}
