"use client";

import { useEffect, useState, useCallback } from "react";
import Link from "next/link";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { DataTable } from "@/components/shared/data-table";
import { api } from "@/lib/api";
import type { User, SystemInfo } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from "@/components/ui/dialog";
import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import { Plus, Pencil, Trash2, Shield, Download, FileDown } from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";
import type { ColumnDef } from "@tanstack/react-table";

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + " " + sizes[i];
}

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
    accessorKey: "quota_bytes",
    header: "Quota",
    cell: ({ row }) => {
      const used = row.original.used_bytes || 0;
      const quota = row.original.quota_bytes || 1073741824;
      const pct = Math.min((used / quota) * 100, 100);
      const color = pct > 90 ? "bg-destructive" : pct > 70 ? "bg-amber-500" : "bg-primary";
      return (
        <div className="w-28">
          <div className="flex items-center justify-between mb-0.5">
            <span className="text-[11px] text-muted-foreground tabular-nums">
              {formatBytes(used)} / {formatBytes(quota)}
            </span>
          </div>
          <div className="h-1.5 w-full rounded-full bg-muted overflow-hidden">
            <div className={`h-full rounded-full transition-all ${color}`} style={{ width: `${pct}%` }} />
          </div>
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
  const [systemInfo, setSystemInfo] = useState<SystemInfo | null>(null);
  const [downloadMenu, setDownloadMenu] = useState<{ id: number; x: number; y: number } | null>(null);

  const fetchUsers = useCallback(async () => {
    setLoading(true);
    const params: Record<string, string> = { page: String(page), page_size: "20" };
    if (domainFilter) params.domain = domainFilter;
    if (adminOnly) params.is_admin = "true";
    const res = await api.get<User[]>("/v1/users", params);
    if (res.success && Array.isArray(res.data)) {
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
      if (res.success && Array.isArray(res.data)) setDomains(res.data);
    });
    api.get<SystemInfo>("/v1/system").then((res) => {
      if (res.success && res.data) setSystemInfo(res.data);
    });
  }, []);

  const triggerDownload = (content: string, filename: string, mimeType: string) => {
    const blob = new Blob([content], { type: mimeType });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  };

  const generateSimpleUUID = (seed: string) => {
    let hash = 0;
    for (let i = 0; i < seed.length; i++) {
      hash = ((hash << 5) - hash + seed.charCodeAt(i)) | 0;
    }
    const hex = Math.abs(hash).toString(16).padStart(8, "0");
    return `${hex.slice(0, 8)}-${hex.slice(0, 4)}-4${hex.slice(1, 4)}-a${hex.slice(1, 4)}-${hex.padEnd(12, "0").slice(0, 12)}`;
  };

  const downloadProfile = (user: User, type: "apple" | "thunderbird" | "outlook") => {
    if (!systemInfo) return;
    const domain = user.email.split("@")[1];
    const hostname = systemInfo.hostname;
    const imapsPort = systemInfo.config.imaps_port;
    const smtpsPort = systemInfo.config.smtps_port;
    const smtpPort = systemInfo.config.smtp_port;

    if (type === "apple") {
      const displayName = domain + " Mail";
      const xml = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>PayloadContent</key>
    <array>
        <dict>
            <key>EmailAccountDescription</key>
            <string>${displayName}</string>
            <key>EmailAccountName</key>
            <string>${user.email}</string>
            <key>EmailAccountType</key>
            <string>EmailTypeIMAP</string>
            <key>EmailAddress</key>
            <string>${user.email}</string>
            <key>IncomingMailServerAuthentication</key>
            <string>EmailAuthPassword</string>
            <key>IncomingMailServerHostName</key>
            <string>${hostname}</string>
            <key>IncomingMailServerPortNumber</key>
            <integer>${imapsPort}</integer>
            <key>IncomingMailServerUseSSL</key>
            <true/>
            <key>IncomingMailServerUsername</key>
            <string>${user.email}</string>
            <key>OutgoingMailServerAuthentication</key>
            <string>EmailAuthPassword</string>
            <key>OutgoingMailServerHostName</key>
            <string>${hostname}</string>
            <key>OutgoingMailServerPortNumber</key>
            <integer>${smtpsPort}</integer>
            <key>OutgoingMailServerUseSSL</key>
            <true/>
            <key>OutgoingMailServerUsername</key>
            <string>${user.email}</string>
            <key>OutgoingPasswordSameAsIncomingPassword</key>
            <true/>
            <key>PayloadDescription</key>
            <string>Email account</string>
            <key>PayloadDisplayName</key>
            <string>${displayName}</string>
            <key>PayloadIdentifier</key>
            <string>com.${domain}.email</string>
            <key>PayloadType</key>
            <string>com.apple.mail.managed</string>
            <key>PayloadUUID</key>
            <string>${generateSimpleUUID(user.email + "-email")}</string>
            <key>PayloadVersion</key>
            <integer>1</integer>
            <key>PreventAppSheet</key>
            <false/>
            <key>PreventMove</key>
            <false/>
            <key>SMIMEEnabled</key>
            <false/>
        </dict>
    </array>
    <key>PayloadDescription</key>
    <string>Email configuration for ${domain}</string>
    <key>PayloadDisplayName</key>
    <string>${displayName}</string>
    <key>PayloadIdentifier</key>
    <string>com.${domain}.profile</string>
    <key>PayloadOrganization</key>
    <string>${domain}</string>
    <key>PayloadRemovalDisallowed</key>
    <false/>
    <key>PayloadType</key>
    <string>Configuration</string>
    <key>PayloadUUID</key>
    <string>${generateSimpleUUID(user.email + "-profile")}</string>
    <key>PayloadVersion</key>
    <integer>1</integer>
</dict>
</plist>`;
      triggerDownload(xml, `${domain}-email.mobileconfig`, "application/x-apple-aspen-config");
    } else if (type === "thunderbird") {
      const xml = `<?xml version="1.0" encoding="UTF-8"?>
<clientConfig version="1.1">
  <emailProvider id="${domain}">
    <domain>${domain}</domain>
    <displayName>${domain} Mail</displayName>
    <incomingServer type="imap">
      <hostname>${hostname}</hostname>
      <port>${imapsPort}</port>
      <socketType>SSL</socketType>
      <username>${user.email}</username>
      <authentication>password-cleartext</authentication>
    </incomingServer>
    <outgoingServer type="smtp">
      <hostname>${hostname}</hostname>
      <port>${smtpPort}</port>
      <socketType>STARTTLS</socketType>
      <username>${user.email}</username>
      <authentication>password-cleartext</authentication>
    </outgoingServer>
  </emailProvider>
</clientConfig>`;
      triggerDownload(xml, `autoconfig-${user.email}.xml`, "application/xml");
    } else {
      const xml = `<?xml version="1.0" encoding="utf-8"?>
<Autodiscover xmlns="http://schemas.microsoft.com/exchange/autodiscover/responseschema/2006">
  <Response xmlns="http://schemas.microsoft.com/exchange/autodiscover/outlook/responseschema/2006a">
    <Account>
      <AccountType>email</AccountType>
      <Action>settings</Action>
      <Protocol>
        <Type>IMAP</Type>
        <Server>${hostname}</Server>
        <Port>${imapsPort}</Port>
        <LoginName>${user.email}</LoginName>
        <DomainRequired>off</DomainRequired>
        <SPA>off</SPA>
        <SSL>on</SSL>
        <AuthRequired>on</AuthRequired>
      </Protocol>
      <Protocol>
        <Type>SMTP</Type>
        <Server>${hostname}</Server>
        <Port>${smtpPort}</Port>
        <LoginName>${user.email}</LoginName>
        <DomainRequired>off</DomainRequired>
        <SPA>off</SPA>
        <Encryption>TLS</Encryption>
        <AuthRequired>on</AuthRequired>
        <UsePOPAuth>on</UsePOPAuth>
        <SMTPLast>off</SMTPLast>
      </Protocol>
    </Account>
  </Response>
</Autodiscover>`;
      triggerDownload(xml, `autodiscover-${user.email}.xml`, "application/xml");
    }
    toast.success(`Profile downloaded for ${user.email}`);
    setDownloadMenu(null);
  };

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
          {systemInfo && (
            <button
              onClick={(e) => {
                if (downloadMenu?.id === row.original.id) {
                  setDownloadMenu(null);
                } else {
                  const rect = e.currentTarget.getBoundingClientRect();
                  setDownloadMenu({ id: row.original.id, x: rect.right, y: rect.top });
                }
              }}
              className="inline-flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground/50 transition-colors hover:bg-accent hover:text-foreground"
              title="Download mail profile"
            >
              <Download className="h-3.5 w-3.5" />
            </button>
          )}
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
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" className="h-8 text-[12px] gap-1.5" onClick={() => {
            const csv = ["Email,Domain,Role,Quota,Used,Created"];
            users.forEach(u => csv.push(`${u.email},${u.domain},${u.is_admin ? "admin" : "user"},${u.quota_bytes},${u.used_bytes || 0},${u.created_at}`));
            const blob = new Blob([csv.join("\n")], { type: "text/csv" });
            const a = document.createElement("a"); a.href = URL.createObjectURL(blob); a.download = "users.csv"; a.click(); URL.revokeObjectURL(a.href);
            toast.success("Users exported");
          }}>
            <FileDown className="h-3.5 w-3.5" />Export
          </Button>
          <Link href="/users/new/">
            <Button size="sm" className="h-8 text-[12px] gap-1.5">
              <Plus className="h-3.5 w-3.5" />
              Add User
            </Button>
          </Link>
        </div>
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

      {downloadMenu && (() => {
        const menuUser = users.find((u) => u.id === downloadMenu.id);
        if (!menuUser) return null;
        return (
          <>
            <div className="fixed inset-0 z-40" onClick={() => setDownloadMenu(null)} />
            <div
              className="fixed z-50 w-44 rounded-lg border bg-popover p-1 shadow-md animate-in fade-in-0 zoom-in-95"
              style={{ top: downloadMenu.y - 108, left: downloadMenu.x - 176 }}
            >
              <button
                onClick={() => downloadProfile(menuUser, "apple")}
                className="flex w-full items-center gap-2 rounded-md px-2.5 py-1.5 text-[12px] hover:bg-accent transition-colors"
              >
                Apple Mail
              </button>
              <button
                onClick={() => downloadProfile(menuUser, "thunderbird")}
                className="flex w-full items-center gap-2 rounded-md px-2.5 py-1.5 text-[12px] hover:bg-accent transition-colors"
              >
                Thunderbird
              </button>
              <button
                onClick={() => downloadProfile(menuUser, "outlook")}
                className="flex w-full items-center gap-2 rounded-md px-2.5 py-1.5 text-[12px] hover:bg-accent transition-colors"
              >
                Outlook
              </button>
            </div>
          </>
        );
      })()}
    </PageShell>
  );
}

export default function Page() {
  return <AuthGuard><UsersContent /></AuthGuard>;
}
