"use client";

import { useState } from "react";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { api } from "@/lib/api";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Search, Globe, CheckCircle, XCircle, AlertTriangle, Copy, Check } from "lucide-react";
import { toast } from "sonner";

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
  const [copiedField, setCopiedField] = useState<string | null>(null);

  const copyValue = (value: string, label: string) => {
    navigator.clipboard.writeText(value);
    setCopiedField(label);
    toast.success("Copied to clipboard");
    setTimeout(() => setCopiedField(null), 2000);
  };

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
                  <div className="flex items-start gap-1.5 mt-0.5">
                    <p className="text-[12px] text-muted-foreground/70 font-mono break-all flex-1">{result.actual}</p>
                    <button
                      onClick={() => copyValue(result.actual, `actual-${result.record_type}`)}
                      className="shrink-0 mt-0.5 text-muted-foreground/40 hover:text-foreground transition-colors"
                      title="Copy actual value"
                    >
                      {copiedField === `actual-${result.record_type}` ? <Check className="h-3 w-3 text-emerald-500" /> : <Copy className="h-3 w-3" />}
                    </button>
                  </div>
                )}
                {result.status !== "pass" && result.expected && (
                  <div className="flex items-start gap-1.5 mt-1 rounded-md bg-amber-500/5 border border-amber-500/20 px-2 py-1.5">
                    <p className="text-[11px] text-amber-600 font-mono break-all flex-1">
                      <span className="text-amber-500/70 font-sans">Expected: </span>{result.expected}
                    </p>
                    <button
                      onClick={() => copyValue(result.expected, `expected-${result.record_type}`)}
                      className="shrink-0 mt-0.5 text-amber-500/50 hover:text-amber-600 transition-colors"
                      title="Copy expected value"
                    >
                      {copiedField === `expected-${result.record_type}` ? <Check className="h-3 w-3 text-emerald-500" /> : <Copy className="h-3 w-3" />}
                    </button>
                  </div>
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
