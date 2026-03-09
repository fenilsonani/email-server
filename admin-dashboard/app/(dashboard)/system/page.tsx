"use client";

import { useEffect, useState } from "react";
import { PageShell } from "@/components/shared/page-shell";
import { api } from "@/lib/api";
import type { SystemInfo } from "@/lib/types";
import { Skeleton } from "@/components/ui/skeleton";
import { Button } from "@/components/ui/button";
import { Server, Globe, Clock, Tag, Code, HardDrive, Download, Shield, ShieldCheck, KeyRound, Archive, ChevronRight, Wrench } from "lucide-react";
import Link from "next/link";
import { Switch } from "@/components/ui/switch";
import { useAdvancedMode } from "@/lib/advanced-mode";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";

function InfoCard({ icon: Icon, label, value }: { icon: React.ElementType; label: string; value: string }) {
  return (
    <div className="stat-card rounded-lg border border-border bg-card p-3.5">
      <div className="flex items-center gap-3">
        <Icon className="h-3.5 w-3.5 text-muted-foreground/40 shrink-0" strokeWidth={1.5} />
        <div>
          <p className="text-[11px] text-muted-foreground/60 font-medium uppercase tracking-wider">{label}</p>
          <p className="text-[13px] font-medium mt-0.5">{value}</p>
        </div>
      </div>
    </div>
  );
}

function AdvancedModeToggle() {
  const { enabled, toggle, hydrate } = useAdvancedMode();
  const [showConfirm, setShowConfirm] = useState(false);

  useEffect(() => {
    hydrate();
  }, [hydrate]);

  const handleToggle = () => {
    if (enabled) {
      // Turning off — no confirmation needed
      toggle();
    } else {
      // Turning on — ask for confirmation
      setShowConfirm(true);
    }
  };

  const confirmEnable = () => {
    toggle();
    setShowConfirm(false);
  };

  return (
    <>
      <div className="rounded-lg border border-border bg-card overflow-hidden">
        <div className="flex items-center justify-between px-4 py-3">
          <div className="flex items-center gap-3">
            <Wrench className="h-3.5 w-3.5 text-muted-foreground/40 shrink-0" strokeWidth={1.5} />
            <div>
              <span className="text-[13px] font-medium">Advanced Mode</span>
              <p className="text-[11px] text-muted-foreground/60 mt-0.5">
                Show logs, filters, DNS tools, and other advanced options in the sidebar
              </p>
            </div>
          </div>
          <Switch checked={enabled} onCheckedChange={handleToggle} />
        </div>
      </div>

      <Dialog open={showConfirm} onOpenChange={setShowConfirm}>
        <DialogContent showCloseButton={false}>
          <DialogHeader>
            <DialogTitle>Enable Advanced Mode?</DialogTitle>
            <DialogDescription>
              This will show additional tools in the sidebar including server logs, mail filters, DNS diagnostics, backup, and other system utilities. You can turn this off anytime from Settings.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowConfirm(false)}>Cancel</Button>
            <Button onClick={confirmEnable}>Enable Advanced Mode</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

function PageContent() {
  const [info, setInfo] = useState<SystemInfo | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api.get<SystemInfo>("/v1/system").then((res) => {
      if (res.success && res.data) setInfo(res.data);
      setLoading(false);
    });
  }, []);

  if (loading) {
    return (
      <PageShell title="System">
        <div className="grid gap-3 grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 5 }).map((_, i) => (
            <div key={i} className="rounded-lg border border-border bg-card p-3.5">
              <Skeleton className="h-3 w-16 mb-2" /><Skeleton className="h-4 w-24" />
            </div>
          ))}
        </div>
        <div className="rounded-lg border border-border bg-card p-4"><Skeleton className="h-48" /></div>
      </PageShell>
    );
  }

  if (!info) return null;

  const configRows = [
    { label: "IMAP Port", value: info.config.imap_port },
    { label: "IMAPS Port", value: info.config.imaps_port },
    { label: "SMTP Port", value: info.config.smtp_port },
    { label: "SMTPS Port", value: info.config.smtps_port },
    { label: "Admin Port", value: info.config.admin_port },
    { label: "Storage Path", value: info.config.storage_path },
  ];

  return (
    <PageShell title="System" description="Server information and configuration">
      <div className="grid gap-3 grid-cols-2 lg:grid-cols-3">
        <InfoCard icon={Server} label="Hostname" value={info.hostname} />
        <InfoCard icon={Globe} label="Domain" value={info.domain} />
        <InfoCard icon={Clock} label="Uptime" value={info.uptime_human} />
        <InfoCard icon={Tag} label="Version" value={info.version} />
        <InfoCard icon={Code} label="Go Version" value={info.go_version} />
      </div>

      <div className="rounded-lg border border-border bg-card overflow-hidden">
        <div className="px-4 py-3 border-b border-border flex items-center gap-2">
          <HardDrive className="h-3.5 w-3.5 text-muted-foreground/40" strokeWidth={1.5} />
          <span className="text-[13px] font-medium">Configuration</span>
        </div>
        <div className="divide-y divide-border">
          {configRows.map((row) => (
            <div key={row.label} className="flex items-center justify-between px-4 py-2.5 activity-row">
              <span className="text-[13px] text-muted-foreground">{row.label}</span>
              <span className="text-[13px] font-medium font-mono tabular-nums">{row.value}</span>
            </div>
          ))}
        </div>
      </div>
      <div className="rounded-lg border border-border bg-card overflow-hidden">
        <div className="px-4 py-3 border-b border-border flex items-center gap-2">
          <span className="text-[13px] font-medium">Management</span>
        </div>
        <div className="divide-y divide-border">
          {[
            { href: "/system/update", icon: Download, label: "Server Update", desc: "Check for and apply updates" },
            { href: "/system/certificates", icon: Shield, label: "TLS Certificates", desc: "Manage TLS certificates" },
            { href: "/system/2fa", icon: ShieldCheck, label: "Two-Factor Auth", desc: "Configure 2FA security" },
            { href: "/system/backup", icon: Archive, label: "Backup & Restore", desc: "Create backups and restore data" },
            { href: "/system/dkim", icon: KeyRound, label: "DKIM Auto-Rotation", desc: "Automatic DKIM key rotation settings" },
          ].map((item) => (
            <Link key={item.href} href={item.href} className="flex items-center justify-between px-4 py-3 activity-row hover:bg-muted/50 transition-colors">
              <div className="flex items-center gap-3">
                <item.icon className="h-3.5 w-3.5 text-muted-foreground/40 shrink-0" strokeWidth={1.5} />
                <div>
                  <span className="text-[13px] font-medium">{item.label}</span>
                  <p className="text-[11px] text-muted-foreground/60 mt-0.5">{item.desc}</p>
                </div>
              </div>
              <ChevronRight className="h-3.5 w-3.5 text-muted-foreground/30" />
            </Link>
          ))}
        </div>
      </div>
      <AdvancedModeToggle />
    </PageShell>
  );
}

export default function Page() {
  return <PageContent />;
}
