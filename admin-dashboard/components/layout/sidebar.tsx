"use client";

import { useEffect, useState, useCallback } from "react";
import { usePathname, useRouter } from "next/navigation";
import Link from "next/link";
import { cn } from "@/lib/utils";
import { useAdvancedMode } from "@/lib/advanced-mode";
import { usePresetStore } from "@/lib/preset";
import {
  LayoutDashboard,
  Users,
  Globe,
  Mail,
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
  ListTodo,
  Settings,
  Key,
  Webhook,
  FileCode2,
  Send,
  Code,
  Building2,
  ChevronDown,
  Search,
} from "lucide-react";

interface NavItem {
  label: string;
  href: string;
  icon: React.ComponentType<{ className?: string; strokeWidth?: number }>;
  feature?: string;
}

interface NavGroup {
  label: string;
  items: NavItem[];
  advanced?: boolean;
  feature?: string;
}

const navGroups: NavGroup[] = [
  {
    label: "Home",
    items: [
      { label: "Dashboard", href: "/", icon: LayoutDashboard },
      { label: "Analytics", href: "/analytics/", icon: BarChart3 },
    ],
  },
  {
    label: "Manage",
    feature: "users",
    items: [
      { label: "Users", href: "/users/", icon: Users, feature: "users" },
      { label: "Domains", href: "/domains/", icon: Globe, feature: "domains" },
      { label: "Lists", href: "/lists/", icon: List, feature: "lists" },
      { label: "Features", href: "/features/", icon: Sparkles, feature: "features" },
    ],
  },
  {
    label: "Email",
    items: [
      { label: "Queue", href: "/queue/", icon: ListTodo, feature: "queue" },
      { label: "Security", href: "/security/", icon: ShieldAlert, feature: "security" },
    ],
  },
  {
    label: "Developer",
    feature: "api_keys",
    items: [
      { label: "API Overview", href: "/api/", icon: Code, feature: "api_keys" },
      { label: "API Keys", href: "/api-keys/", icon: Key, feature: "api_keys" },
      { label: "Webhooks", href: "/webhooks/", icon: Webhook, feature: "webhooks" },
      { label: "Templates", href: "/templates/", icon: FileCode2, feature: "templates" },
      { label: "Send Logs", href: "/emails/", icon: Send, feature: "send_logs" },
    ],
  },
  {
    label: "Advanced",
    advanced: true,
    items: [
      { label: "Auth Logs", href: "/logs/auth/", icon: Shield, feature: "logs" },
      { label: "Delivery Logs", href: "/logs/delivery/", icon: MailOpen, feature: "logs" },
      { label: "Audit Logs", href: "/logs/audit/", icon: FileText, feature: "logs" },
      { label: "Mail Filters", href: "/sieve/", icon: FileCode, feature: "sieve" },
      { label: "DNS Check", href: "/tools/dns/", icon: Globe },
      { label: "Test Email", href: "/tools/test-email/", icon: Mail },
      { label: "Doctor", href: "/tools/doctor/", icon: Wrench },
      { label: "Backup", href: "/system/backup/", icon: HardDrive },
    ],
  },
];

// All searchable pages (flat list for Cmd+K)
const allPages = [
  { label: "Dashboard", href: "/", group: "Home" },
  { label: "Analytics", href: "/analytics/", group: "Home" },
  { label: "Users", href: "/users/", group: "Manage" },
  { label: "Add User", href: "/users/new/", group: "Manage" },
  { label: "Domains", href: "/domains/", group: "Manage" },
  { label: "Add Domain", href: "/domains/new/", group: "Manage" },
  { label: "Mailing Lists", href: "/lists/", group: "Manage" },
  { label: "Features", href: "/features/", group: "Manage" },
  { label: "Message Queue", href: "/queue/", group: "Email" },
  { label: "Security", href: "/security/", group: "Email" },
  { label: "Blocklist", href: "/security/blocklist/", group: "Security" },
  { label: "Greylist", href: "/security/greylist/", group: "Security" },
  { label: "Failed Logins", href: "/security/failed-logins/", group: "Security" },
  { label: "API Overview", href: "/api/", group: "Developer" },
  { label: "API Keys", href: "/api-keys/", group: "Developer" },
  { label: "Webhooks", href: "/webhooks/", group: "Developer" },
  { label: "Templates", href: "/templates/", group: "Developer" },
  { label: "Send Logs", href: "/emails/", group: "Developer" },
  { label: "Auth Logs", href: "/logs/auth/", group: "Logs" },
  { label: "Delivery Logs", href: "/logs/delivery/", group: "Logs" },
  { label: "Audit Logs", href: "/logs/audit/", group: "Logs" },
  { label: "Mail Filters (Sieve)", href: "/sieve/", group: "Advanced" },
  { label: "DNS Checker", href: "/tools/dns/", group: "Tools" },
  { label: "Test Email", href: "/tools/test-email/", group: "Tools" },
  { label: "Doctor", href: "/tools/doctor/", group: "Tools" },
  { label: "Backup & Restore", href: "/system/backup/", group: "System" },
  { label: "TLS Certificates", href: "/system/certificates/", group: "System" },
  { label: "DKIM", href: "/system/dkim/", group: "System" },
  { label: "Two-Factor Auth", href: "/system/2fa/", group: "System" },
  { label: "System Update", href: "/system/update/", group: "System" },
  { label: "Organization", href: "/settings/", group: "Settings" },
  { label: "System Settings", href: "/system/", group: "Settings" },
];

