"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { api } from "@/lib/api";
import type { FeaturesOverview } from "@/lib/types";
import { Skeleton } from "@/components/ui/skeleton";
import { Shield, AtSign, Star, Clock, BellOff, Settings2, ChevronRight } from "lucide-react";

const featureItems: { key: keyof FeaturesOverview | null; label: string; desc: string; icon: React.ComponentType<{ className?: string; strokeWidth?: number }>; href: string }[] = [
  { key: "screener_count", label: "Screener", desc: "Filter unknown senders", icon: Shield, href: "/features/screener" },
  { key: "alias_count", label: "Aliases", desc: "Email address aliases", icon: AtSign, href: "/features/aliases" },
  { key: "vip_count", label: "VIP", desc: "Priority sender rules", icon: Star, href: "/features/vip" },
  { key: "scheduled_count", label: "Scheduled", desc: "Delayed send emails", icon: Clock, href: "/features/scheduled" },
  { key: "snoozed_count", label: "Snoozed", desc: "Temporarily hidden emails", icon: BellOff, href: "/features/snoozed" },
  { key: null, label: "Preferences", desc: "Auto-reply, forwarding, signature", icon: Settings2, href: "/features/preferences" },
];

function PageContent() {
  const [features, setFeatures] = useState<FeaturesOverview | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api.get<FeaturesOverview>("/v1/features").then((res) => {
      if (res.success && res.data) setFeatures(res.data);
      setLoading(false);
    });
  }, []);

  if (loading) {
    return (
      <PageShell title="Features">
        <div className="space-y-2">
          {Array.from({ length: 5 }).map((_, i) => (
            <div key={i} className="rounded-lg border border-border p-3.5"><Skeleton className="h-8" /></div>
          ))}
        </div>
      </PageShell>
    );
  }

  return (
    <PageShell title="Features" description="Email features and smart rules">
      <div className="rounded-lg border border-border overflow-hidden divide-y divide-border">
        {featureItems.map((item) => (
          <Link
            key={item.label}
            href={item.href}
            className="flex items-center gap-3 px-4 py-3 activity-row group"
          >
            <item.icon className="h-4 w-4 text-muted-foreground/40 shrink-0" strokeWidth={1.5} />
            <div className="flex-1 min-w-0">
              <p className="text-[13px] font-medium">{item.label}</p>
              <p className="text-[12px] text-muted-foreground/60">{item.desc}</p>
            </div>
            {item.key && features && (
              <span className="text-[13px] font-medium tabular-nums text-muted-foreground">
                {features[item.key]}
              </span>
            )}
            <ChevronRight className="h-3.5 w-3.5 text-muted-foreground/30 group-hover:text-muted-foreground transition-colors" />
          </Link>
        ))}
      </div>
    </PageShell>
  );
}

export default function Page() {
  return <AuthGuard><PageContent /></AuthGuard>;
}
