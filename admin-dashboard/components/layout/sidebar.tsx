"use client";

import { usePathname } from "next/navigation";
import Link from "next/link";
import { cn } from "@/lib/utils";
import {
  LayoutDashboard,
  Users,
  Globe,
  ListTodo,
  Mail,
  Settings,
  Wrench,
  Shield,
  MailOpen,
  FileText,
  Sparkles,
  List,
  Server,
  FileCode,
  HardDrive,
  BarChart3,
  ShieldAlert,
} from "lucide-react";

const navGroups = [
  {
    label: "Overview",
    items: [
      { label: "Dashboard", href: "/", icon: LayoutDashboard },
      { label: "Analytics", href: "/analytics/", icon: BarChart3 },
    ],
  },
  {
    label: "Management",
    items: [
      { label: "Users", href: "/users/", icon: Users },
      { label: "Domains", href: "/domains/", icon: Globe },
      { label: "Mailing Lists", href: "/lists/", icon: List },
    ],
  },
  {
    label: "Monitoring",
    items: [
      { label: "Auth Logs", href: "/logs/auth/", icon: Shield },
      { label: "Delivery Logs", href: "/logs/delivery/", icon: MailOpen },
      { label: "Audit Logs", href: "/logs/audit/", icon: FileText },
      { label: "Queue", href: "/queue/", icon: ListTodo },
    ],
  },
  {
    label: "Features",
    items: [
      { label: "Features", href: "/features/", icon: Sparkles },
    ],
  },
  {
    label: "Tools",
    items: [
      { label: "Sieve Filters", href: "/sieve/", icon: FileCode },
      { label: "DNS Check", href: "/tools/dns/", icon: Globe },
      { label: "Test Email", href: "/tools/test-email/", icon: Mail },
      { label: "Doctor", href: "/tools/doctor/", icon: Wrench },
    ],
  },
  {
    label: "Security",
    items: [
      { label: "Security", href: "/security/", icon: ShieldAlert },
    ],
  },
  {
    label: "System",
    items: [
      { label: "System", href: "/system/", icon: Settings },
      { label: "Backup", href: "/system/backup/", icon: HardDrive },
    ],
  },
];

export function AdminSidebar() {
  const pathname = usePathname();

  return (
    <aside className="hidden md:flex w-52 shrink-0 flex-col border-r border-border bg-sidebar">
      {/* Logo */}
      <div className="flex items-center gap-2 px-3 pt-3 pb-2">
        <div className="flex h-7 w-7 items-center justify-center rounded-md bg-primary/10">
          <Server className="h-3.5 w-3.5 text-primary" strokeWidth={2} />
        </div>
        <span className="text-[13px] font-semibold tracking-tight text-foreground">Mail Admin</span>
      </div>

      {/* Navigation */}
      <nav className="flex-1 overflow-y-auto scrollbar-none pt-2 px-2 space-y-4">
        {navGroups.map((group) => (
          <div key={group.label}>
            <p className="px-2 mb-0.5 text-[10px] font-medium uppercase tracking-widest text-muted-foreground/50">
              {group.label}
            </p>
            <div className="space-y-px">
              {group.items.map((item) => {
                const isActive = pathname === item.href ||
                  (item.href !== "/" && pathname.startsWith(item.href));
                return (
                  <Link
                    key={item.href}
                    href={item.href}
                    className={cn(
                      "flex items-center gap-2 rounded-md px-2 py-1.5 text-[13px] transition-colors duration-100",
                      isActive
                        ? "bg-accent text-foreground font-medium"
                        : "text-muted-foreground hover:bg-accent/50 hover:text-foreground"
                    )}
                  >
                    <item.icon
                      className={cn(
                        "h-4 w-4 shrink-0",
                        isActive ? "text-foreground" : "text-muted-foreground/60"
                      )}
                      strokeWidth={isActive ? 2 : 1.5}
                    />
                    <span>{item.label}</span>
                  </Link>
                );
              })}
            </div>
          </div>
        ))}
      </nav>

      {/* Bottom spacer */}
      <div className="h-2" />
    </aside>
  );
}
