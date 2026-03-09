"use client";

import { useEffect } from "react";
import { useRouter, usePathname } from "next/navigation";
import { useAuthStore } from "@/lib/auth-store";
import { TwoFactorInput } from "@/components/auth/two-factor-input";
import { MailShell } from "@/components/mail/mail-shell";

export default function MailLayout({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const pathname = usePathname();
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const requires2FA = useAuthStore((s) => s.requires2FA);
  const verifyTwoFactor = useAuthStore((s) => s.verifyTwoFactor);
  const isLoading = useAuthStore((s) => s.isLoading);

  useEffect(() => {
    if (!isAuthenticated && !requires2FA) {
      router.push("/login");
    }
  }, [isAuthenticated, requires2FA, router]);

  if (requires2FA) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-background">
        <div className="w-full max-w-sm space-y-6 px-4">
          <div className="text-center">
            <h1 className="font-sans text-2xl font-semibold tracking-tight">
              Two-factor authentication
            </h1>
            <p className="mt-2 text-sm text-muted-foreground">
              Enter the 6-digit code from your authenticator app
            </p>
          </div>
          <TwoFactorInput
            onComplete={async (code) => {
              await verifyTwoFactor(code);
            }}
            isLoading={isLoading}
          />
        </div>
      </div>
    );
  }

  if (!isAuthenticated) {
    return null;
  }

  // Settings page renders its own layout
  if (pathname === "/settings") {
    return <div className="h-screen overflow-hidden bg-background">{children}</div>;
  }

  return <MailShell />;
}
