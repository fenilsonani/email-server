"use client";

import { useEffect, useState } from "react";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { api } from "@/lib/api";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { ArrowLeft, Shield, Clock, CheckCircle, XCircle, RefreshCw, Download } from "lucide-react";
import Link from "next/link";

interface Certificate {
  domain: string;
  issuer: string;
  expires_at: string;
  is_valid: boolean;
  auto_renew: boolean;
}

function getDaysUntilExpiry(expiresAt: string): number {
  const now = new Date();
  const expiry = new Date(expiresAt);
  return Math.floor((expiry.getTime() - now.getTime()) / (1000 * 60 * 60 * 24));
}

function ExpiryBadge({ expiresAt }: { expiresAt: string }) {
  const days = getDaysUntilExpiry(expiresAt);

  if (days < 0) {
    return (
      <Badge variant="outline" className="text-[10px] border-red-500/30 text-red-600 bg-red-500/10">
        Expired
      </Badge>
    );
  }
  if (days < 30) {
    return (
      <Badge variant="outline" className="text-[10px] border-amber-500/30 text-amber-600 bg-amber-500/10">
        {days}d remaining
      </Badge>
    );
  }
  return (
    <Badge variant="outline" className="text-[10px] border-green-500/30 text-green-600 bg-green-500/10">
      {days}d remaining
    </Badge>
  );
}

function PageContent() {
  const [certs, setCerts] = useState<Certificate[]>([]);
  const [loading, setLoading] = useState(true);
  const [renewing, setRenewing] = useState(false);

  const fetchCerts = async () => {
    const res = await api.get<Certificate[]>("/v1/system/certificates");
    if (res.success && res.data) {
      setCerts(res.data);
    }
    setLoading(false);
  };

  useEffect(() => {
    fetchCerts();
  }, []);

  const handleRenew = async () => {
    setRenewing(true);
    try {
      const res = await api.post("/v1/system/certificates/renew");
      if (res.success) {
        toast.success("Certificate renewal initiated");
        await fetchCerts();
      } else {
        toast.error(res.error || "Failed to renew certificates");
      }
    } catch {
      toast.error("Failed to renew certificates");
    } finally {
      setRenewing(false);
    }
  };

  if (loading) {
    return (
      <PageShell
        title="TLS Certificates"
        actions={
          <Link href="/system/">
            <Button variant="ghost" size="icon-xs">
              <ArrowLeft className="h-4 w-4" />
            </Button>
          </Link>
        }
      >
        <div className="space-y-3">
          {Array.from({ length: 2 }).map((_, i) => (
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
      title="TLS Certificates"
      description="Manage TLS certificates for your mail server"
      actions={
        <div className="flex items-center gap-2">
          <Button
            size="sm"
            className="h-8 text-[12px] gap-1.5"
            onClick={handleRenew}
            disabled={renewing}
          >
            <RefreshCw className={`h-3.5 w-3.5 ${renewing ? "animate-spin" : ""}`} />
            {renewing ? "Renewing..." : "Renew Certificates"}
          </Button>
          <Link href="/system/">
            <Button variant="ghost" size="icon-xs">
              <ArrowLeft className="h-4 w-4" />
            </Button>
          </Link>
        </div>
      }
    >
      {certs.length === 0 ? (
        <div className="rounded-lg border border-border bg-card p-8 text-center">
          <Shield className="h-8 w-8 text-muted-foreground/30 mx-auto mb-3" />
          <p className="text-[13px] text-muted-foreground">No certificates found</p>
        </div>
      ) : (
        <div className="space-y-3">
          {certs.map((cert) => (
            <div key={cert.domain} className="rounded-lg border border-border bg-card p-4">
              <div className="flex items-start justify-between gap-4">
                <div className="min-w-0 space-y-2">
                  <div className="flex items-center gap-2">
                    <Shield className="h-3.5 w-3.5 text-muted-foreground/40 shrink-0" strokeWidth={1.5} />
                    <span className="text-[13px] font-medium">{cert.domain}</span>
                    {cert.is_valid ? (
                      <Badge variant="outline" className="text-[10px] border-green-500/30 text-green-600 bg-green-500/10 gap-1">
                        <CheckCircle className="h-2.5 w-2.5" />
                        Valid
                      </Badge>
                    ) : (
                      <Badge variant="outline" className="text-[10px] border-red-500/30 text-red-600 bg-red-500/10 gap-1">
                        <XCircle className="h-2.5 w-2.5" />
                        Invalid
                      </Badge>
                    )}
                  </div>

                  <div className="flex items-center gap-4 flex-wrap">
                    <div className="flex items-center gap-1.5">
                      <span className="text-[11px] text-muted-foreground/60">Issuer:</span>
                      <span className="text-[12px] text-muted-foreground">{cert.issuer}</span>
                    </div>
                    <div className="flex items-center gap-1.5">
                      <Clock className="h-3 w-3 text-muted-foreground/40" strokeWidth={1.5} />
                      <span className="text-[11px] text-muted-foreground/60">Expires:</span>
                      <span className="text-[12px] text-muted-foreground">
                        {new Date(cert.expires_at).toLocaleDateString()}
                      </span>
                      <ExpiryBadge expiresAt={cert.expires_at} />
                    </div>
                    {cert.auto_renew && (
                      <Badge variant="outline" className="text-[10px]">
                        Auto-renew
                      </Badge>
                    )}
                  </div>
                </div>
              </div>
              <Button
                variant="outline"
                size="sm"
                className="h-7 text-[11px] gap-1 shrink-0"
                onClick={async () => {
                  const res = await fetch(`${process.env.NEXT_PUBLIC_API_URL || "/admin/api"}/v1/system/certificates/${encodeURIComponent(cert.domain)}/download`, { credentials: "include" });
                  if (res.ok) {
                    const blob = await res.blob();
                    const a = document.createElement("a"); a.href = URL.createObjectURL(blob); a.download = `${cert.domain}.pem`; a.click(); URL.revokeObjectURL(a.href);
                    toast.success("Certificate downloaded");
                  } else {
                    toast.error("Failed to download certificate");
                  }
                }}
              >
                <Download className="h-3 w-3" />Download
              </Button>
            </div>
          ))}
        </div>
      )}
    </PageShell>
  );
}

export default function Page() {
  return <AuthGuard><PageContent /></AuthGuard>;
}
