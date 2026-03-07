"use client";

import { useEffect, useState, useCallback } from "react";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { api } from "@/lib/api";
import { Skeleton } from "@/components/ui/skeleton";
import { Mail, MailOpen, AlertTriangle, ArrowUpDown } from "lucide-react";
import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  BarChart,
  Bar,
} from "recharts";

interface TimePoint {
  time: string;
  inbound: number;
  outbound: number;
}

interface AnalyticsData {
  time_series: TimePoint[];
  summary: {
    total_inbound: number;
    total_outbound: number;
    bounces: number;
    avg_size_bytes: number;
  };
  top_domains: { domain: string; count: number }[];
  top_senders: { email: string; count: number }[];
  range: string;
}

function formatSize(bytes: number): string {
  if (bytes === 0) return "0 B";
  const k = 1024;
  const sizes = ["B", "KB", "MB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + " " + sizes[i];
}

function formatTimeLabel(time: string, range: string): string {
  const d = new Date(time);
  if (range === "30d") {
    return d.toLocaleDateString("en-US", { month: "short", day: "numeric" });
  }
  return d.toLocaleDateString("en-US", { month: "short", day: "numeric", hour: "numeric" });
}

function AnalyticsContent() {
  const [data, setData] = useState<AnalyticsData | null>(null);
  const [loading, setLoading] = useState(true);
  const [range, setRange] = useState<"7d" | "30d">("7d");

  const fetchData = useCallback(async () => {
    setLoading(true);
    const res = await api.get<AnalyticsData>("/v1/analytics", { range });
    if (res.success && res.data) {
      setData(res.data);
    }
    setLoading(false);
  }, [range]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  const statCards = data
    ? [
        { label: "Inbound", value: data.summary.total_inbound, icon: MailOpen, color: "text-blue-500" },
        { label: "Outbound", value: data.summary.total_outbound, icon: Mail, color: "text-emerald-500" },
        { label: "Bounces", value: data.summary.bounces, icon: AlertTriangle, color: "text-amber-500" },
        { label: "Avg Size", value: formatSize(data.summary.avg_size_bytes), icon: ArrowUpDown, color: "text-purple-500" },
      ]
    : [];

  return (
    <PageShell
      title="Analytics"
      description="Email traffic overview and trends"
      actions={
        <div className="flex items-center rounded-lg border border-border bg-background/50 p-0.5">
          {(["7d", "30d"] as const).map((r) => (
            <button
              key={r}
              onClick={() => setRange(r)}
              className={`px-3 py-1 text-[12px] rounded-md transition-colors ${
                range === r
                  ? "bg-accent text-foreground font-medium"
                  : "text-muted-foreground hover:text-foreground"
              }`}
            >
              {r === "7d" ? "7 Days" : "30 Days"}
            </button>
          ))}
        </div>
      }
    >
      {/* Summary Cards */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        {loading
          ? Array.from({ length: 4 }).map((_, i) => (
              <Card key={i}>
                <CardContent className="pt-4 pb-3 px-4">
                  <Skeleton className="h-4 w-20 mb-2" />
                  <Skeleton className="h-7 w-16" />
                </CardContent>
              </Card>
            ))
          : statCards.map((card) => (
              <Card key={card.label}>
                <CardContent className="pt-4 pb-3 px-4">
                  <div className="flex items-center gap-1.5 mb-1">
                    <card.icon className={`h-3.5 w-3.5 ${card.color}`} />
                    <span className="text-[11px] text-muted-foreground font-medium uppercase tracking-wider">
                      {card.label}
                    </span>
                  </div>
                  <p className="text-xl font-semibold tabular-nums">{card.value}</p>
                </CardContent>
              </Card>
            ))}
      </div>

      {/* Volume Chart */}
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-[13px] font-medium">Email Volume</CardTitle>
        </CardHeader>
        <CardContent>
          {loading ? (
            <Skeleton className="h-[280px] w-full" />
          ) : (
            <ResponsiveContainer width="100%" height={280}>
              <AreaChart data={data?.time_series || []}>
                <defs>
                  <linearGradient id="inboundGrad" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="hsl(217, 91%, 60%)" stopOpacity={0.2} />
                    <stop offset="95%" stopColor="hsl(217, 91%, 60%)" stopOpacity={0} />
                  </linearGradient>
                  <linearGradient id="outboundGrad" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="hsl(160, 84%, 39%)" stopOpacity={0.2} />
                    <stop offset="95%" stopColor="hsl(160, 84%, 39%)" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" />
                <XAxis
                  dataKey="time"
                  tickFormatter={(t) => formatTimeLabel(t, range)}
                  tick={{ fontSize: 11 }}
                  stroke="hsl(var(--muted-foreground))"
                />
                <YAxis tick={{ fontSize: 11 }} stroke="hsl(var(--muted-foreground))" />
                <Tooltip
                  contentStyle={{
                    fontSize: 12,
                    backgroundColor: "hsl(var(--card))",
                    border: "1px solid hsl(var(--border))",
                    borderRadius: 8,
                  }}
                  labelFormatter={(t) => formatTimeLabel(t as string, range)}
                />
                <Area
                  type="monotone"
                  dataKey="inbound"
                  stroke="hsl(217, 91%, 60%)"
                  fill="url(#inboundGrad)"
                  strokeWidth={2}
                />
                <Area
                  type="monotone"
                  dataKey="outbound"
                  stroke="hsl(160, 84%, 39%)"
                  fill="url(#outboundGrad)"
                  strokeWidth={2}
                />
              </AreaChart>
            </ResponsiveContainer>
          )}
        </CardContent>
      </Card>

      {/* Top Domains & Top Senders */}
      <div className="grid md:grid-cols-2 gap-4">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-[13px] font-medium">Top Domains</CardTitle>
          </CardHeader>
          <CardContent>
            {loading ? (
              <Skeleton className="h-[220px] w-full" />
            ) : data?.top_domains.length ? (
              <ResponsiveContainer width="100%" height={220}>
                <BarChart data={data.top_domains} layout="vertical" margin={{ left: 0 }}>
                  <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" />
                  <XAxis type="number" tick={{ fontSize: 11 }} stroke="hsl(var(--muted-foreground))" />
                  <YAxis
                    dataKey="domain"
                    type="category"
                    tick={{ fontSize: 11 }}
                    width={120}
                    stroke="hsl(var(--muted-foreground))"
                  />
                  <Tooltip
                    contentStyle={{
                      fontSize: 12,
                      backgroundColor: "hsl(var(--card))",
                      border: "1px solid hsl(var(--border))",
                      borderRadius: 8,
                    }}
                  />
                  <Bar dataKey="count" fill="hsl(217, 91%, 60%)" radius={[0, 4, 4, 0]} />
                </BarChart>
              </ResponsiveContainer>
            ) : (
              <p className="text-[13px] text-muted-foreground py-8 text-center">No data</p>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-[13px] font-medium">Top Senders</CardTitle>
          </CardHeader>
          <CardContent>
            {loading ? (
              <Skeleton className="h-[220px] w-full" />
            ) : data?.top_senders.length ? (
              <ResponsiveContainer width="100%" height={220}>
                <BarChart data={data.top_senders} layout="vertical" margin={{ left: 0 }}>
                  <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" />
                  <XAxis type="number" tick={{ fontSize: 11 }} stroke="hsl(var(--muted-foreground))" />
                  <YAxis
                    dataKey="email"
                    type="category"
                    tick={{ fontSize: 11 }}
                    width={140}
                    stroke="hsl(var(--muted-foreground))"
                  />
                  <Tooltip
                    contentStyle={{
                      fontSize: 12,
                      backgroundColor: "hsl(var(--card))",
                      border: "1px solid hsl(var(--border))",
                      borderRadius: 8,
                    }}
                  />
                  <Bar dataKey="count" fill="hsl(160, 84%, 39%)" radius={[0, 4, 4, 0]} />
                </BarChart>
              </ResponsiveContainer>
            ) : (
              <p className="text-[13px] text-muted-foreground py-8 text-center">No data</p>
            )}
          </CardContent>
        </Card>
      </div>
    </PageShell>
  );
}

export default function Page() {
  return (
    <AuthGuard>
      <AnalyticsContent />
    </AuthGuard>
  );
}
