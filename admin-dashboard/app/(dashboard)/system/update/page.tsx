"use client";

import { useState } from "react";
import { PageShell } from "@/components/shared/page-shell";
import { api } from "@/lib/api";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { ArrowLeft, Download, Tag, RefreshCw } from "lucide-react";
import Link from "next/link";

interface UpdateInfo {
  current_version: string;
  latest_version: string;
  update_available: boolean;
  release_notes?: string;
}

function PageContent() {
  const [updateInfo, setUpdateInfo] = useState<UpdateInfo | null>(null);
  const [checking, setChecking] = useState(false);

  const checkForUpdates = async () => {
    setChecking(true);
    try {
      const res = await api.post<UpdateInfo>("/v1/system/check-update");
      if (res.success && res.data) {
        setUpdateInfo(res.data);
        if (res.data.update_available) {
          toast.success("Update available!");
        } else {
          toast.info("You are running the latest version.");
        }
      } else {
        toast.error(res.error || "Failed to check for updates");
      }
    } catch {
      toast.error("Failed to check for updates");
    } finally {
      setChecking(false);
    }
  };

  return (
    <PageShell
      title="Server Update"
      description="Check for and apply server updates"
      actions={
        <Link href="/system/">
          <Button variant="ghost" size="icon-xs">
            <ArrowLeft className="h-4 w-4" />
          </Button>
        </Link>
      }
    >
      <div className="rounded-lg border border-border bg-card p-4 space-y-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Download className="h-3.5 w-3.5 text-muted-foreground/40" strokeWidth={1.5} />
            <span className="text-[13px] font-medium">Update Check</span>
          </div>
          <Button
            size="sm"
            className="h-8 text-[12px] gap-1.5"
            onClick={checkForUpdates}
            disabled={checking}
          >
            <RefreshCw className={`h-3.5 w-3.5 ${checking ? "animate-spin" : ""}`} />
            {checking ? "Checking..." : "Check for Updates"}
          </Button>
        </div>

        {checking && !updateInfo && (
          <div className="space-y-3">
            <Skeleton className="h-4 w-48" />
            <Skeleton className="h-4 w-32" />
          </div>
        )}

        {updateInfo && (
          <div className="space-y-4">
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="rounded-lg border border-border bg-background/50 p-3">
                <div className="flex items-center gap-2 mb-1">
                  <Tag className="h-3 w-3 text-muted-foreground/60" strokeWidth={1.5} />
                  <span className="text-[12px] text-muted-foreground font-normal">Current Version</span>
                </div>
                <div className="flex items-center gap-2">
                  <span className="text-[13px] font-medium font-mono">{updateInfo.current_version}</span>
                  {!updateInfo.update_available && (
                    <Badge variant="outline" className="text-[10px] border-green-500/30 text-green-600 bg-green-500/10">
                      Latest
                    </Badge>
                  )}
                </div>
              </div>

              <div className="rounded-lg border border-border bg-background/50 p-3">
                <div className="flex items-center gap-2 mb-1">
                  <Tag className="h-3 w-3 text-muted-foreground/60" strokeWidth={1.5} />
                  <span className="text-[12px] text-muted-foreground font-normal">Latest Version</span>
                </div>
                <div className="flex items-center gap-2">
                  <span className="text-[13px] font-medium font-mono">{updateInfo.latest_version}</span>
                  {updateInfo.update_available && (
                    <Badge variant="outline" className="text-[10px] border-blue-500/30 text-blue-600 bg-blue-500/10">
                      New
                    </Badge>
                  )}
                </div>
              </div>
            </div>

            {updateInfo.update_available && (
              <>
                {updateInfo.release_notes && (
                  <div className="rounded-lg border border-border bg-background/50 p-3">
                    <span className="text-[12px] text-muted-foreground font-normal">Release Notes</span>
                    <p className="text-[13px] mt-1.5 whitespace-pre-wrap">{updateInfo.release_notes}</p>
                  </div>
                )}

                <div className="rounded-lg border border-border bg-background/50 p-3">
                  <span className="text-[12px] text-muted-foreground font-normal">How to Update</span>
                  <p className="text-[13px] mt-1.5 text-muted-foreground">
                    SSH into the server and run the following command:
                  </p>
                  <pre className="mt-2 rounded-md bg-muted/50 border border-border p-3 text-[12px] font-mono overflow-x-auto">
{`git pull && go build -o /usr/local/bin/mailserver ./cmd/mailserver/ && systemctl restart mailserver`}
                  </pre>
                </div>
              </>
            )}
          </div>
        )}
      </div>
    </PageShell>
  );
}

export default function Page() {
  return <PageContent />;
}