export function AdminSidebar() {
  const pathname = usePathname();
  const router = useRouter();
  const { enabled: advancedEnabled, hydrate } = useAdvancedMode();
  const { currentOrg, orgs, loaded, load, switchOrg, hasFeature } = usePresetStore();
  const [searchOpen, setSearchOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");

  useEffect(() => {
    hydrate();
    load();
  }, [hydrate, load]);

  // Cmd+K to open search
  const handleKeyDown = useCallback((e: KeyboardEvent) => {
    if ((e.metaKey || e.ctrlKey) && e.key === "k") {
      e.preventDefault();
      setSearchOpen(prev => !prev);
      setSearchQuery("");
    }
    if (e.key === "Escape") setSearchOpen(false);
  }, []);

  useEffect(() => {
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [handleKeyDown]);

  const filteredPages = searchQuery.trim()
    ? allPages.filter(p => p.label.toLowerCase().includes(searchQuery.toLowerCase()) || p.group.toLowerCase().includes(searchQuery.toLowerCase()))
    : [];

  const visibleGroups = navGroups
    .filter((g) => !g.advanced || advancedEnabled)
    .filter((g) => !g.feature || hasFeature(g.feature))
    .map((g) => ({
      ...g,
      items: g.items.filter((item) => !item.feature || hasFeature(item.feature)),
    }))
    .filter((g) => g.items.length > 0);

  return (
    <aside className="hidden md:flex w-52 shrink-0 flex-col border-r border-border bg-sidebar">
      {/* Org switcher */}
      {loaded && currentOrg && orgs.length > 0 && (
        <div className="px-2 pt-2">
          <div className="relative group">
            <button className="flex items-center gap-2 w-full rounded-md px-2 py-1.5 hover:bg-accent/50 transition-colors">
              <div className="flex h-6 w-6 items-center justify-center rounded bg-primary/10 text-[11px] font-bold text-primary">
                {currentOrg.name.charAt(0).toUpperCase()}
              </div>
              <span className="text-[12px] font-medium truncate flex-1 text-left">{currentOrg.name}</span>
              {orgs.length > 1 && <ChevronDown className="h-3 w-3 text-muted-foreground/50" />}
            </button>
            {orgs.length > 1 && (
              <div className="absolute left-0 top-full mt-1 w-full rounded-md border border-border bg-popover shadow-lg opacity-0 invisible group-hover:opacity-100 group-hover:visible transition-all z-50">
                {orgs.map((o) => (
                  <button
                    key={o.id}
                    onClick={() => switchOrg(o.id)}
                    className={cn(
                      "flex items-center gap-2 w-full px-3 py-2 text-[12px] hover:bg-accent transition-colors first:rounded-t-md last:rounded-b-md",
                      o.id === currentOrg.id && "bg-accent/50 font-medium"
                    )}
                  >
                    <div className="flex h-5 w-5 items-center justify-center rounded bg-primary/10 text-[10px] font-bold text-primary">
                      {o.name.charAt(0).toUpperCase()}
                    </div>
                    {o.name}
                  </button>
                ))}
              </div>
            )}
          </div>
        </div>
      )}

      {/* Search trigger */}
      <div className="px-2 pt-2">
        <button
          onClick={() => { setSearchOpen(true); setSearchQuery(""); }}
          className="flex items-center gap-2 w-full rounded-md px-2 py-1.5 text-[12px] text-muted-foreground/50 hover:bg-accent/50 hover:text-muted-foreground transition-colors border border-border/50"
        >
          <Search className="h-3.5 w-3.5" />
          <span className="flex-1 text-left">Search...</span>
          <kbd className="text-[10px] bg-muted/50 px-1 rounded">&#8984;K</kbd>
        </button>
      </div>

      {/* Search modal */}
      {searchOpen && (
        <>
          <div className="fixed inset-0 z-50 bg-black/50" onClick={() => setSearchOpen(false)} />
          <div className="fixed left-1/2 top-[20%] -translate-x-1/2 z-50 w-full max-w-md rounded-lg border bg-popover shadow-lg">
            <div className="flex items-center gap-2 border-b px-3">
              <Search className="h-4 w-4 text-muted-foreground/50 shrink-0" />
              <input
                autoFocus
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && filteredPages.length > 0) {
                    router.push(filteredPages[0].href);
                    setSearchOpen(false);
                  }
                }}
                placeholder="Search pages..."
                className="flex-1 h-10 bg-transparent text-[13px] outline-none placeholder:text-muted-foreground/40"
              />
              <kbd className="text-[10px] text-muted-foreground/40 bg-muted/50 px-1.5 py-0.5 rounded">ESC</kbd>
            </div>
            <div className="max-h-64 overflow-y-auto p-1">
              {searchQuery.trim() && filteredPages.length === 0 && (
                <p className="text-[12px] text-muted-foreground/50 text-center py-6">No results</p>
              )}
              {filteredPages.map((page) => (
                <button
                  key={page.href}
                  onClick={() => { router.push(page.href); setSearchOpen(false); }}
                  className="flex items-center justify-between w-full rounded-md px-3 py-2 text-[13px] hover:bg-accent transition-colors"
                >
                  <span>{page.label}</span>
                  <span className="text-[10px] text-muted-foreground/40">{page.group}</span>
                </button>
              ))}
              {!searchQuery.trim() && (
                <p className="text-[12px] text-muted-foreground/50 text-center py-6">Type to search pages...</p>
              )}
            </div>
          </div>
        </>
      )}

      {/* Logo */}
      <div className="flex items-center gap-2 px-3 pt-2 pb-2">
        <div className="flex h-7 w-7 items-center justify-center rounded-md bg-primary/10">
          <Server className="h-3.5 w-3.5 text-primary" strokeWidth={2} />
        </div>
        <span className="text-[13px] font-semibold tracking-tight text-foreground">Mail Admin</span>
      </div>

      {/* Navigation */}
      <nav className="flex-1 overflow-y-auto scrollbar-none pt-2 px-2 space-y-4">
        {visibleGroups.map((group) => (
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

      {/* Settings — always visible at bottom */}
      <div className="border-t border-border px-2 py-2 space-y-px">
        <Link
          href="/settings/"
          className={cn(
            "flex items-center gap-2 rounded-md px-2 py-1.5 text-[13px] transition-colors duration-100",
            (pathname === "/settings/" || pathname.startsWith("/settings/"))
              ? "bg-accent text-foreground font-medium"
              : "text-muted-foreground hover:bg-accent/50 hover:text-foreground"
          )}
        >
          <Building2
            className={cn(
              "h-4 w-4 shrink-0",
              (pathname === "/settings/" || pathname.startsWith("/settings/"))
                ? "text-foreground"
                : "text-muted-foreground/60"
            )}
            strokeWidth={(pathname === "/settings/" || pathname.startsWith("/settings/")) ? 2 : 1.5}
          />
          <span>Organization</span>
        </Link>
        <Link
          href="/system/"
          className={cn(
            "flex items-center gap-2 rounded-md px-2 py-1.5 text-[13px] transition-colors duration-100",
            (pathname === "/system/" || pathname.startsWith("/system/"))
              ? "bg-accent text-foreground font-medium"
              : "text-muted-foreground hover:bg-accent/50 hover:text-foreground"
          )}
        >
          <Settings
            className={cn(
              "h-4 w-4 shrink-0",
              (pathname === "/system/" || pathname.startsWith("/system/"))
                ? "text-foreground"
                : "text-muted-foreground/60"
            )}
            strokeWidth={(pathname === "/system/" || pathname.startsWith("/system/")) ? 2 : 1.5}
          />
          <span>Settings</span>
        </Link>
      </div>
    </aside>
  );
}
