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
import { Plus, Trash2, ExternalLink, Shield, Users, CheckCircle2, XCircle, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";
import type { ColumnDef } from "@tanstack/react-table";

function DomainsContent() {
  const [domains, setDomains] = useState<Domain[]>([]);
  const [loading, setLoading] = useState(true);
  const [deleteTarget, setDeleteTarget] = useState<Domain | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [dnsStatus, setDnsStatus] = useState<Record<number, { spf: string; dkim: string; dmarc: string; mx: string } | "loading">>({});

  const fetchDomains = useCallback(async () => {
    setLoading(true);
    const res = await api.get<Domain[]>("/v1/domains");
    if (res.success && res.data) setDomains(res.data);
    setLoading(false);
  }, []);

  useEffect(() => { fetchDomains(); }, [fetchDomains]);

  // Fetch DNS status for each domain
  useEffect(() => {
    domains.forEach((d) => {
      if (dnsStatus[d.id]) return;
      setDnsStatus(prev => ({ ...prev, [d.id]: "loading" }));
      api.get<{ type: string; status: string }[]>(`/v1/domains/${d.id}/dns`).then((res) => {
        if (res.success && res.data && Array.isArray(res.data)) {
          const records = res.data;
          const get = (type: string) => records.find(r => r.type.toLowerCase() === type.toLowerCase())?.status || "missing";
          setDnsStatus(prev => ({ ...prev, [d.id]: { spf: get("TXT (SPF)"), dkim: get("TXT (DKIM)"), dmarc: get("TXT (DMARC)"), mx: get("MX") } }));
        } else {
          setDnsStatus(prev => ({ ...prev, [d.id]: { spf: "missing", dkim: "missing", dmarc: "missing", mx: "missing" } }));
        }
      }).catch(() => {
        setDnsStatus(prev => ({ ...prev, [d.id]: { spf: "missing", dkim: "missing", dmarc: "missing", mx: "missing" } }));
      });
    });
  }, [domains]); // eslint-disable-line react-hooks/exhaustive-deps

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
      id: "dns_status",
      header: "DNS",
      enableSorting: false,
      cell: ({ row }) => {
        const status = dnsStatus[row.original.id];
        if (!status || status === "loading") {
          return <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground/30" />;
        }
        const DnsBadge = ({ label, ok }: { label: string; ok: boolean }) => (
          <span className={`inline-flex items-center gap-0.5 text-[10px] font-medium ${ok ? "text-emerald-500" : "text-destructive/70"}`}>
            {ok ? <CheckCircle2 className="h-3 w-3" /> : <XCircle className="h-3 w-3" />}
            {label}
          </span>
        );
        return (
          <div className="flex items-center gap-2">
            <DnsBadge label="SPF" ok={status.spf === "ok"} />
            <DnsBadge label="DKIM" ok={status.dkim === "ok"} />
            <DnsBadge label="DMARC" ok={status.dmarc === "ok"} />
          </div>
        );
      },
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
