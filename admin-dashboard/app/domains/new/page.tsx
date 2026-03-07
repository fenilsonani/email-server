"use client";

import { useState } from "react";
import Link from "next/link";
import { AuthGuard } from "@/components/layout/auth-guard";
import { api } from "@/lib/api";
import { PageShell } from "@/components/shared/page-shell";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ArrowLeft, Loader2 } from "lucide-react";
import { toast } from "sonner";

function CreateDomainContent() {
  const [loading, setLoading] = useState(false);

  const [name, setName] = useState("");
  const [mailHostname, setMailHostname] = useState("");
  const [autoFill, setAutoFill] = useState(true);
  const [errors, setErrors] = useState<Record<string, string>>({});

  const handleNameChange = (value: string) => {
    setName(value);
    if (errors.name) setErrors((prev) => ({ ...prev, name: "" }));
    if (autoFill) {
      setMailHostname(value ? `mail.${value}` : "");
    }
  };

  const handleHostnameChange = (value: string) => {
    setMailHostname(value);
    setAutoFill(false);
    if (errors.mailHostname)
      setErrors((prev) => ({ ...prev, mailHostname: "" }));
  };

  const validate = () => {
    const newErrors: Record<string, string> = {};
    if (!name.trim()) newErrors.name = "Domain name is required";
    else if (!/^[a-zA-Z0-9][a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$/.test(name.trim()))
      newErrors.name = "Enter a valid domain name (e.g., example.com)";
    setErrors(newErrors);
    return Object.keys(newErrors).length === 0;
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!validate()) return;

    setLoading(true);
    const body: Record<string, string> = { name: name.trim() };
    if (mailHostname.trim()) body.mail_hostname = mailHostname.trim();

    const res = await api.post("/v1/domains", body);
    if (res.success) {
      toast.success(`Domain ${name} created successfully`);
      window.location.href = "/admin/domains/";
    } else {
      toast.error(res.error || "Failed to create domain");
    }
    setLoading(false);
  };

  return (
    <PageShell title="Add Domain" description="Configure a new mail domain">
      <div className="max-w-lg">
        <Link href="/domains/" className="inline-flex items-center gap-1.5 text-[12px] text-muted-foreground/60 hover:text-foreground transition-colors mb-4">
          <ArrowLeft className="h-3.5 w-3.5" />Back to Domains
        </Link>
      </div>

      <Card className="max-w-lg">
        <CardHeader className="pb-3">
          <CardTitle className="text-[13px] font-medium">
            Domain Details
          </CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="space-y-1.5">
              <Label className="text-[13px]">Domain Name</Label>
              <Input
                value={name}
                onChange={(e) => handleNameChange(e.target.value)}
                placeholder="example.com"
                className="text-[13px]"
                aria-invalid={!!errors.name}
              />
              {errors.name && (
                <p className="text-[12px] text-destructive">{errors.name}</p>
              )}
            </div>

            <div className="space-y-1.5">
              <Label className="text-[13px]">Mail Hostname</Label>
              <Input
                value={mailHostname}
                onChange={(e) => handleHostnameChange(e.target.value)}
                placeholder="mail.example.com"
                className="text-[13px]"
              />
              <p className="text-[12px] text-muted-foreground">
                The hostname used for MX records and mail delivery
              </p>
            </div>

            <div className="flex items-center gap-2 pt-2">
              <Button
                type="submit"
                size="sm"
                className="text-[13px]"
                disabled={loading}
              >
                {loading && <Loader2 className="h-4 w-4 animate-spin" />}
                Create Domain
              </Button>
              <Link href="/domains/">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="text-[13px]"
                >
                  Cancel
                </Button>
              </Link>
            </div>
          </form>
        </CardContent>
      </Card>
    </PageShell>
  );
}

export default function Page() {
  return (
    <AuthGuard>
      <CreateDomainContent />
    </AuthGuard>
  );
}
