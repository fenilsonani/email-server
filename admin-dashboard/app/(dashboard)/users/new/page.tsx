"use client";

import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import { PageShell } from "@/components/shared/page-shell";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { ArrowLeft, Loader2 } from "lucide-react";
import { toast } from "sonner";
import Link from "next/link";

interface DomainOption {
  id: number;
  name: string;
}

function CreateUserContent() {
  const [domains, setDomains] = useState<DomainOption[]>([]);
  const [loading, setLoading] = useState(false);

  const [username, setUsername] = useState("");
  const [domainId, setDomainId] = useState<number | "">("");
  const [password, setPassword] = useState("");
  const [isAdmin, setIsAdmin] = useState(false);

  const [errors, setErrors] = useState<Record<string, string>>({});

  useEffect(() => {
    api.get<DomainOption[]>("/v1/domains-list").then((res) => {
      if (res.success && res.data) {
        setDomains(res.data);
        if (res.data.length > 0) setDomainId(res.data[0].id);
      }
    });
  }, []);

  const validate = () => {
    const newErrors: Record<string, string> = {};
    if (!username.trim()) newErrors.username = "Username is required";
    else if (!/^[a-zA-Z0-9._-]+$/.test(username))
      newErrors.username =
        "Username can only contain letters, numbers, dots, hyphens, and underscores";
    if (!domainId) newErrors.domain = "Please select a domain";
    if (!password) newErrors.password = "Password is required";
    else if (password.length < 8)
      newErrors.password = "Password must be at least 8 characters";
    setErrors(newErrors);
    return Object.keys(newErrors).length === 0;
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!validate()) return;

    setLoading(true);
    const res = await api.post("/v1/users", {
      username: username.trim(),
      domain_id: domainId,
      password,
      is_admin: isAdmin,
    });

    if (res.success) {
      toast.success("User created successfully");
      window.location.href = "/admin/users/";
    } else {
      toast.error(res.error || "Failed to create user");
    }
    setLoading(false);
  };

  const selectedDomain = domains.find((d) => d.id === domainId);

  return (
    <PageShell title="Create User" description="Add a new email account">
      <div className="max-w-lg">
        <Link href="/users/" className="inline-flex items-center gap-1.5 text-[12px] text-muted-foreground/60 hover:text-foreground transition-colors mb-4">
          <ArrowLeft className="h-3.5 w-3.5" />Back to Users
        </Link>
      </div>

      <Card className="max-w-lg">
        <CardHeader className="pb-3">
          <CardTitle className="text-[13px] font-medium">
            Account Details
          </CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="space-y-1.5">
              <Label className="text-[13px]">Username</Label>
              <div className="flex items-center gap-1.5">
                <Input
                  value={username}
                  onChange={(e) => {
                    setUsername(e.target.value);
                    if (errors.username)
                      setErrors((prev) => ({ ...prev, username: "" }));
                  }}
                  placeholder="john"
                  className="text-[13px]"
                  aria-invalid={!!errors.username}
                />
                <span className="text-[13px] text-muted-foreground shrink-0">
                  @
                </span>
                <select
                  value={domainId}
                  onChange={(e) => {
                    setDomainId(Number(e.target.value));
                    if (errors.domain)
                      setErrors((prev) => ({ ...prev, domain: "" }));
                  }}
                  className="h-8 rounded-lg border border-input bg-transparent px-2.5 text-[13px] text-foreground outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30"
                >
                  <option value="">Select domain</option>
                  {domains.map((d) => (
                    <option key={d.id} value={d.id}>
                      {d.name}
                    </option>
                  ))}
                </select>
              </div>
              {errors.username && (
                <p className="text-[12px] text-destructive">{errors.username}</p>
              )}
              {errors.domain && (
                <p className="text-[12px] text-destructive">{errors.domain}</p>
              )}
              {username && selectedDomain && (
                <p className="text-[12px] text-muted-foreground">
                  Email: {username}@{selectedDomain.name}
                </p>
              )}
            </div>

            <div className="space-y-1.5">
              <Label className="text-[13px]">Password</Label>
              <Input
                type="password"
                value={password}
                onChange={(e) => {
                  setPassword(e.target.value);
                  if (errors.password)
                    setErrors((prev) => ({ ...prev, password: "" }));
                }}
                placeholder="Minimum 8 characters"
                className="text-[13px]"
                aria-invalid={!!errors.password}
              />
              {errors.password && (
                <p className="text-[12px] text-destructive">{errors.password}</p>
              )}
            </div>

            <div className="flex items-center justify-between py-1">
              <div>
                <Label className="text-[13px]">Administrator</Label>
                <p className="text-[12px] text-muted-foreground mt-0.5">
                  Admins can manage users, domains, and server settings
                </p>
              </div>
              <Switch
                checked={isAdmin}
                onCheckedChange={setIsAdmin}
                size="sm"
              />
            </div>

            <div className="flex items-center gap-2 pt-2">
              <Button
                type="submit"
                size="sm"
                className="text-[13px]"
                disabled={loading}
              >
                {loading && <Loader2 className="h-4 w-4 animate-spin" />}
                Create User
              </Button>
              <Link href="/users/">
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
      <CreateUserContent />
  );
}
