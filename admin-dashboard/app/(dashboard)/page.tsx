"use client";

import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import { useAuthStore } from "@/lib/auth-store";
import type { DashboardStats } from "@/lib/types";
import Link from "next/link";
import { Users, Globe, Mail, Clock, CheckCircle2, XCircle, Server, UserPlus, BarChart3, Search, FileText, AlertTriangle, ShieldAlert, Inbox } from "lucide-react";
import { Skeleton } from "@/components/ui/skeleton";
import { formatDistanceToNow } from "date-fns";

function StatCard({ label, value, icon: Icon }: { label: string; value: string | number; icon: React.ComponentType<{ className?: string; strokeWidth?: number }> }) {
  return (
    <div className="stat-card rounded-lg border border-border bg-card p-3.5">
      <div className="flex items-center justify-between">
        <p className="text-[11px] text-muted-foreground/70 font-medium uppercase tracking-wider">{label}</p>
        <Icon className="h-3.5 w-3.5 text-muted-foreground/40" strokeWidth={1.5} />
      </div>
      <p className="text-xl font-semibold tracking-tight mt-1.5 text-foreground">{value}</p>
    </div>
  );
}

const quickActions = [
  { label: "Add User", href: "/users/new/", icon: UserPlus },
  { label: "View Analytics", href: "/analytics/", icon: BarChart3 },
  { label: "Check DNS", href: "/tools/dns/", icon: Search },
  { label: "View Logs", href: "/logs/auth/", icon: FileText },
];

function Dashboard() {
  const [stats, setStats] = useState<DashboardStats | null>(null);
  const [loading, setLoading] = useState(true);
  const username = useAuthStore((s) => s.username);

  useEffect(() => {
    api.get<DashboardStats>("/v1/stats").then((res) => {
      if (res.success && res.data) setStats(res.data);
      setLoading(false);
    });
  }, []);

  if (loading) {
    return (
      <div className="p-5 space-y-5">
        <Skeleton className="h-7 w-48" />
        <div className="grid gap-3 grid-cols-2 lg:grid-cols-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <div key={i} className="rounded-lg border border-border bg-card p-3.5">
              <Skeleton className="h-3 w-16 mb-3" />
              <Skeleton className="h-6 w-12" />
            </div>
          ))}
        </div>
        <Skeleton className="h-20 rounded-lg" />
        <Skeleton className="h-64 rounded-lg" />
      </div>
    );
  }

  if (!stats) return null;

  const statCards = [
    { label: "Users", value: stats.total_users, icon: Users },
    { label: "Domains", value: stats.total_domains, icon: Globe },
    { label: "Messages", value: stats.total_messages.toLocaleString(), icon: Mail },
    { label: "Uptime", value: stats.uptime_human, icon: Clock },
  ];

  return (
    <div className="p-5 space-y-5">
      {/* Welcome header */}
      <div className="flex items-center justify-between">
        <h1 className="text-[15px] font-semibold tracking-tight">
          Welcome back{username ? `, ${username}` : ""}
        </h1>
        <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground/60">
          <Server className="h-3 w-3" strokeWidth={1.5} />
          <span className="font-mono">{stats.server_hostname}</span>
        </div>
      </div>

      {/* Stat grid */}
      <div className="grid gap-3 grid-cols-2 lg:grid-cols-4">
        {statCards.map((card) => (
          <StatCard key={card.label} label={card.label} value={card.value} icon={card.icon} />
        ))}
      </div>

      {/* Health Alerts */}
      {(() => {
        const alerts: { icon: typeof AlertTriangle; label: string; value: string; color: string; href: string }[] = [];
        if (stats.failed_logins_24h > 0) {
          alerts.push({
            icon: ShieldAlert,
            label: "Failed logins (24h)",
            value: String(stats.failed_logins_24h),
            color: stats.failed_logins_24h > 10 ? "border-destructive/40 bg-destructive/5" : "border-amber-500/40 bg-amber-500/5",
            href: "/logs/auth/",
          });
        }
        if (stats.queue_failed > 0) {
          alerts.push({
            icon: Inbox,
            label: "Failed in queue",
            value: String(stats.queue_failed),
            color: "border-destructive/40 bg-destructive/5",
            href: "/queue/",
          });
        }
        if (stats.bounced_24h > 0) {
          alerts.push({
            icon: AlertTriangle,
            label: "Bounced/failed emails (24h)",
            value: String(stats.bounced_24h),
            color: stats.bounced_24h > 20 ? "border-destructive/40 bg-destructive/5" : "border-amber-500/40 bg-amber-500/5",
            href: "/logs/delivery/",
          });
        }
        if (stats.queue_pending > 5) {
          alerts.push({
            icon: Clock,
            label: "Pending in queue",
            value: String(stats.queue_pending),
            color: "border-amber-500/40 bg-amber-500/5",
            href: "/queue/",
          });
        }
        if (alerts.length === 0) return null;
        return (
          <div className="space-y-2">
            {alerts.map((alert) => (
              <Link key={alert.label} href={alert.href} className={`flex items-center gap-3 rounded-lg border p-3 transition-colors hover:bg-accent/30 ${alert.color}`}>
                <alert.icon className="h-4 w-4 shrink-0 text-current opacity-70" strokeWidth={2} />
                <span className="text-[13px] flex-1">{alert.label}</span>
                <span className="text-[15px] font-semibold tabular-nums">{alert.value}</span>
              </Link>
            ))}
          </div>
        );
      })()}

      {/* Quick actions */}
      <div>
        <h2 className="text-[13px] font-medium text-foreground mb-2">Quick Actions</h2>
        <div className="grid gap-3 grid-cols-2 lg:grid-cols-4">
          {quickActions.map((action) => (
            <Link
              key={action.href}
              href={action.href}
              className="flex items-center gap-2.5 rounded-lg border border-border bg-card p-3 text-[13px] text-muted-foreground hover:bg-accent/50 hover:text-foreground transition-colors"
            >
              <action.icon className="h-4 w-4 shrink-0" strokeWidth={1.5} />
              {action.label}
            </Link>
          ))}
        </div>
      </div>

      {/* Recent activity */}
      {stats.recent_activity?.length > 0 && (
        <div className="rounded-lg border border-border bg-card">
          <div className="px-4 py-3 border-b border-border">
            <h2 className="text-[13px] font-medium text-foreground">Recent Activity</h2>
          </div>
          <div className="divide-y divide-border">
            {stats.recent_activity.map((item, i) => (
              <div key={i} className="activity-row flex items-center gap-3 px-4 py-2.5">
                {item.status === "success" ? (
                  <CheckCircle2 className="h-3.5 w-3.5 shrink-0 text-emerald-500/70" strokeWidth={2} />
                ) : (
                  <XCircle className="h-3.5 w-3.5 shrink-0 text-destructive/70" strokeWidth={2} />
                )}
                <span className="text-[13px] text-foreground/90 truncate flex-1 min-w-0">
                  {item.description}
                </span>
                <span className="text-[11px] text-muted-foreground/50 shrink-0 font-mono tabular-nums">
                  {formatDistanceToNow(new Date(item.time), { addSuffix: true })}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

export default function DashboardPage() {
  return (
      <Dashboard />
  );
}
