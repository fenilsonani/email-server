"use client";

import { useEffect, useState, useCallback } from "react";
import Link from "next/link";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { DataTable } from "@/components/shared/data-table";
import { api } from "@/lib/api";
import type { User } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from "@/components/ui/dialog";
import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import { Plus, Pencil, Trash2, Shield } from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";
import type { ColumnDef } from "@tanstack/react-table";

interface DomainOption { id: number; name: string }

const columns: ColumnDef<User>[] = [
  {
    accessorKey: "email",
    header: "Email",
    cell: ({ row }) => <span className="font-medium">{row.original.email}</span>,
  },
  {
    accessorKey: "domain",
    header: "Domain",
    cell: ({ row }) => <span className="text-muted-foreground">{row.original.domain}</span>,
  },
  {
    accessorKey: "is_admin",
    header: "Role",
    enableSorting: false,
    cell: ({ row }) =>
      row.original.is_admin ? (
        <Badge variant="default" className="text-[10px] gap-1"><Shield className="h-3 w-3" />Admin</Badge>
      ) : (
        <Badge variant="secondary" className="text-[10px]">User</Badge>
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
];

function UsersContent() {
  const [users, setUsers] = useState<User[]>([]);
  const [loading, setLoading] = useState(true);
  const [page, setPage] = useState(1);
  const [totalPages, setTotalPages] = useState(1);
  const [totalCount, setTotalCount] = useState(0);
  const [domainFilter, setDomainFilter] = useState("");
  const [adminOnly, setAdminOnly] = useState(false);
  const [domains, setDomains] = useState<DomainOption[]>([]);
  const [deleteTarget, setDeleteTarget] = useState<User | null>(null);
  const [deleting, setDeleting] = useState(false);

  const fetchUsers = useCallback(async () => {
    setLoading(true);
    const params: Record<string, string> = { page: String(page), page_size: "20" };
    if (domainFilter) params.domain = domainFilter;
    if (adminOnly) params.is_admin = "true";
    const res = await api.get<User[]>("/v1/users", params);
    if (res.success && res.data) {
      setUsers(res.data);
      if (res.meta) {
        setTotalPages(res.meta.total_pages);
        setTotalCount(res.meta.total_count);
      }
    }
    setLoading(false);
  }, [page, domainFilter, adminOnly]);

  useEffect(() => { fetchUsers(); }, [fetchUsers]);

  useEffect(() => {
    api.get<DomainOption[]>("/v1/domains-list").then((res) => {
      if (res.success && res.data) setDomains(res.data);
    });
  }, []);

  const handleDelete = async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    const res = await api.delete(`/v1/users/${deleteTarget.id}`);
    if (res.success) {
      toast.success(`User ${deleteTarget.email} deleted`);
      setDeleteTarget(null);
      fetchUsers();
    } else {
      toast.error(res.error || "Failed to delete user");
    }
    setDeleting(false);
  };

  const columnsWithActions: ColumnDef<User>[] = [
    ...columns,
    {
      id: "actions",
      header: "",
      enableSorting: false,
      cell: ({ row }) => (
        <div className="flex items-center justify-end gap-0.5">
          <Link href={`/users/${row.original.id}/`}>
            <button className="inline-flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground/50 transition-colors hover:bg-accent hover:text-foreground">
              <Pencil className="h-3.5 w-3.5" />
            </button>
          </Link>
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
      title="Users"
      description="Manage email accounts across your domains"
      actions={
        <Link href="/users/new/">
          <Button size="sm" className="h-8 text-[12px] gap-1.5">
            <Plus className="h-3.5 w-3.5" />
            Add User
          </Button>
        </Link>
      }
    >
      <DataTable
        columns={columnsWithActions}
        data={users}
        loading={loading}
        searchKey="email"
        searchPlaceholder="Filter by email..."
        emptyMessage="No users found."
        serverPagination={{ page, totalPages, totalCount, onPageChange: setPage }}
        toolbar={
          <>
            <select
              value={domainFilter}
              onChange={(e) => { setDomainFilter(e.target.value); setPage(1); }}
              className="h-8 rounded-lg border border-border bg-background/50 px-2.5 text-[12px] text-foreground outline-none focus-visible:border-ring"
            >
              <option value="">All domains</option>
              {domains.map((d) => <option key={d.id} value={d.name}>{d.name}</option>)}
            </select>
            <div className="flex items-center gap-1.5">
              <Switch size="sm" checked={adminOnly} onCheckedChange={(v) => { setAdminOnly(v); setPage(1); }} />
              <Label className="text-[12px] text-muted-foreground/70 cursor-pointer">Admins</Label>
            </div>
          </>
        }
      />

      <Dialog open={!!deleteTarget} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="text-[15px]">Delete User</DialogTitle>
            <DialogDescription className="text-[13px]">
              Are you sure you want to delete <strong>{deleteTarget?.email}</strong>? This cannot be undone.
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
  return <AuthGuard><UsersContent /></AuthGuard>;
}
