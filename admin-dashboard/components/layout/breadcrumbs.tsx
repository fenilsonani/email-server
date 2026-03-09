"use client";

import { usePathname } from "next/navigation";
import Link from "next/link";
import { ChevronRight } from "lucide-react";

const labelMap: Record<string, string> = {
  analytics: "Analytics",
  users: "Users",
  domains: "Domains",
  lists: "Mailing Lists",
  features: "Features",
  queue: "Queue",
  security: "Security",
  api: "API Overview",
  "api-keys": "API Keys",
  webhooks: "Webhooks",
  templates: "Templates",
  emails: "Send Logs",
  logs: "Logs",
  sieve: "Mail Filters",
  tools: "Tools",
  system: "System",
  settings: "Organization",
  new: "New",
  blocklist: "Blocklist",
  greylist: "Greylist",
  "failed-logins": "Failed Logins",
  auth: "Auth",
  delivery: "Delivery",
  audit: "Audit",
  dns: "DNS Check",
  "test-email": "Test Email",
  doctor: "Doctor",
  backup: "Backup",
  certificates: "Certificates",
  "2fa": "Two-Factor",
  update: "Update",
  dkim: "DKIM",
  aliases: "Aliases",
  preferences: "Preferences",
  scheduled: "Scheduled",
  screener: "Screener",
  snoozed: "Snoozed",
  vip: "VIP",
  members: "Members",
  archives: "Archives",
  moderation: "Moderation",
};

export function Breadcrumbs() {
  const pathname = usePathname();

  // Strip trailing slash and split
  const segments = pathname.replace(/\/$/, "").split("/").filter(Boolean);
  if (segments.length === 0) return null;

  // Build crumbs, skip numeric/uuid segments (dynamic [id] params)
  const crumbs: { label: string; href: string }[] = [];
  let href = "";
  for (const seg of segments) {
    href += `/${seg}`;
    // Skip dynamic IDs
    if (/^[0-9a-f-]{8,}$/i.test(seg) || /^\d+$/.test(seg)) continue;
    const label = labelMap[seg] || seg.charAt(0).toUpperCase() + seg.slice(1).replace(/-/g, " ");
    crumbs.push({ label, href: href + "/" });
  }

  if (crumbs.length <= 1) return null;

  return (
    <nav aria-label="Breadcrumb" className="flex items-center gap-1 text-[12px]">
      {crumbs.map((crumb, i) => {
        const isLast = i === crumbs.length - 1;
        return (
          <span key={crumb.href} className="flex items-center gap-1">
            {i > 0 && <ChevronRight className="h-3 w-3 text-muted-foreground/30" />}
            {isLast ? (
              <span className="text-muted-foreground/70 font-medium">{crumb.label}</span>
            ) : (
              <Link
                href={crumb.href}
                className="text-muted-foreground/50 hover:text-foreground transition-colors"
              >
                {crumb.label}
              </Link>
            )}
          </span>
        );
      })}
    </nav>
  );
}
