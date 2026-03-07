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
import { Reply, Forward, FileSignature, ArrowLeft, Save } from "lucide-react";
import Link from "next/link";

interface Preferences {
  auto_reply_enabled: boolean;
  auto_reply_subject: string;
  auto_reply_body: string;
  forwarding_enabled: boolean;
  forwarding_address: string;
  forwarding_keep_copy: boolean;
  signature: string;
}

const defaultPreferences: Preferences = {
  auto_reply_enabled: false,
  auto_reply_subject: "",
  auto_reply_body: "",
  forwarding_enabled: false,
  forwarding_address: "",
  forwarding_keep_copy: true,
  signature: "",
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
        {/* Auto Reply */}
        <Card className="rounded-lg border border-border bg-card">
          <CardHeader className="pb-3">
            <div className="flex items-center justify-between">
              <CardTitle className="flex items-center gap-2 text-[14px] font-medium">
                <Reply className="h-4 w-4 text-muted-foreground/60" />
                Auto Reply
              </CardTitle>
              <Switch
                checked={prefs.auto_reply_enabled}
                onCheckedChange={(checked) => update("auto_reply_enabled", checked)}
              />
            </div>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="space-y-1.5">
              <label className="text-[12px] text-muted-foreground font-normal">Subject</label>
              <Input
                value={prefs.auto_reply_subject}
                onChange={(e) => update("auto_reply_subject", e.target.value)}
                placeholder="Out of office"
                className="h-8 text-[13px] bg-background/50 border-border"
                disabled={!prefs.auto_reply_enabled}
              />
            </div>
            <div className="space-y-1.5">
              <label className="text-[12px] text-muted-foreground font-normal">Message</label>
              <textarea
                value={prefs.auto_reply_body}
                onChange={(e) => update("auto_reply_body", e.target.value)}
                placeholder="I am currently out of the office and will respond when I return."
                className="w-full text-[13px] bg-background/50 border-border rounded-lg p-3 min-h-[120px] resize-y border focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 focus:ring-offset-background disabled:opacity-50 disabled:cursor-not-allowed"
                disabled={!prefs.auto_reply_enabled}
              />
            </div>
          </CardContent>
        </Card>

        {/* Forwarding */}
        <Card className="rounded-lg border border-border bg-card">
          <CardHeader className="pb-3">
            <div className="flex items-center justify-between">
              <CardTitle className="flex items-center gap-2 text-[14px] font-medium">
                <Forward className="h-4 w-4 text-muted-foreground/60" />
                Forwarding
              </CardTitle>
              <Switch
                checked={prefs.forwarding_enabled}
                onCheckedChange={(checked) => update("forwarding_enabled", checked)}
              />
            </div>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="space-y-1.5">
              <label className="text-[12px] text-muted-foreground font-normal">Forward to address</label>
              <Input
                type="email"
                value={prefs.forwarding_address}
                onChange={(e) => update("forwarding_address", e.target.value)}
                placeholder="user@example.com"
                className="h-8 text-[13px] bg-background/50 border-border"
                disabled={!prefs.forwarding_enabled}
              />
            </div>
            <div className="flex items-center justify-between">
              <label className="text-[12px] text-muted-foreground font-normal">Keep a copy of forwarded messages</label>
              <Switch
                checked={prefs.forwarding_keep_copy}
                onCheckedChange={(checked) => update("forwarding_keep_copy", checked)}
                disabled={!prefs.forwarding_enabled}
              />
            </div>
          </CardContent>
        </Card>

        {/* Signature */}
        <Card className="rounded-lg border border-border bg-card">
          <CardHeader className="pb-3">
            <CardTitle className="flex items-center gap-2 text-[14px] font-medium">
              <FileSignature className="h-4 w-4 text-muted-foreground/60" />
              Email Signature
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-1.5">
              <label className="text-[12px] text-muted-foreground font-normal">Signature text</label>
              <textarea
                value={prefs.signature}
                onChange={(e) => update("signature", e.target.value)}
                placeholder="Best regards,&#10;Your Name"
                className="w-full text-[13px] bg-background/50 border-border rounded-lg p-3 min-h-[120px] resize-y border focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 focus:ring-offset-background"
              />
            </div>
          </CardContent>
        </Card>
      </div>
    </PageShell>
  );
}

export default function Page() {
  return <AuthGuard><PageContent /></AuthGuard>;
}
