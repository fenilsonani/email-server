"use client";

import { useState } from "react";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Stethoscope, Loader2, CheckCircle, XCircle, AlertTriangle } from "lucide-react";
import { toast } from "sonner";

interface DiagnosticResult {
  name: string;
  status: "pass" | "warn" | "fail";
  message: string;
}

function PageContent() {
  const [running, setRunning] = useState(false);
  const [results, setResults] = useState<DiagnosticResult[] | null>(null);

  const runDiagnostics = async () => {
    setRunning(true);
    setResults(null);
    try {
      const res = await api.post<DiagnosticResult[]>("/v1/tools/doctor");
      if (res.success && res.data) {
        setResults(res.data);
        const failCount = res.data.filter((r) => r.status === "fail").length;
        const warnCount = res.data.filter((r) => r.status === "warn").length;
        if (failCount > 0) toast.error(`${failCount} failed, ${warnCount} warnings`);
        else if (warnCount > 0) toast.warning(`${warnCount} warnings`);
        else toast.success("All checks passed");
      } else {
        toast.error(res.error || "Failed to run diagnostics");
      }
    } catch {
      toast.error("Failed to run diagnostics");
    } finally {
      setRunning(false);
    }
  };

  const statusIcon = (status: DiagnosticResult["status"]) => {
    switch (status) {
      case "pass": return <CheckCircle className="h-3.5 w-3.5 text-emerald-500/70 shrink-0" strokeWidth={2} />;
      case "warn": return <AlertTriangle className="h-3.5 w-3.5 text-amber-500/70 shrink-0" strokeWidth={2} />;
      case "fail": return <XCircle className="h-3.5 w-3.5 text-destructive/70 shrink-0" strokeWidth={2} />;
    }
  };

  return (
    <PageShell title="Server Doctor" description="Run diagnostics to check mail server health">
      <Button onClick={runDiagnostics} disabled={running} size="sm" className="h-8 text-[12px] gap-1.5">
        {running ? (
          <><Loader2 className="h-3.5 w-3.5 animate-spin" />Running...</>
        ) : (
          <><Stethoscope className="h-3.5 w-3.5" />Run Diagnostics</>
        )}
      </Button>

      {results && (
        <>
          <div className="rounded-lg border border-border overflow-hidden divide-y divide-border max-w-2xl">
            {results.map((result, i) => (
              <div key={i} className="flex items-start gap-3 px-4 py-3 activity-row">
                {statusIcon(result.status)}
                <div className="flex-1 min-w-0">
                  <p className="text-[13px] font-medium">{result.name}</p>
                  <p className="text-[12px] text-muted-foreground/60 mt-0.5">{result.message}</p>
                </div>
              </div>
            ))}
          </div>

          <div className="flex items-center gap-4 text-[11px] text-muted-foreground/50">
            <span className="flex items-center gap-1">
              <CheckCircle className="h-3 w-3 text-emerald-500/70" />{results.filter((r) => r.status === "pass").length} passed
            </span>
            <span className="flex items-center gap-1">
              <AlertTriangle className="h-3 w-3 text-amber-500/70" />{results.filter((r) => r.status === "warn").length} warnings
            </span>
            <span className="flex items-center gap-1">
              <XCircle className="h-3 w-3 text-destructive/70" />{results.filter((r) => r.status === "fail").length} failed
            </span>
          </div>
        </>
      )}
    </PageShell>
  );
}

export default function Page() {
  return <AuthGuard><PageContent /></AuthGuard>;
}
