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
import { AreaChart, Area, BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, CartesianGrid } from "recharts";
import { useChartColors } from "@/lib/chart-colors";

interface DayCount { date: string; count: number }
interface IPCount { ip: string; count: number }
interface ProtoCount { protocol: string; count: number }

interface SecurityOverview {
  suppression_count: number;
  greylist_count: number;
  failed_login_count: number;
  daily_trend: DayCount[];
  top_ips: IPCount[];
  protocol_breakdown: ProtoCount[];
}

const PROTO_COLORS: Record<string, string> = {
  imap: "#6366f1",
  smtp: "#f59e0b",
  pop3: "#10b981",
  unknown: "#6b7280",
};

function SecurityContent() {
  const [data, setData] = useState<SecurityOverview | null>(null);
  const [loading, setLoading] = useState(true);
  const colors = useChartColors();

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

      {/* Trend Charts */}
      {data && (
        <div className="grid md:grid-cols-2 gap-4 mt-4">
          {/* Failed Logins Trend */}
          {Array.isArray(data.daily_trend) && data.daily_trend.length > 0 && (
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-[13px] font-medium">Failed Logins (30 days)</CardTitle>
              </CardHeader>
              <CardContent>
                <ResponsiveContainer width="100%" height={200}>
                  <AreaChart data={data.daily_trend}>
                    <CartesianGrid strokeDasharray="3 3" stroke={colors.grid} />
                    <XAxis dataKey="date" tick={{ fontSize: 10, fill: colors.tick }} tickFormatter={(v: string) => v.slice(5)} stroke={colors.tick} />
                    <YAxis tick={{ fontSize: 10, fill: colors.tick }} stroke={colors.tick} allowDecimals={false} />
                    <Tooltip contentStyle={{ fontSize: 12, background: colors.tooltipBg, border: `1px solid ${colors.tooltipBorder}`, borderRadius: 8, color: colors.tooltipText }} />
                    <Area type="monotone" dataKey="count" stroke="#ef4444" fill="#ef444420" name="Failed Logins" />
                  </AreaChart>
                </ResponsiveContainer>
              </CardContent>
            </Card>
          )}

          {/* Top Offending IPs */}
          {Array.isArray(data.top_ips) && data.top_ips.length > 0 && (
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-[13px] font-medium">Top Offending IPs (7 days)</CardTitle>
              </CardHeader>
              <CardContent>
                <ResponsiveContainer width="100%" height={200}>
                  <BarChart data={data.top_ips} layout="vertical">
                    <CartesianGrid strokeDasharray="3 3" stroke={colors.grid} />
                    <XAxis type="number" tick={{ fontSize: 10, fill: colors.tick }} stroke={colors.tick} allowDecimals={false} />
                    <YAxis type="category" dataKey="ip" tick={{ fontSize: 10, fill: colors.tick }} stroke={colors.tick} width={120} />
                    <Tooltip contentStyle={{ fontSize: 12, background: colors.tooltipBg, border: `1px solid ${colors.tooltipBorder}`, borderRadius: 8, color: colors.tooltipText }} />
                    <Bar dataKey="count" fill="#f59e0b" name="Attempts" radius={[0, 4, 4, 0]} />
                  </BarChart>
                </ResponsiveContainer>
              </CardContent>
            </Card>
          )}

          {/* Protocol Breakdown */}
          {Array.isArray(data.protocol_breakdown) && data.protocol_breakdown.length > 0 && (
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-[13px] font-medium">Failed Logins by Protocol (30 days)</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="space-y-2">
                  {(() => {
                    const total = data.protocol_breakdown.reduce((s, p) => s + p.count, 0);
                    return data.protocol_breakdown.map((p) => {
                      const pct = total > 0 ? (p.count / total * 100) : 0;
                      const color = PROTO_COLORS[p.protocol.toLowerCase()] || "#6b7280";
                      return (
                        <div key={p.protocol} className="space-y-1">
                          <div className="flex items-center justify-between text-[12px]">
                            <span className="font-medium uppercase">{p.protocol}</span>
                            <span className="text-muted-foreground tabular-nums">{p.count} ({pct.toFixed(0)}%)</span>
                          </div>
                          <div className="h-2 w-full rounded-full bg-muted overflow-hidden">
                            <div className="h-full rounded-full transition-all" style={{ width: `${pct}%`, backgroundColor: color }} />
                          </div>
                        </div>
                      );
                    });
                  })()}
                </div>
              </CardContent>
            </Card>
          )}
        </div>
      )}
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
