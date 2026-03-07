"use client";

import { useEffect, useState } from "react";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { api } from "@/lib/api";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Shield, Timer, Eye, Layers, BellOff, ArrowLeft, Save } from "lucide-react";
import Link from "next/link";

interface Preferences {
  undo_send_delay: number;
  screener_enabled: boolean;
  tracker_blocking: string;
  zones_enabled: boolean;
  snooze_mark_unread: boolean;
}

const defaultPreferences: Preferences = {
  undo_send_delay: 10,
  screener_enabled: true,
  tracker_blocking: "block",
  zones_enabled: true,
  snooze_mark_unread: true,
};

function PageContent() {
  const [prefs, setPrefs] = useState<Preferences>(defaultPreferences);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    const fetchPrefs = async () => {
      const res = await api.get<Preferences>("/v1/features/preferences");
      if (res.success && res.data) {
        setPrefs(res.data);
      }
      setLoading(false);
    };
    fetchPrefs();
  }, []);

  const handleSave = async () => {
    setSaving(true);
    try {
      const res = await api.put("/v1/features/preferences", prefs);
      if (res.success) {
        toast.success("Preferences saved");
      } else {
        toast.error(res.error || "Failed to save preferences");
      }
    } catch {
      toast.error("Failed to save preferences");
    } finally {
      setSaving(false);
    }
  };

  const update = <K extends keyof Preferences>(key: K, value: Preferences[K]) => {
    setPrefs((prev) => ({ ...prev, [key]: value }));
  };

  if (loading) {
    return (
      <PageShell title="Email Preferences">
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
      title="Email Preferences"
      description="Configure auto-reply, forwarding, and signature settings"
      actions={
        <Button
          size="sm"
          className="h-8 text-[12px] gap-1.5"
          onClick={handleSave}
          disabled={saving}
        >
          <Save className="h-3.5 w-3.5" />
          {saving ? "Saving..." : "Save Preferences"}
        </Button>
      }
    >
      <Link href="/features/" className="inline-flex items-center gap-1.5 text-[12px] text-muted-foreground/60 hover:text-foreground transition-colors mb-4">
        <ArrowLeft className="h-3.5 w-3.5" />
        Back to Features
      </Link>

      <div className="space-y-4">
        {/* Undo Send */}
        <Card className="rounded-lg border border-border bg-card">
          <CardHeader className="pb-3">
            <CardTitle className="flex items-center gap-2 text-[14px] font-medium">
              <Timer className="h-4 w-4 text-muted-foreground/60" />
              Undo Send Delay
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-1.5">
              <label className="text-[12px] text-muted-foreground font-normal">Delay before sending (seconds)</label>
              <select
                value={prefs.undo_send_delay}
                onChange={(e) => update("undo_send_delay", parseInt(e.target.value))}
                className="h-8 w-full rounded-lg border border-border bg-background/50 px-2.5 text-[13px] text-foreground outline-none focus-visible:border-ring"
              >
                <option value={0}>Off</option>
                <option value={5}>5 seconds</option>
                <option value={10}>10 seconds</option>
                <option value={20}>20 seconds</option>
                <option value={30}>30 seconds</option>
              </select>
            </div>
          </CardContent>
        </Card>

        {/* Screener */}
        <Card className="rounded-lg border border-border bg-card">
          <CardHeader className="pb-3">
            <div className="flex items-center justify-between">
              <CardTitle className="flex items-center gap-2 text-[14px] font-medium">
                <Shield className="h-4 w-4 text-muted-foreground/60" />
                Screener
              </CardTitle>
              <Switch
                checked={prefs.screener_enabled}
                onCheckedChange={(checked) => update("screener_enabled", checked)}
              />
            </div>
          </CardHeader>
          <CardContent>
            <p className="text-[12px] text-muted-foreground">
              Screen emails from unknown senders before they reach your inbox.
            </p>
          </CardContent>
        </Card>

        {/* Tracker Blocking */}
        <Card className="rounded-lg border border-border bg-card">
          <CardHeader className="pb-3">
            <CardTitle className="flex items-center gap-2 text-[14px] font-medium">
              <Eye className="h-4 w-4 text-muted-foreground/60" />
              Tracker Blocking
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-1.5">
              <label className="text-[12px] text-muted-foreground font-normal">Tracking pixel handling</label>
              <select
                value={prefs.tracker_blocking}
                onChange={(e) => update("tracker_blocking", e.target.value)}
                className="h-8 w-full rounded-lg border border-border bg-background/50 px-2.5 text-[13px] text-foreground outline-none focus-visible:border-ring"
              >
                <option value="block">Block trackers</option>
                <option value="proxy">Proxy through server</option>
                <option value="off">Allow all</option>
              </select>
            </div>
          </CardContent>
        </Card>

        {/* Smart Zones */}
        <Card className="rounded-lg border border-border bg-card">
          <CardHeader className="pb-3">
            <div className="flex items-center justify-between">
              <CardTitle className="flex items-center gap-2 text-[14px] font-medium">
                <Layers className="h-4 w-4 text-muted-foreground/60" />
                Smart Zones
              </CardTitle>
              <Switch
                checked={prefs.zones_enabled}
                onCheckedChange={(checked) => update("zones_enabled", checked)}
              />
            </div>
          </CardHeader>
          <CardContent>
            <p className="text-[12px] text-muted-foreground">
              Automatically categorize incoming emails into zones (Priority, Feed, Paper Trail).
            </p>
          </CardContent>
        </Card>

        {/* Snooze */}
        <Card className="rounded-lg border border-border bg-card">
          <CardHeader className="pb-3">
            <div className="flex items-center justify-between">
              <CardTitle className="flex items-center gap-2 text-[14px] font-medium">
                <BellOff className="h-4 w-4 text-muted-foreground/60" />
                Snooze Mark Unread
              </CardTitle>
              <Switch
                checked={prefs.snooze_mark_unread}
                onCheckedChange={(checked) => update("snooze_mark_unread", checked)}
              />
            </div>
          </CardHeader>
          <CardContent>
            <p className="text-[12px] text-muted-foreground">
              Mark snoozed emails as unread when they reappear in your inbox.
            </p>
          </CardContent>
        </Card>
      </div>
    </PageShell>
  );
}

export default function Page() {
  return <AuthGuard><PageContent /></AuthGuard>;
}
