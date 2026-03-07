"use client";

import { useEffect, useState, useRef } from "react";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { api } from "@/lib/api";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from "@/components/ui/dialog";
import { HardDrive, Download, Upload, Archive, AlertTriangle, Clock, ArrowLeft } from "lucide-react";
import Link from "next/link";
import { formatDistanceToNow } from "date-fns";

interface BackupStatus {
  last_backup: string;
  size: string;
  location: string;
}

interface BackupHistoryItem {
  id: string;
  created_at: string;
  size: string;
  type: "manual" | "auto";
}

function PageContent() {
  const [status, setStatus] = useState<BackupStatus | null>(null);
  const [history, setHistory] = useState<BackupHistoryItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [restoring, setRestoring] = useState(false);
  const [showConfirm, setShowConfirm] = useState(false);
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const fetchData = async () => {
    setLoading(true);
    const [statusRes, historyRes] = await Promise.all([
      api.get<BackupStatus>("/v1/system/backup/status"),
      api.get<BackupHistoryItem[]>("/v1/system/backup/history"),
    ]);
    if (statusRes.success && statusRes.data) setStatus(statusRes.data);
    if (historyRes.success && historyRes.data) setHistory(historyRes.data);
    setLoading(false);
  };

  useEffect(() => {
    fetchData();
  }, []);

  const handleCreateBackup = async () => {
    setCreating(true);
    try {
      const res = await api.post("/v1/system/backup");
      if (res.success) {
        toast.success("Backup created successfully");
        await fetchData();
      } else {
        toast.error(res.error || "Failed to create backup");
      }
    } catch {
      toast.error("Failed to create backup");
    } finally {
      setCreating(false);
    }
  };

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0] || null;
    setSelectedFile(file);
    if (file) {
      setShowConfirm(true);
    }
  };

  const handleRestore = async () => {
    if (!selectedFile) return;
    setRestoring(true);
    try {
      const formData = new FormData();
      formData.append("file", selectedFile);
      const res = await fetch(`${process.env.NEXT_PUBLIC_API_URL || "/admin/api"}/v1/system/restore`, {
        method: "POST",
        credentials: "include",
        body: formData,
      });
      const data = await res.json();
      if (data.success) {
        toast.success("Backup restored successfully");
        await fetchData();
      } else {
        toast.error(data.error || "Failed to restore backup");
      }
    } catch {
      toast.error("Failed to restore backup");
    } finally {
      setRestoring(false);
      setShowConfirm(false);
      setSelectedFile(null);
      if (fileInputRef.current) fileInputRef.current.value = "";
    }
  };

  if (loading) {
    return (
      <PageShell title="Backup & Restore">
        <div className="space-y-4">
          {Array.from({ length: 3 }).map((_, i) => (
            <div key={i} className="rounded-lg border border-border bg-card p-4">
              <Skeleton className="h-4 w-48 mb-2" />
              <Skeleton className="h-3 w-32" />
            </div>
          ))}
        </div>
      </PageShell>
    );
  }

  return (
    <PageShell
      title="Backup & Restore"
      description="Create and restore system backups"
    >
      <Link href="/system/" className="inline-flex items-center gap-1.5 text-[12px] text-muted-foreground/60 hover:text-foreground transition-colors mb-4">
        <ArrowLeft className="h-3.5 w-3.5" />
        Back to System
      </Link>

      <div className="grid gap-4 md:grid-cols-2">
        {/* Create Backup */}
        <Card className="rounded-lg border border-border bg-card">
          <CardHeader className="pb-3">
            <CardTitle className="flex items-center gap-2 text-[14px] font-medium">
              <Download className="h-4 w-4 text-muted-foreground/60" />
              Create Backup
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            {status && (
              <div className="rounded-md bg-muted/30 p-3 space-y-2">
                {status.last_backup && (
                <div className="flex items-center gap-1.5">
                  <Clock className="h-3 w-3 text-muted-foreground/40" />
                  <span className="text-[11px] text-muted-foreground/60">Last backup:</span>
                  <span className="text-[12px] text-muted-foreground">
                    {formatDistanceToNow(new Date(status.last_backup), { addSuffix: true })}
                  </span>
                </div>
                )}
                <div className="flex items-center gap-1.5">
                  <HardDrive className="h-3 w-3 text-muted-foreground/40" />
                  <span className="text-[11px] text-muted-foreground/60">Size:</span>
                  <span className="text-[12px] text-muted-foreground">{status.size}</span>
                </div>
                <div className="flex items-center gap-1.5">
                  <Archive className="h-3 w-3 text-muted-foreground/40" />
                  <span className="text-[11px] text-muted-foreground/60">Location:</span>
                  <span className="text-[12px] text-muted-foreground font-mono">{status.location}</span>
                </div>
              </div>
            )}
            <Button
              size="sm"
              className="h-8 text-[12px] gap-1.5 w-full"
              onClick={handleCreateBackup}
              disabled={creating}
            >
              {creating ? (
                <>
                  <Download className="h-3.5 w-3.5 animate-spin" />
                  Creating Backup...
                </>
              ) : (
                <>
                  <Download className="h-3.5 w-3.5" />
                  Create Backup Now
                </>
              )}
            </Button>
          </CardContent>
        </Card>

        {/* Restore from Backup */}
        <Card className="rounded-lg border border-border bg-card">
          <CardHeader className="pb-3">
            <CardTitle className="flex items-center gap-2 text-[14px] font-medium">
              <Upload className="h-4 w-4 text-muted-foreground/60" />
              Restore from Backup
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="rounded-md bg-amber-500/5 border border-amber-500/20 p-3 flex items-start gap-2">
              <AlertTriangle className="h-3.5 w-3.5 text-amber-500 mt-0.5 shrink-0" />
              <p className="text-[11px] text-amber-600 leading-relaxed">
                Restoring a backup will overwrite all current data. This action cannot be undone. Make sure you have a recent backup before proceeding.
              </p>
            </div>
            <div className="space-y-1.5">
              <label className="text-[12px] text-muted-foreground font-normal">Backup file</label>
              <input
                ref={fileInputRef}
                type="file"
                accept=".tar.gz,.zip"
                onChange={handleFileChange}
                className="block w-full text-[12px] text-muted-foreground file:mr-3 file:py-1.5 file:px-3 file:rounded-md file:border-0 file:text-[12px] file:font-medium file:bg-primary file:text-primary-foreground hover:file:bg-primary/90 file:cursor-pointer"
              />
              <p className="text-[11px] text-muted-foreground/50">Accepted formats: .tar.gz, .zip</p>
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Backup History */}
      {history.length > 0 && (
        <Card className="rounded-lg border border-border bg-card">
          <CardHeader className="pb-3">
            <CardTitle className="flex items-center gap-2 text-[14px] font-medium">
              <Clock className="h-4 w-4 text-muted-foreground/60" />
              Backup History
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-2">
              {history.map((item) => (
                <div
                  key={item.id}
                  className="flex items-center justify-between rounded-md bg-muted/20 px-3 py-2"
                >
                  <div className="flex items-center gap-3">
                    <Archive className="h-3.5 w-3.5 text-muted-foreground/40" />
                    <div>
                      <span className="text-[12px] text-foreground">{item.id}</span>
                      <p className="text-[11px] text-muted-foreground/60">
                        {formatDistanceToNow(new Date(item.created_at), { addSuffix: true })}
                      </p>
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <span className="text-[11px] text-muted-foreground">{item.size}</span>
                    <Badge
                      variant="outline"
                      className="text-[10px]"
                    >
                      {item.type}
                    </Badge>
                  </div>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Restore Confirmation Dialog */}
      <Dialog open={showConfirm} onOpenChange={(open) => { if (!open) { setShowConfirm(false); setSelectedFile(null); if (fileInputRef.current) fileInputRef.current.value = ""; } }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="text-[15px]">Confirm Restore</DialogTitle>
            <DialogDescription className="text-[13px]">
              You are about to restore from <strong>{selectedFile?.name}</strong>. This will overwrite all current data and cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <div className="rounded-md bg-amber-500/5 border border-amber-500/20 p-3 flex items-start gap-2">
            <AlertTriangle className="h-3.5 w-3.5 text-amber-500 mt-0.5 shrink-0" />
            <p className="text-[11px] text-amber-600 leading-relaxed">
              All existing emails, users, domains, and settings will be replaced with the backup data.
            </p>
          </div>
          <DialogFooter>
            <Button variant="outline" size="sm" className="text-[12px]" onClick={() => { setShowConfirm(false); setSelectedFile(null); if (fileInputRef.current) fileInputRef.current.value = ""; }}>
              Cancel
            </Button>
            <Button variant="destructive" size="sm" className="text-[12px]" onClick={handleRestore} disabled={restoring}>
              {restoring ? "Restoring..." : "Restore Backup"}
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
