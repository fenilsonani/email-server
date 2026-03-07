"use client";

import { useEffect, useState } from "react";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { api } from "@/lib/api";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { FileCode, Save, CheckCircle, XCircle } from "lucide-react";

interface SieveData {
  script: string;
  updated_at: string;
}

interface ValidationResult {
  valid: boolean;
  errors?: string[];
}

function PageContent() {
  const [script, setScript] = useState("");
  const [updatedAt, setUpdatedAt] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [validating, setValidating] = useState(false);
  const [validation, setValidation] = useState<ValidationResult | null>(null);

  useEffect(() => {
    const fetchScript = async () => {
      const res = await api.get<SieveData>("/v1/sieve");
      if (res.success && res.data) {
        setScript(res.data.script);
        setUpdatedAt(res.data.updated_at);
      }
      setLoading(false);
    };
    fetchScript();
  }, []);

  const handleSave = async () => {
    setSaving(true);
    try {
      const res = await api.put("/v1/sieve", { script });
      if (res.success) {
        toast.success("Sieve script saved");
        setValidation(null);
      } else {
        toast.error(res.error || "Failed to save script");
      }
    } catch {
      toast.error("Failed to save script");
    } finally {
      setSaving(false);
    }
  };

  const handleValidate = async () => {
    setValidating(true);
    setValidation(null);
    try {
      const res = await api.post<ValidationResult>("/v1/sieve/validate", { script });
      if (res.success && res.data) {
        setValidation(res.data);
        if (res.data.valid) {
          toast.success("Script is valid");
        } else {
          toast.error("Script has errors");
        }
      } else {
        toast.error(res.error || "Validation failed");
      }
    } catch {
      toast.error("Validation failed");
    } finally {
      setValidating(false);
    }
  };

  if (loading) {
    return (
      <PageShell title="Sieve Filters">
        <div className="space-y-4">
          <Skeleton className="h-4 w-64" />
          <Skeleton className="h-[400px] w-full" />
        </div>
      </PageShell>
    );
  }

  return (
    <PageShell
      title="Sieve Filters"
      description="Edit email filtering rules using the Sieve scripting language"
      actions={
        <div className="flex items-center gap-2">
          <Button
            size="sm"
            className="h-8 text-[12px] gap-1.5"
            variant="outline"
            onClick={handleValidate}
            disabled={validating}
          >
            <CheckCircle className="h-3.5 w-3.5" />
            {validating ? "Validating..." : "Validate"}
          </Button>
          <Button
            size="sm"
            className="h-8 text-[12px] gap-1.5"
            onClick={handleSave}
            disabled={saving}
          >
            <Save className="h-3.5 w-3.5" />
            {saving ? "Saving..." : "Save Script"}
          </Button>
        </div>
      }
    >
      <Card className="rounded-lg border border-border bg-card">
        <CardHeader className="pb-3">
          <div className="flex items-center justify-between">
            <CardTitle className="flex items-center gap-2 text-[14px] font-medium">
              <FileCode className="h-4 w-4 text-muted-foreground/60" />
              Sieve Script
            </CardTitle>
            {updatedAt && (
              <span className="text-[11px] text-muted-foreground/50">
                Last updated: {new Date(updatedAt).toLocaleString()}
              </span>
            )}
          </div>
          <p className="text-[12px] text-muted-foreground/60 mt-1">
            Sieve is a language for filtering email messages at time of final delivery.
          </p>
        </CardHeader>
        <CardContent>
          <textarea
            value={script}
            onChange={(e) => { setScript(e.target.value); setValidation(null); }}
            className="w-full font-mono text-[13px] bg-muted/30 border-border rounded-lg p-4 min-h-[400px] resize-y border focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 focus:ring-offset-background"
            placeholder={`# Example sieve script\nrequire ["fileinto", "reject"];\n\n# Reject spam\nif header :contains "X-Spam-Flag" "YES" {\n  fileinto "Junk";\n}\n\n# Keep everything else\nkeep;`}
            spellCheck={false}
          />
        </CardContent>
      </Card>

      {/* Validation Results */}
      {validation && (
        <Card className="rounded-lg border border-border bg-card">
          <CardContent className="pt-4">
            {validation.valid ? (
              <div className="flex items-center gap-2 text-green-600">
                <CheckCircle className="h-4 w-4" />
                <span className="text-[13px] font-medium">Script is valid</span>
              </div>
            ) : (
              <div className="space-y-2">
                <div className="flex items-center gap-2 text-red-600">
                  <XCircle className="h-4 w-4" />
                  <span className="text-[13px] font-medium">Script has errors</span>
                </div>
                {validation.errors && validation.errors.length > 0 && (
                  <ul className="space-y-1 ml-6">
                    {validation.errors.map((err, i) => (
                      <li key={i} className="text-[12px] text-red-500 font-mono list-disc">
                        {err}
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            )}
          </CardContent>
        </Card>
      )}
    </PageShell>
  );
}

export default function Page() {
  return <AuthGuard><PageContent /></AuthGuard>;
}
