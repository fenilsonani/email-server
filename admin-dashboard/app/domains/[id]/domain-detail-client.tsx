"use client";

import { useEffect, useState } from "react";
import { useRouteId } from "@/lib/use-route-id";
import Link from "next/link";
import { AuthGuard } from "@/components/layout/auth-guard";
import { api } from "@/lib/api";
import type { Domain } from "@/lib/types";
import { PageShell } from "@/components/shared/page-shell";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Separator } from "@/components/ui/separator";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import {
  ArrowLeft,
  Trash2,
  KeyRound,
  Shield,
  CheckCircle2,
  XCircle,
  Globe,
  Users,
  RefreshCw,
  Loader2,
  Copy,
  Check,
} from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";

interface DKIMInfo {
  enabled: boolean;
  selector: string;
  public_key: string;
  dns_record: string;
}

interface DNSRecord {
  type: string;
  name: string;
  expected: string;
  actual: string;
  status: "pass" | "fail" | "warning";
}

interface DNSCheck {
  records: DNSRecord[];
  checked_at: string;
}

function DomainDetailContent() {
  const domainId = useRouteId("domains");

  const [domain, setDomain] = useState<Domain | null>(null);
  const [dkim, setDkim] = useState<DKIMInfo | null>(null);
  const [dns, setDns] = useState<DNSCheck | null>(null);
  const [loading, setLoading] = useState(true);
  const [dnsLoading, setDnsLoading] = useState(false);
  const [dkimGenerating, setDkimGenerating] = useState(false);
  const [showDelete, setShowDelete] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [copiedField, setCopiedField] = useState<string | null>(null);

  const copyValue = (text: string, field: string) => {
    navigator.clipboard.writeText(text);
    setCopiedField(field);
    toast.success("Copied to clipboard");
    setTimeout(() => setCopiedField(null), 2000);
  };

  useEffect(() => {
    Promise.all([
      api.get<Domain>(`/v1/domains/${domainId}`),
      api.get<DKIMInfo>(`/v1/domains/${domainId}/dkim`),
      api.get<DNSCheck>(`/v1/domains/${domainId}/dns`),
    ]).then(([domainRes, dkimRes, dnsRes]) => {
      if (domainRes.success && domainRes.data) {
        setDomain(domainRes.data);
      } else {
        toast.error("Domain not found");
        window.location.href = "/admin/domains/";
        return;
      }
      if (dkimRes.success && dkimRes.data) setDkim(dkimRes.data);
      if (dnsRes.success && dnsRes.data) setDns(dnsRes.data);
      setLoading(false);
    });
  }, [domainId]);

  const generateDKIM = async () => {
    setDkimGenerating(true);
    try {
      const res = await api.post(`/v1/domains/${domainId}/dkim/generate`, {
        selector: "mail",
        bits: 2048,
        storage: "database",
      });
      if (res.success) {
        toast.success("DKIM key generated");
        // Refresh DKIM info
        const dkimRes = await api.get<DKIMInfo>(`/v1/domains/${domainId}/dkim`);
        if (dkimRes.success && dkimRes.data) setDkim(dkimRes.data);
      } else {
        toast.error(res.error || "Failed to generate DKIM key");
      }
    } catch {
      toast.error("Failed to generate DKIM key");
    } finally {
      setDkimGenerating(false);
    }
  };

  const refreshDNS = async () => {
    setDnsLoading(true);
    const res = await api.get<DNSCheck>(`/v1/domains/${domainId}/dns`);
    if (res.success && res.data) {
      setDns(res.data);
      toast.success("DNS check refreshed");
    } else {
      toast.error("Failed to refresh DNS check");
    }
    setDnsLoading(false);
  };

  const handleDelete = async () => {
    setDeleting(true);
    const res = await api.delete(`/v1/domains/${domainId}`);
    if (res.success) {
      toast.success(`Domain ${domain?.name} deleted`);
      window.location.href = "/admin/domains/";
    } else {
      toast.error(res.error || "Failed to delete domain");
    }
    setDeleting(false);
  };

  const statusIcon = (status: string) => {
    if (status === "pass")
      return <CheckCircle2 className="h-4 w-4 text-green-500" />;
    if (status === "warning")
      return <XCircle className="h-4 w-4 text-amber-500" />;
    return <XCircle className="h-4 w-4 text-destructive" />;
  };

  const statusBadge = (status: string) => {
    if (status === "pass")
      return (
        <Badge
          variant="outline"
          className="text-[10px] text-green-500 border-green-500/30"
        >
          Pass
        </Badge>
      );
    if (status === "warning")
      return (
        <Badge
          variant="outline"
          className="text-[10px] text-amber-500 border-amber-500/30"
        >
          Warning
        </Badge>
      );
    return (
      <Badge variant="destructive" className="text-[10px]">
        Fail
      </Badge>
    );
  };

  if (loading) {
    return (
      <PageShell title="Domain">
        <div className="max-w-2xl space-y-4">
          <Skeleton className="h-8 w-48" />
          <Skeleton className="h-40 w-full" />
          <Skeleton className="h-40 w-full" />
        </div>
      </PageShell>
    );
  }

  if (!domain) return null;

  return (
    <PageShell
      title={domain.name}
      description="Domain configuration and DNS status"
      actions={
        domain.is_primary ? (
          <Badge variant="default" className="text-[10px]"><Shield className="h-3 w-3" />Primary</Badge>
        ) : undefined
      }
    >
      <div className="max-w-2xl">
        <Link href="/domains/" className="inline-flex items-center gap-1.5 text-[12px] text-muted-foreground/60 hover:text-foreground transition-colors mb-4">
          <ArrowLeft className="h-3.5 w-3.5" />Back to Domains
        </Link>
      </div>

      {/* Domain Info */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-[13px] font-medium flex items-center gap-2">
            <Globe className="h-4 w-4" />
            Domain Information
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-2 gap-4 text-[13px]">
            <div>
              <p className="text-muted-foreground">Domain Name</p>
              <p className="font-medium mt-0.5">{domain.name}</p>
            </div>
            <div>
              <p className="text-muted-foreground">Mail Hostname</p>
              <p className="font-medium mt-0.5">{domain.mail_hostname}</p>
            </div>
            <div>
              <p className="text-muted-foreground">Users</p>
              <p className="font-medium mt-0.5 flex items-center gap-1">
                <Users className="h-3 w-3" />
                {domain.user_count}
              </p>
            </div>
            <div>
              <p className="text-muted-foreground">Created</p>
              <p className="font-medium mt-0.5">
                {formatDistanceToNow(new Date(domain.created_at), {
                  addSuffix: true,
                })}
              </p>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* DKIM */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-[13px] font-medium flex items-center gap-2">
            <KeyRound className="h-4 w-4" />
            DKIM Configuration
          </CardTitle>
        </CardHeader>
        <CardContent>
          {dkim ? (
            <div className="space-y-3 text-[13px]">
              <div className="flex items-center gap-2">
                <span className="text-muted-foreground">Status:</span>
                {dkim.enabled ? (
                  <Badge
                    variant="outline"
                    className="text-[10px] text-green-500 border-green-500/30"
                  >
                    <CheckCircle2 className="h-3 w-3" />
                    Enabled
                  </Badge>
                ) : (
                  <Badge variant="destructive" className="text-[10px]">
                    Not configured
                  </Badge>
                )}
              </div>
              {dkim.enabled && dkim.selector && (
                <div>
                  <p className="text-muted-foreground">Selector</p>
                  <p className="font-mono text-[12px] mt-0.5 bg-muted/50 px-2 py-1 rounded">
                    {dkim.selector}
                  </p>
                </div>
              )}
              {dkim.enabled && dkim.dns_record && (
                <div>
                  <p className="text-muted-foreground">DNS TXT Record</p>
                  <div className="flex items-start gap-2 mt-0.5 bg-muted/50 px-2 py-1.5 rounded">
                    <p className="font-mono text-[11px] break-all leading-relaxed flex-1">
                      {dkim.selector}._domainkey.{domain.name} IN TXT &quot;{dkim.dns_record}&quot;
                    </p>
                    <button
                      onClick={() => copyValue(`${dkim.selector}._domainkey.${domain.name} IN TXT "${dkim.dns_record}"`, "dkim-record")}
                      className="shrink-0 p-0.5 text-muted-foreground hover:text-foreground transition-colors mt-0.5"
                    >
                      {copiedField === "dkim-record" ? (
                        <Check className="h-3 w-3 text-green-500" />
                      ) : (
                        <Copy className="h-3 w-3" />
                      )}
                    </button>
                  </div>
                </div>
              )}
              {!dkim.enabled && (
                <Button
                  variant="outline"
                  size="sm"
                  className="text-[12px] gap-1.5"
                  onClick={generateDKIM}
                  disabled={dkimGenerating}
                >
                  {dkimGenerating ? (
                    <><Loader2 className="h-3.5 w-3.5 animate-spin" />Generating...</>
                  ) : (
                    <><KeyRound className="h-3.5 w-3.5" />Generate DKIM Key</>
                  )}
                </Button>
              )}
            </div>
          ) : (
            <div className="space-y-3">
              <p className="text-[13px] text-muted-foreground">
                No DKIM key configured for this domain.
              </p>
              <Button
                variant="outline"
                size="sm"
                className="text-[12px] gap-1.5"
                onClick={generateDKIM}
                disabled={dkimGenerating}
              >
                {dkimGenerating ? (
                  <><Loader2 className="h-3.5 w-3.5 animate-spin" />Generating...</>
                ) : (
                  <><KeyRound className="h-3.5 w-3.5" />Generate DKIM Key</>
                )}
              </Button>
            </div>
          )}
        </CardContent>
      </Card>

      {/* DNS Check */}
      <Card>
        <CardHeader className="pb-3">
          <div className="flex items-center justify-between">
            <CardTitle className="text-[13px] font-medium flex items-center gap-2">
              <Globe className="h-4 w-4" />
              DNS Records
            </CardTitle>
            <Button
              variant="outline"
              size="sm"
              className="text-[13px]"
              onClick={refreshDNS}
              disabled={dnsLoading}
            >
              <RefreshCw
                className={`h-3 w-3 ${dnsLoading ? "animate-spin" : ""}`}
              />
              Refresh
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          {dns && dns.records && dns.records.length > 0 ? (
            <div className="space-y-3">
              {dns.records.map((record, i) => (
                <div
                  key={i}
                  className="flex items-start gap-3 py-2 border-b border-border last:border-0"
                >
                  <div className="mt-0.5">{statusIcon(record.status)}</div>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="text-[13px] font-medium">
                        {record.type}
                      </span>
                      {statusBadge(record.status)}
                    </div>
                    {record.name && (
                      <p className="text-[12px] text-muted-foreground mt-0.5">
                        {record.name}
                      </p>
                    )}
                    <div className="mt-1 space-y-0.5">
                      <div className="flex items-start gap-1 text-[12px]">
                        <span className="text-muted-foreground shrink-0">Expected: </span>
                        <span className="font-mono break-all flex-1">
                          {record.expected}
                        </span>
                        <button
                          onClick={() => copyValue(record.expected, `expected-${i}`)}
                          className="shrink-0 p-0.5 text-muted-foreground hover:text-foreground transition-colors"
                          title="Copy expected value"
                        >
                          {copiedField === `expected-${i}` ? (
                            <Check className="h-3 w-3 text-green-500" />
                          ) : (
                            <Copy className="h-3 w-3" />
                          )}
                        </button>
                      </div>
                      {record.actual && (
                        <p className="text-[12px]">
                          <span className="text-muted-foreground">Found: </span>
                          <span className="font-mono break-all">
                            {record.actual}
                          </span>
                        </p>
                      )}
                      {record.status === "fail" && (
                        <p className="text-[11px] text-amber-500 mt-1">
                          Add the expected record to your DNS provider to fix this.
                        </p>
                      )}
                    </div>
                  </div>
                </div>
              ))}
              {dns.checked_at && (
                <p className="text-[11px] text-muted-foreground pt-1">
                  Last checked{" "}
                  {formatDistanceToNow(new Date(dns.checked_at), {
                    addSuffix: true,
                  })}
                </p>
              )}
            </div>
          ) : (
            <p className="text-[13px] text-muted-foreground">
              No DNS check results available. Click Refresh to run a check.
            </p>
          )}
        </CardContent>
      </Card>

      {/* Danger Zone */}
      {!domain.is_primary && (
        <>
          <Separator />
          <Card className="border-destructive/30">
            <CardHeader className="pb-3">
              <CardTitle className="text-[13px] font-medium text-destructive">
                Danger Zone
              </CardTitle>
            </CardHeader>
            <CardContent>
              <div className="flex items-center justify-between">
                <div>
                  <p className="text-[13px] font-medium">Delete this domain</p>
                  <p className="text-[12px] text-muted-foreground mt-0.5">
                    Permanently remove this domain and its configuration
                  </p>
                </div>
                <Button
                  variant="destructive"
                  size="sm"
                  className="text-[13px]"
                  onClick={() => setShowDelete(true)}
                  disabled={domain.user_count > 0}
                >
                  <Trash2 className="h-4 w-4" />
                  Delete
                </Button>
              </div>
              {domain.user_count > 0 && (
                <p className="text-[12px] text-muted-foreground mt-2">
                  You must remove all {domain.user_count} user
                  {domain.user_count !== 1 ? "s" : ""} before deleting this
                  domain.
                </p>
              )}
            </CardContent>
          </Card>
        </>
      )}

      <Dialog open={showDelete} onOpenChange={setShowDelete}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Domain</DialogTitle>
            <DialogDescription className="text-[13px]">
              Are you sure you want to delete <strong>{domain.name}</strong>?
              This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              size="sm"
              className="text-[13px]"
              onClick={() => setShowDelete(false)}
            >
              Cancel
            </Button>
            <Button
              variant="destructive"
              size="sm"
              className="text-[13px]"
              onClick={handleDelete}
              disabled={deleting}
            >
              {deleting ? "Deleting..." : "Delete Domain"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </PageShell>
  );
}

export default function DomainDetailClient() {
  return (
    <AuthGuard>
      <DomainDetailContent />
    </AuthGuard>
  );
}
