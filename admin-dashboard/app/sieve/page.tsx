"use client";

import { useEffect, useState } from "react";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { api } from "@/lib/api";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { FileCode, Save, CheckCircle, XCircle, BookOpen } from "lucide-react";

interface SieveData {
  script: string;
  updated_at: string;
}

interface ValidationResult {
  valid: boolean;
  errors?: string[];
}

const sieveTemplates = [
  {
    name: "Spam Filter",
    description: "Move spam-flagged messages to Junk folder",
    script: `require ["fileinto"];

# Move spam to Junk folder
if header :contains "X-Spam-Flag" "YES" {
  fileinto "Junk";
}

keep;`,
  },
  {
    name: "Subject Sorting",
    description: "Sort emails into folders based on subject keywords",
    script: `require ["fileinto"];

# Sort newsletters
if header :contains "Subject" "[newsletter]" {
  fileinto "Newsletters";
}

# Sort invoices
if header :contains "Subject" "invoice" {
  fileinto "Finance";
}

keep;`,
  },
  {
    name: "Vacation Auto-Reply",
    description: "Send automatic out-of-office replies",
    script: `require ["vacation"];

vacation
  :days 7
  :subject "Out of Office"
  "I am currently out of the office and will return on Monday.
I will respond to your email when I return.

Thank you.";`,
  },
  {
    name: "Forward Mail",
    description: "Redirect copies of incoming mail to another address",
    script: `require ["copy"];

# Forward a copy to another address
redirect :copy "backup@example.com";

keep;`,
  },
  {
    name: "Reject by Sender",
    description: "Reject emails from specific senders",
    script: `require ["reject"];

# Reject emails from specific senders
if address :is "from" "spammer@example.com" {
  reject "Your message has been rejected.";
}

keep;`,
  },
  {
    name: "Large Attachment Filter",
    description: "File large messages into a separate folder",
    script: `require ["fileinto"];

# Move large messages (over 10MB) to a separate folder
if size :over 10M {
  fileinto "Large Messages";
}

keep;`,
  },
];

function PageContent() {
  const [script, setScript] = useState("");
  const [updatedAt, setUpdatedAt] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [validating, setValidating] = useState(false);
  const [validation, setValidation] = useState<ValidationResult | null>(null);
  const [showTemplates, setShowTemplates] = useState(false);

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
            onClick={() => setShowTemplates(!showTemplates)}
          >
            <BookOpen className="h-3.5 w-3.5" />
            Templates
          </Button>
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
      {/* Template Library */}
      {showTemplates && (
        <Card className="rounded-lg border border-border bg-card">
          <CardHeader className="pb-3">
            <CardTitle className="flex items-center gap-2 text-[14px] font-medium">
              <BookOpen className="h-4 w-4 text-muted-foreground/60" />
              Template Library
            </CardTitle>
            <p className="text-[12px] text-muted-foreground/60 mt-1">
              Click a template to insert it into the editor. This will replace the current script.
            </p>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
              {sieveTemplates.map((tpl) => (
                <button
                  key={tpl.name}
                  onClick={() => {
                    setScript(tpl.script);
                    setValidation(null);
                    setShowTemplates(false);
                    toast.success(`Loaded "${tpl.name}" template`);
                  }}
                  className="text-left rounded-lg border border-border p-3 hover:bg-accent/50 transition-colors"
                >
                  <span className="text-[13px] font-medium block">{tpl.name}</span>
                  <span className="text-[11px] text-muted-foreground/60 block mt-0.5">{tpl.description}</span>
                </button>
              ))}
            </div>
          </CardContent>
        </Card>
      )}
    </PageShell>
  );
}

export default function Page() {
  return <AuthGuard><PageContent /></AuthGuard>;
}
