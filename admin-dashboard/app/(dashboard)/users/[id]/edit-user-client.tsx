"use client";

import { useEffect, useState } from "react";

import Link from "next/link";
import { api } from "@/lib/api";
import { useRouteId } from "@/lib/use-route-id";
import type { User, SystemInfo } from "@/lib/types";
import { PageShell } from "@/components/shared/page-shell";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Separator } from "@/components/ui/separator";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { ArrowLeft, Loader2, Trash2, HardDrive, Download, Smartphone, Copy, Check, ChevronDown } from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";

function EditUserContent() {
  const userId = useRouteId("users");

  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [showDelete, setShowDelete] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const [password, setPassword] = useState("");
  const [isAdmin, setIsAdmin] = useState(false);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [quotaBytes, setQuotaBytes] = useState<number>(1073741824);
  const [savingQuota, setSavingQuota] = useState(false);
  const [systemInfo, setSystemInfo] = useState<SystemInfo | null>(null);
  const [showManualSetup, setShowManualSetup] = useState(false);
  const [copiedField, setCopiedField] = useState<string | null>(null);

  useEffect(() => {
    api.get<User>(`/v1/users/${userId}`).then((res) => {
      if (res.success && res.data) {
        setUser(res.data);
        setIsAdmin(res.data.is_admin);
        setQuotaBytes(res.data.quota_bytes || 1073741824);
      } else {
        toast.error("User not found");
        window.location.href = "/admin/users/";
      }
      setLoading(false);
    });
    api.get<SystemInfo>("/v1/system").then((res) => {
      if (res.success && res.data) setSystemInfo(res.data);
    });
  }, [userId]);

  const copyToClipboard = (text: string, field: string) => {
    navigator.clipboard.writeText(text);
    setCopiedField(field);
    setTimeout(() => setCopiedField(null), 2000);
  };

  const triggerDownload = (content: string, filename: string, mimeType: string) => {
    const blob = new Blob([content], { type: mimeType });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  };

  const generateSimpleUUID = (seed: string) => {
    let hash = 0;
    for (let i = 0; i < seed.length; i++) {
      hash = ((hash << 5) - hash + seed.charCodeAt(i)) | 0;
    }
    const hex = Math.abs(hash).toString(16).padStart(8, "0");
    return `${hex.slice(0, 8)}-${hex.slice(0, 4)}-4${hex.slice(1, 4)}-a${hex.slice(1, 4)}-${hex.padEnd(12, "0").slice(0, 12)}`;
  };

  const downloadAppleProfile = () => {
    if (!user || !systemInfo) return;
    const domain = user.email.split("@")[1];
    const hostname = systemInfo.hostname;
    const imapsPort = systemInfo.config.imaps_port;
    const smtpsPort = systemInfo.config.smtps_port;
    const displayName = domain + " Mail";
    const emailUUID = generateSimpleUUID(user.email + "-email");
    const profileUUID = generateSimpleUUID(user.email + "-profile");

    const xml = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>PayloadContent</key>
    <array>
        <dict>
            <key>EmailAccountDescription</key>
            <string>${displayName}</string>
            <key>EmailAccountName</key>
            <string>${user.email}</string>
            <key>EmailAccountType</key>
            <string>EmailTypeIMAP</string>
            <key>EmailAddress</key>
            <string>${user.email}</string>
            <key>IncomingMailServerAuthentication</key>
            <string>EmailAuthPassword</string>
            <key>IncomingMailServerHostName</key>
            <string>${hostname}</string>
            <key>IncomingMailServerPortNumber</key>
            <integer>${imapsPort}</integer>
            <key>IncomingMailServerUseSSL</key>
            <true/>
            <key>IncomingMailServerUsername</key>
            <string>${user.email}</string>
            <key>OutgoingMailServerAuthentication</key>
            <string>EmailAuthPassword</string>
            <key>OutgoingMailServerHostName</key>
            <string>${hostname}</string>
            <key>OutgoingMailServerPortNumber</key>
            <integer>${smtpsPort}</integer>
            <key>OutgoingMailServerUseSSL</key>
            <true/>
            <key>OutgoingMailServerUsername</key>
            <string>${user.email}</string>
            <key>OutgoingPasswordSameAsIncomingPassword</key>
            <true/>
            <key>PayloadDescription</key>
            <string>Email account</string>
            <key>PayloadDisplayName</key>
            <string>${displayName}</string>
            <key>PayloadIdentifier</key>
            <string>com.${domain}.email</string>
            <key>PayloadType</key>
            <string>com.apple.mail.managed</string>
            <key>PayloadUUID</key>
            <string>${emailUUID}</string>
            <key>PayloadVersion</key>
            <integer>1</integer>
            <key>PreventAppSheet</key>
            <false/>
            <key>PreventMove</key>
            <false/>
            <key>SMIMEEnabled</key>
            <false/>
        </dict>
    </array>
    <key>PayloadDescription</key>
    <string>Email configuration for ${domain}</string>
    <key>PayloadDisplayName</key>
    <string>${displayName}</string>
    <key>PayloadIdentifier</key>
    <string>com.${domain}.profile</string>
    <key>PayloadOrganization</key>
    <string>${domain}</string>
    <key>PayloadRemovalDisallowed</key>
    <false/>
    <key>PayloadType</key>
    <string>Configuration</string>
    <key>PayloadUUID</key>
    <string>${profileUUID}</string>
    <key>PayloadVersion</key>
    <integer>1</integer>
</dict>
</plist>`;
    triggerDownload(xml, `${domain}-email.mobileconfig`, "application/x-apple-aspen-config");
    toast.success("Apple Mail profile downloaded");
  };

  const downloadThunderbirdConfig = () => {
    if (!user || !systemInfo) return;
    const domain = user.email.split("@")[1];
    const hostname = systemInfo.hostname;
    const imapsPort = systemInfo.config.imaps_port;
    const smtpPort = systemInfo.config.smtp_port;

    const xml = `<?xml version="1.0" encoding="UTF-8"?>
<clientConfig version="1.1">
  <emailProvider id="${domain}">
    <domain>${domain}</domain>
    <displayName>${domain} Mail</displayName>
    <incomingServer type="imap">
      <hostname>${hostname}</hostname>
      <port>${imapsPort}</port>
      <socketType>SSL</socketType>
      <username>${user.email}</username>
      <authentication>password-cleartext</authentication>
    </incomingServer>
    <outgoingServer type="smtp">
      <hostname>${hostname}</hostname>
      <port>${smtpPort}</port>
      <socketType>STARTTLS</socketType>
      <username>${user.email}</username>
      <authentication>password-cleartext</authentication>
    </outgoingServer>
  </emailProvider>
</clientConfig>`;
    triggerDownload(xml, `autoconfig-${user.email}.xml`, "application/xml");
    toast.success("Thunderbird config downloaded");
  };

  const downloadOutlookConfig = () => {
    if (!user || !systemInfo) return;
    const hostname = systemInfo.hostname;
    const imapsPort = systemInfo.config.imaps_port;
    const smtpPort = systemInfo.config.smtp_port;

    const xml = `<?xml version="1.0" encoding="utf-8"?>
<Autodiscover xmlns="http://schemas.microsoft.com/exchange/autodiscover/responseschema/2006">
  <Response xmlns="http://schemas.microsoft.com/exchange/autodiscover/outlook/responseschema/2006a">
    <Account>
      <AccountType>email</AccountType>
      <Action>settings</Action>
      <Protocol>
        <Type>IMAP</Type>
        <Server>${hostname}</Server>
        <Port>${imapsPort}</Port>
        <LoginName>${user.email}</LoginName>
        <DomainRequired>off</DomainRequired>
        <SPA>off</SPA>
        <SSL>on</SSL>
        <AuthRequired>on</AuthRequired>
      </Protocol>
      <Protocol>
        <Type>SMTP</Type>
        <Server>${hostname}</Server>
        <Port>${smtpPort}</Port>
        <LoginName>${user.email}</LoginName>
        <DomainRequired>off</DomainRequired>
        <SPA>off</SPA>
        <Encryption>TLS</Encryption>
        <AuthRequired>on</AuthRequired>
        <UsePOPAuth>on</UsePOPAuth>
        <SMTPLast>off</SMTPLast>
      </Protocol>
    </Account>
  </Response>
</Autodiscover>`;
    triggerDownload(xml, `autodiscover-${user.email}.xml`, "application/xml");
    toast.success("Outlook config downloaded");
  };

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    const newErrors: Record<string, string> = {};
    if (password && password.length < 8)
      newErrors.password = "Password must be at least 8 characters";
    setErrors(newErrors);
    if (Object.keys(newErrors).length > 0) return;

    setSaving(true);
    const body: Record<string, unknown> = { is_admin: isAdmin };
    if (password) body.password = password;

    const res = await api.put(`/v1/users/${userId}`, body);
    if (res.success) {
      toast.success("User updated successfully");
      setPassword("");
    } else {
      toast.error(res.error || "Failed to update user");
    }
    setSaving(false);
  };

  const handleDelete = async () => {
    setDeleting(true);
    const res = await api.delete(`/v1/users/${userId}`);
    if (res.success) {
      toast.success(`User ${user?.email} deleted`);
      window.location.href = "/admin/users/";
    } else {
      toast.error(res.error || "Failed to delete user");
    }
    setDeleting(false);
  };

  if (loading) {
    return (
      <PageShell title="Edit User">
        <div className="max-w-lg space-y-4">
          <Skeleton className="h-8 w-48" />
          <Skeleton className="h-64 w-full" />
        </div>
      </PageShell>
    );
  }

  if (!user) return null;

  return (
    <PageShell
      title="Edit User"
      description={user.email}
      actions={
        user.is_admin ? <Badge variant="default" className="text-[10px]">Admin</Badge> : undefined
      }
    >
      <div className="max-w-lg">
        <Link href="/users/" className="inline-flex items-center gap-1.5 text-[12px] text-muted-foreground/60 hover:text-foreground transition-colors mb-4">
          <ArrowLeft className="h-3.5 w-3.5" />Back to Users
        </Link>
      </div>

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-[13px] font-medium">
            Account Details
          </CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSave} className="space-y-4">
            <div className="space-y-1.5">
              <Label className="text-[13px]">Email Address</Label>
              <Input
                value={user.email}
                disabled
                className="text-[13px] opacity-60"
              />
              <p className="text-[12px] text-muted-foreground">
                Email address cannot be changed
              </p>
            </div>

            <div className="space-y-1.5">
              <Label className="text-[13px]">New Password</Label>
              <Input
                type="password"
                value={password}
                onChange={(e) => {
                  setPassword(e.target.value);
                  if (errors.password)
                    setErrors((prev) => ({ ...prev, password: "" }));
                }}
                placeholder="Leave empty to keep current"
                className="text-[13px]"
                aria-invalid={!!errors.password}
              />
              {errors.password && (
                <p className="text-[12px] text-destructive">
                  {errors.password}
                </p>
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
              />
            </div>

            <div className="space-y-1.5">
              <Label className="text-[13px] text-muted-foreground">
                Created
              </Label>
              <p className="text-[13px]">
                {formatDistanceToNow(new Date(user.created_at), {
                  addSuffix: true,
                })}
              </p>
            </div>

            <div className="flex items-center gap-2 pt-2">
              <Button
                type="submit"
                size="sm"
                className="text-[13px]"
                disabled={saving}
              >
                {saving && <Loader2 className="h-4 w-4 animate-spin" />}
                Save Changes
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

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-[13px] font-medium flex items-center gap-1.5">
            <HardDrive className="h-3.5 w-3.5" />
            Storage Quota
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {(() => {
            const used = user.used_bytes || 0;
            const quota = user.quota_bytes || 1073741824;
            const pct = Math.min((used / quota) * 100, 100);
            const color = pct > 90 ? "bg-destructive" : pct > 70 ? "bg-amber-500" : "bg-primary";
            const formatBytes = (b: number) => {
              if (b === 0) return "0 B";
              const k = 1024;
              const s = ["B", "KB", "MB", "GB", "TB"];
              const i = Math.floor(Math.log(b) / Math.log(k));
              return parseFloat((b / Math.pow(k, i)).toFixed(1)) + " " + s[i];
            };
            return (
              <div>
                <div className="flex items-center justify-between mb-1">
                  <span className="text-[12px] text-muted-foreground">
                    {formatBytes(used)} of {formatBytes(quota)} used
                  </span>
                  <span className="text-[12px] text-muted-foreground tabular-nums">
                    {pct.toFixed(1)}%
                  </span>
                </div>
                <div className="h-2 w-full rounded-full bg-muted overflow-hidden">
                  <div className={`h-full rounded-full transition-all ${color}`} style={{ width: `${pct}%` }} />
                </div>
              </div>
            );
          })()}

          <div className="space-y-1.5">
            <Label className="text-[13px]">Quota</Label>
            <select
              value={String(quotaBytes)}
              onChange={(e) => setQuotaBytes(Number(e.target.value))}
              className="h-8 w-full rounded-lg border border-border bg-background/50 px-2.5 text-[13px] text-foreground outline-none focus-visible:border-ring"
            >
              <option value="536870912">512 MB</option>
              <option value="1073741824">1 GB</option>
              <option value="2147483648">2 GB</option>
              <option value="5368709120">5 GB</option>
              <option value="10737418240">10 GB</option>
            </select>
          </div>

          <Button
            size="sm"
            className="text-[13px]"
            disabled={savingQuota}
            onClick={async () => {
              setSavingQuota(true);
              const res = await api.put(`/v1/users/${userId}/quota`, { quota_bytes: quotaBytes });
              if (res.success) {
                toast.success("Quota updated");
              } else {
                toast.error(res.error || "Failed to update quota");
              }
              setSavingQuota(false);
            }}
          >
            {savingQuota && <Loader2 className="h-4 w-4 animate-spin" />}
            Save Quota
          </Button>
        </CardContent>
      </Card>

      {systemInfo && (
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-[13px] font-medium flex items-center gap-1.5">
              <Smartphone className="h-3.5 w-3.5" />
              Mail Client Setup
            </CardTitle>
            <p className="text-[12px] text-muted-foreground">
              Download a profile to auto-configure this account in a mail client.
            </p>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex flex-wrap gap-2">
              <Button variant="outline" size="sm" className="text-[13px]" onClick={downloadAppleProfile}>
                <Download className="h-3.5 w-3.5" />
                Apple Mail
              </Button>
              <Button variant="outline" size="sm" className="text-[13px]" onClick={downloadThunderbirdConfig}>
                <Download className="h-3.5 w-3.5" />
                Thunderbird
              </Button>
              <Button variant="outline" size="sm" className="text-[13px]" onClick={downloadOutlookConfig}>
                <Download className="h-3.5 w-3.5" />
                Outlook
              </Button>
            </div>

            <button
              type="button"
              className="flex items-center gap-1 text-[12px] text-muted-foreground hover:text-foreground transition-colors"
              onClick={() => setShowManualSetup(!showManualSetup)}
            >
              <ChevronDown className={`h-3.5 w-3.5 transition-transform ${showManualSetup ? "rotate-0" : "-rotate-90"}`} />
              Manual setup instructions
            </button>

            {showManualSetup && (() => {
              const domain = user.email.split("@")[1];
              const mailHost = `mail.${domain}`;
              const fields = [
                { label: "IMAP Server", value: mailHost },
                { label: "IMAP Port", value: `${systemInfo.config.imaps_port} (SSL)` },
                { label: "SMTP Server", value: mailHost },
                { label: "SMTP Port", value: `${systemInfo.config.smtps_port} (SSL)` },
                { label: "Username", value: user.email },
              ];
              return (
                <div className="rounded-lg border bg-muted/30 p-3 space-y-2">
                  {fields.map((f) => (
                    <div key={f.label} className="flex items-center justify-between gap-4">
                      <span className="text-[12px] text-muted-foreground whitespace-nowrap">{f.label}</span>
                      <div className="flex items-center gap-1.5">
                        <span className="text-[13px] font-mono">{f.value}</span>
                        <button
                          type="button"
                          className="text-muted-foreground hover:text-foreground transition-colors p-0.5"
                          onClick={() => copyToClipboard(f.value.replace(/ \(SSL\)/, ""), f.label)}
                        >
                          {copiedField === f.label ? (
                            <Check className="h-3 w-3 text-green-500" />
                          ) : (
                            <Copy className="h-3 w-3" />
                          )}
                        </button>
                      </div>
                    </div>
                  ))}
                </div>
              );
            })()}
          </CardContent>
        </Card>
      )}

      <Separator />

      <Card className="border-destructive/30">
        <CardHeader className="pb-3">
          <CardTitle className="text-[13px] font-medium text-destructive">
            Danger Zone
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex items-center justify-between">
            <div>
              <p className="text-[13px] font-medium">Delete this user</p>
              <p className="text-[12px] text-muted-foreground mt-0.5">
                Permanently remove this account and all associated data
              </p>
            </div>
            <Button
              variant="destructive"
              size="sm"
              className="text-[13px]"
              onClick={() => setShowDelete(true)}
            >
              <Trash2 className="h-4 w-4" />
              Delete
            </Button>
          </div>
        </CardContent>
      </Card>

      <Dialog open={showDelete} onOpenChange={setShowDelete}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete User</DialogTitle>
            <DialogDescription className="text-[13px]">
              Are you sure you want to delete <strong>{user.email}</strong>? This
              action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              size="sm"
              className="text-[13px]"
              onClick={() => setShowDelete(false)}
            >
              Cancel
            </Button>
            <Button
              variant="destructive"
              size="sm"
              className="text-[13px]"
              onClick={handleDelete}
              disabled={deleting}
            >
              {deleting ? "Deleting..." : "Delete User"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </PageShell>
  );
}

export default function EditUserClient() {
  return (
      <EditUserContent />
  );
}
