"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Badge } from "@/components/ui/badge";
import { api } from "@/lib/api";
import { Ban, ShieldCheck, AlertTriangle, ArrowRight } from "lucide-react";

interface SecurityOverview {
  suppression_count: number;
  greylist_count: number;
  failed_login_count: number;
}

function SecurityContent() {
  const [data, setData] = useState<SecurityOverview | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api.get<SecurityOverview>("/v1/security/overview").then((res) => {
      if (res.success && res.data) setData(res.data);
      setLoading(false);
    });
  }, []);

  const cards = [
    {
      title: "Blocklist",
      description: "Suppressed email addresses (bounced, complaints, manual blocks)",
      href: "/security/blocklist/",
      icon: Ban,
      count: data?.suppression_count ?? 0,
      color: "text-destructive",
    },
    {
      title: "Greylist",
      description: "Greylisted senders pending verification",
      href: "/security/greylist/",
      icon: ShieldCheck,
      count: data?.greylist_count ?? 0,
      color: "text-amber-500",
    },
    {
      title: "Failed Logins",
      description: "IP addresses with failed authentication attempts",
      href: "/security/failed-logins/",
      icon: AlertTriangle,
      count: data?.failed_login_count ?? 0,
      color: "text-orange-500",
    },
  ];

  return (
    <PageShell title="Security" description="Manage blocklists, greylisting, and monitor failed logins">
      <div className="grid md:grid-cols-3 gap-4">
        {loading
          ? Array.from({ length: 3 }).map((_, i) => (
              <Card key={i}>
                <CardContent className="pt-5 pb-4">
                  <Skeleton className="h-5 w-24 mb-2" />
                  <Skeleton className="h-4 w-full mb-3" />
                  <Skeleton className="h-8 w-16" />
                </CardContent>
              </Card>
            ))
          : cards.map((card) => (
              <Link key={card.title} href={card.href}>
                <Card className="hover:border-ring/50 transition-colors cursor-pointer h-full">
                  <CardHeader className="pb-2">
                    <CardTitle className="text-[13px] font-medium flex items-center gap-2">
                      <card.icon className={`h-4 w-4 ${card.color}`} />
                      {card.title}
                      <Badge variant="secondary" className="ml-auto text-[10px] tabular-nums">
                        {card.count}
                      </Badge>
                    </CardTitle>
                  </CardHeader>
                  <CardContent>
                    <p className="text-[12px] text-muted-foreground mb-3">{card.description}</p>
                    <span className="text-[12px] text-primary flex items-center gap-1">
                      Manage <ArrowRight className="h-3 w-3" />
                    </span>
                  </CardContent>
                </Card>
              </Link>
            ))}
      </div>
    </PageShell>
  );
}

export default function Page() {
  return (
    <AuthGuard>
      <SecurityContent />
    </AuthGuard>
  );
}
