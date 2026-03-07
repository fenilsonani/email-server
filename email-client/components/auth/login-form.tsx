"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useAuthStore } from "@/lib/auth-store";
import { TwoFactorInput } from "./two-factor-input";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Fingerprint, Loader2 } from "lucide-react";
import type { AuthMethod } from "@/lib/types";

function GoogleIcon() {
  return (
    <svg viewBox="0 0 24 24" className="h-5 w-5" aria-hidden="true">
      <path
        d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92a5.06 5.06 0 0 1-2.2 3.32v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.1z"
        fill="#4285F4"
      />
      <path
        d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z"
        fill="#34A853"
      />
      <path
        d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z"
        fill="#FBBC05"
      />
      <path
        d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z"
        fill="#EA4335"
      />
    </svg>
  );
}

export function LoginForm() {
  const router = useRouter();
  const { login, verifyTwoFactor, requires2FA, isLoading } = useAuthStore();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");

  const handleLogin = async (method: AuthMethod) => {
    await login(method);
    if (!useAuthStore.getState().requires2FA) {
      router.push("/mail/inbox");
    }
  };

  const handle2FA = async (code: string) => {
    const ok = await verifyTwoFactor(code);
    if (ok) router.push("/mail/inbox");
  };

  if (requires2FA) {
    return (
      <div className="w-full max-w-sm space-y-6 px-4">
        <div className="text-center">
          <h1 className="font-sans text-2xl font-semibold tracking-tight">Two-factor authentication</h1>
          <p className="mt-2 text-sm text-muted-foreground">
            Enter the 6-digit code from your authenticator app
          </p>
        </div>
        <TwoFactorInput onComplete={handle2FA} isLoading={isLoading} />
      </div>
    );
  }

  return (
    <div className="w-full max-w-sm space-y-6 px-4">
      <div className="text-center">
        <h1 className="font-sans text-2xl font-semibold tracking-tight">Veil</h1>
        <p className="mt-1 text-sm text-muted-foreground">Sign in to your account</p>
      </div>

      <form
        onSubmit={(e) => {
          e.preventDefault();
          handleLogin("email");
        }}
        className="space-y-3"
      >
        <Input
          type="email"
          placeholder="Email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          className="h-10"
          autoComplete="email"
        />
        <Input
          type="password"
          placeholder="Password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          className="h-10"
          autoComplete="current-password"
        />
        <Button
          type="submit"
          disabled={isLoading}
          className="w-full h-10 bg-indigo-600 hover:bg-indigo-700 text-white"
        >
          {isLoading ? <Loader2 className="h-4 w-4 animate-spin" /> : "Sign in"}
        </Button>
      </form>

      <div className="relative">
        <div className="absolute inset-0 flex items-center">
          <div className="w-full border-t border-border" />
        </div>
        <div className="relative flex justify-center text-xs">
          <span className="bg-background px-2 text-muted-foreground">or</span>
        </div>
      </div>

      <div className="space-y-2">
        <Button
          variant="outline"
          disabled={isLoading}
          onClick={() => handleLogin("google")}
          className="w-full h-10 gap-2"
        >
          <GoogleIcon />
          Sign in with Google
        </Button>
        <Button
          variant="ghost"
          disabled={isLoading}
          onClick={() => handleLogin("passkey")}
          className="w-full h-10 gap-2 text-muted-foreground"
        >
          <Fingerprint className="h-4 w-4" />
          Sign in with Passkey
        </Button>
      </div>
    </div>
  );
}
