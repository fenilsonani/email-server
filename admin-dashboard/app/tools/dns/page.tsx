"use client";

import { useState } from "react";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { api } from "@/lib/api";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Search, Globe, CheckCircle, XCircle, AlertTriangle } from "lucide-react";

interface DNSResult {
  record_type: string;
  status: string;
  expected: string;
  actual: string;
  message: string;
}

function PageContent() {
  const [domain, setDomain] = useState("");
  const [checking, setChecking] = useState(false);
  const [results, setResults] = useState<DNSResult[] | null>(null);

  const handleCheck = async () => {
    if (!domain.trim()) return;
    setChecking(true);
    try {
      const res = await api.get<{ domain: string; results: DNSResult[] }>("/v1/tools/dns-check", { domain: domain.trim() });
      if (res.success && res.data) {
        setResults(res.data.results);
      }
    } finally {
      setChecking(false);
    }
  };

  const statusIcon = (status: string) => {
    switch (status) {
      case "pass": return <CheckCircle className="h-3.5 w-3.5 text-emerald-500/70 shrink-0" strokeWidth={2} />;
      case "warning": return <AlertTriangle className="h-3.5 w-3.5 text-amber-500/70 shrink-0" strokeWidth={2} />;
      case "fail": return <XCircle className="h-3.5 w-3.5 text-destructive/70 shrink-0" strokeWidth={2} />;
      default: return <AlertTriangle className="h-3.5 w-3.5 text-amber-500/70 shrink-0" strokeWidth={2} />;
    }
  };

  return (
    <PageShell title="DNS Checker" description="Verify mail server DNS configuration for a domain">
      <div className="flex items-center gap-2 max-w-md">
        <div className="relative flex-1">
          <Globe className="absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground/50" strokeWidth={1.5} />
          <Input
            placeholder="example.com"
            value={domain}
            onChange={(e) => setDomain(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && handleCheck()}
            className="h-8 pl-8 text-[12px] bg-background/50 border-border placeholder:text-muted-foreground/40"
          />
        </div>
        <Button onClick={handleCheck} disabled={checking || !domain.trim()} size="sm" className="h-8 text-[12px] gap-1.5">
          <Search className="h-3.5 w-3.5" />
          {checking ? "Checking..." : "Check"}
        </Button>
      </div>

      {results && (
        <div className="rounded-lg border border-border overflow-hidden divide-y divide-border max-w-2xl">
          {results.map((result) => (
            <div key={result.record_type} className="flex items-start gap-3 px-4 py-3 activity-row">
              {statusIcon(result.status)}
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2 mb-0.5">
                  <span className="text-[13px] font-medium">{result.record_type}</span>
                </div>
                <p className="text-[12px] text-muted-foreground">{result.message}</p>
                {result.actual && (
                  <p className="text-[12px] text-muted-foreground/70 font-mono break-all mt-0.5">{result.actual}</p>
                )}
                {result.status !== "pass" && result.expected && (
                  <p className="text-[11px] text-amber-500/70 mt-1">Expected: {result.expected}</p>
                )}
              </div>
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
