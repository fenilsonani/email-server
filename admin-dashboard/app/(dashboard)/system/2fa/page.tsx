"use client";

import { useEffect, useState } from "react";
import { PageShell } from "@/components/shared/page-shell";
import { api } from "@/lib/api";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { ArrowLeft, ShieldCheck, Smartphone, Key } from "lucide-react";
import Link from "next/link";

interface TwoFAStatus {
  enabled: boolean;
}

interface TwoFASetup {
  secret: string;
  qr_url: string;
}

function PageContent() {
  const [status, setStatus] = useState<TwoFAStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [setupData, setSetupData] = useState<TwoFASetup | null>(null);
  const [settingUp, setSettingUp] = useState(false);
  const [verifyCode, setVerifyCode] = useState("");
  const [verifying, setVerifying] = useState(false);
  const [disableCode, setDisableCode] = useState("");
  const [disabling, setDisabling] = useState(false);
  const [showDisable, setShowDisable] = useState(false);

  const fetchStatus = async () => {
    const res = await api.get<TwoFAStatus>("/v1/system/2fa/status");
    if (res.success && res.data) {
      setStatus(res.data);
    }
    setLoading(false);
  };

  useEffect(() => {
    fetchStatus();
  }, []);

  const handleSetup = async () => {
    setSettingUp(true);
    try {
      const res = await api.post<TwoFASetup>("/v1/system/2fa/setup");
      if (res.success && res.data) {
        setSetupData(res.data);
      } else {
        toast.error(res.error || "Failed to start 2FA setup");
      }
    } catch {
      toast.error("Failed to start 2FA setup");
    } finally {
      setSettingUp(false);
    }
  };

  const handleVerify = async () => {
    if (!verifyCode.trim()) {
      toast.error("Please enter a verification code");
      return;
    }
    setVerifying(true);
    try {
      const res = await api.post("/v1/system/2fa/verify", { code: verifyCode });
      if (res.success) {
        toast.success("Two-factor authentication enabled");
        setSetupData(null);
        setVerifyCode("");
        await fetchStatus();
      } else {
        toast.error(res.error || "Invalid verification code");
      }
    } catch {
      toast.error("Failed to verify code");
    } finally {
      setVerifying(false);
    }
  };

  const handleDisable = async () => {
    if (!disableCode.trim()) {
      toast.error("Please enter your 2FA code");
      return;
    }
    setDisabling(true);
    try {
      const res = await api.post("/v1/system/2fa/disable", { code: disableCode });
      if (res.success) {
        toast.success("Two-factor authentication disabled");
        setDisableCode("");
        setShowDisable(false);
        await fetchStatus();
      } else {
        toast.error(res.error || "Invalid code");
      }
    } catch {
      toast.error("Failed to disable 2FA");
    } finally {
      setDisabling(false);
    }
  };

  if (loading) {
    return (
      <PageShell
        title="Two-Factor Authentication"
        actions={
          <Link href="/system/">
            <Button variant="ghost" size="icon-xs">
              <ArrowLeft className="h-4 w-4" />
            </Button>
          </Link>
        }
      >
        <div className="rounded-lg border border-border bg-card p-4 space-y-3">
          <Skeleton className="h-4 w-48" />
          <Skeleton className="h-4 w-32" />
        </div>
      </PageShell>
    );
  }

  return (
    <PageShell
      title="Two-Factor Authentication"
      description="Add an extra layer of security to your account"
      actions={
        <Link href="/system/">
          <Button variant="ghost" size="icon-xs">
            <ArrowLeft className="h-4 w-4" />
          </Button>
        </Link>
      }
    >
      {/* Status Card */}
      <div className="rounded-lg border border-border bg-card p-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <ShieldCheck className="h-3.5 w-3.5 text-muted-foreground/40" strokeWidth={1.5} />
            <span className="text-[13px] font-medium">2FA Status</span>
          </div>
          {status?.enabled ? (
            <Badge variant="outline" className="text-[10px] border-green-500/30 text-green-600 bg-green-500/10">
              Enabled
            </Badge>
          ) : (
            <Badge variant="outline" className="text-[10px] border-muted-foreground/30 text-muted-foreground">
              Disabled
            </Badge>
          )}
        </div>
      </div>

      {/* Enabled State */}
      {status?.enabled && (
        <div className="space-y-3">
          <div className="rounded-lg border border-border bg-card p-4">
            <div className="flex items-center gap-2 mb-2">
              <ShieldCheck className="h-3.5 w-3.5 text-green-500/60" strokeWidth={1.5} />
              <span className="text-[13px] font-medium">Your account is protected</span>
            </div>
            <p className="text-[12px] text-muted-foreground">
              You will be asked for a verification code from your authenticator app when signing in.
            </p>
          </div>

          <div className="rounded-lg border border-border bg-card p-4">
            <div className="flex items-center gap-2 mb-3">
              <ShieldCheck className="h-3.5 w-3.5 text-muted-foreground/40" strokeWidth={1.5} />
              <span className="text-[13px] font-medium">Disable 2FA</span>
            </div>

            {!showDisable ? (
              <Button
                size="sm"
                variant="destructive"
                className="h-8 text-[12px] gap-1.5"
                onClick={() => setShowDisable(true)}
              >
                Disable Two-Factor Authentication
              </Button>
            ) : (
              <div className="space-y-3">
                <div>
                  <label className="text-[12px] text-muted-foreground font-normal">
                    Enter your 2FA code to confirm
                  </label>
                  <Input
                    className="h-8 text-[13px] bg-background/50 border-border mt-1.5 max-w-[240px]"
                    placeholder="000000"
                    value={disableCode}
                    onChange={(e) => setDisableCode(e.target.value.replace(/\D/g, ""))}
                    maxLength={6}
                  />
                </div>
                <div className="flex items-center gap-2">
                  <Button
                    size="sm"
                    variant="destructive"
                    className="h-8 text-[12px] gap-1.5"
                    onClick={handleDisable}
                    disabled={disabling}
                  >
                    {disabling ? "Disabling..." : "Confirm Disable"}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    className="h-8 text-[12px]"
                    onClick={() => {
                      setShowDisable(false);
                      setDisableCode("");
                    }}
                  >
                    Cancel
                  </Button>
                </div>
              </div>
            )}
          </div>
        </div>
      )}

      {/* Disabled State - Setup Flow */}
      {!status?.enabled && !setupData && (
        <div className="rounded-lg border border-border bg-card p-6 text-center space-y-3">
          <Smartphone className="h-8 w-8 text-muted-foreground/30 mx-auto" />
          <div>
            <p className="text-[13px] font-medium">Protect your account</p>
            <p className="text-[12px] text-muted-foreground mt-1">
              Use an authenticator app to generate verification codes for an additional layer of security.
            </p>
          </div>
          <Button
            size="sm"
            className="h-8 text-[12px] gap-1.5"
            onClick={handleSetup}
            disabled={settingUp}
          >
            <ShieldCheck className="h-3.5 w-3.5" />
            {settingUp ? "Setting up..." : "Enable Two-Factor Authentication"}
          </Button>
        </div>
      )}

      {/* Setup Flow - QR Code and Verification */}
      {!status?.enabled && setupData && (
        <div className="space-y-3">
          <div className="rounded-lg border border-border bg-card p-4 space-y-4">
            <div className="flex items-center gap-2">
              <Smartphone className="h-3.5 w-3.5 text-muted-foreground/40" strokeWidth={1.5} />
              <span className="text-[13px] font-medium">Scan QR Code</span>
            </div>
            <p className="text-[12px] text-muted-foreground">
              Scan the QR code below with your authenticator app (Google Authenticator, Authy, etc.)
            </p>
            <div className="flex justify-center py-2">
              <img
                src={setupData.qr_url}
                alt="2FA QR Code"
                className="rounded-lg border border-border"
                width={200}
                height={200}
              />
            </div>
            <div>
              <label className="text-[12px] text-muted-foreground font-normal">
                Or enter this secret manually:
              </label>
              <pre className="mt-1.5 rounded-md bg-muted/50 border border-border p-2.5 text-[12px] font-mono text-center select-all">
                {setupData.secret}
              </pre>
            </div>
          </div>

          <div className="rounded-lg border border-border bg-card p-4 space-y-3">
            <div className="flex items-center gap-2">
              <Key className="h-3.5 w-3.5 text-muted-foreground/40" strokeWidth={1.5} />
              <span className="text-[13px] font-medium">Verify Setup</span>
            </div>
            <div>
              <label className="text-[12px] text-muted-foreground font-normal">
                Enter the 6-digit code from your authenticator app
              </label>
              <Input
                className="h-8 text-[13px] bg-background/50 border-border mt-1.5 max-w-[240px]"
                placeholder="000000"
                value={verifyCode}
                onChange={(e) => setVerifyCode(e.target.value.replace(/\D/g, ""))}
                maxLength={6}
              />
            </div>
            <div className="flex items-center gap-2">
              <Button
                size="sm"
                className="h-8 text-[12px] gap-1.5"
                onClick={handleVerify}
                disabled={verifying}
              >
                {verifying ? "Verifying..." : "Verify and Enable"}
              </Button>
              <Button
                size="sm"
                variant="ghost"
                className="h-8 text-[12px]"
                onClick={() => {
                  setSetupData(null);
                  setVerifyCode("");
                }}
              >
                Cancel
              </Button>
            </div>
          </div>
        </div>
      )}
    </PageShell>
  );
}

export default function Page() {
  return <PageContent />;
}
