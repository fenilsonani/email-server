"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useAuthStore } from "@/lib/auth-store";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Loader2, Server, Lock } from "lucide-react";

export default function LoginPage() {
  const router = useRouter();
  const login = useAuthStore((s) => s.login);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setLoading(true);

    try {
      const result = await login(username, password);
      if (result.needs_2fa) {
        router.push("/verify-2fa/");
      } else {
        router.push("/");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Login failed");
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="flex min-h-screen items-center justify-center bg-background relative">
      {/* Subtle radial gradient behind the card */}
      <div className="absolute inset-0 overflow-hidden">
        <div className="absolute left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 w-[600px] h-[600px] rounded-full bg-primary/[0.04] blur-[100px]" />
      </div>

      <div className="relative w-full max-w-[360px] px-4">
        {/* Logo + title */}
        <div className="flex flex-col items-center gap-3 mb-8">
          <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-card border border-border">
            <Server className="h-4.5 w-4.5 text-foreground" strokeWidth={1.5} />
          </div>
          <div className="text-center">
            <h1 className="text-[15px] font-semibold tracking-tight text-foreground">Mail Server</h1>
            <p className="text-[12px] text-muted-foreground mt-0.5">Administration panel</p>
          </div>
        </div>

        {/* Login card */}
        <div className="rounded-xl border border-border bg-card p-5 login-glow">
          <form onSubmit={handleSubmit} className="space-y-4">
            {error && (
              <div className="rounded-md border border-destructive/20 bg-destructive/5 px-3 py-2 text-[12px] text-destructive">
                {error}
              </div>
            )}

            <div className="space-y-1.5">
              <Label htmlFor="username" className="text-[12px] text-muted-foreground font-normal">
                Username
              </Label>
              <Input
                id="username"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                placeholder="admin"
                className="h-9 text-[13px] bg-background/50 border-border placeholder:text-muted-foreground/40"
                autoFocus
                required
              />
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="password" className="text-[12px] text-muted-foreground font-normal">
                Password
              </Label>
              <Input
                id="password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="••••••••"
                className="h-9 text-[13px] bg-background/50 border-border placeholder:text-muted-foreground/40"
                required
              />
            </div>

            <Button
              type="submit"
              className="w-full h-9 text-[13px] font-medium mt-1"
              disabled={loading}
            >
              {loading ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                "Sign in"
              )}
            </Button>
          </form>
        </div>

        {/* Footer */}
        <div className="flex items-center justify-center gap-1.5 mt-5 text-[11px] text-muted-foreground/50">
          <Lock className="h-3 w-3" />
          <span>Rate limited &middot; 2FA available</span>
        </div>
      </div>
    </div>
  );
}
