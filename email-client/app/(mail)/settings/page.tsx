"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useAuthStore } from "@/lib/auth-store";
import { TwoFactorInput } from "@/components/auth/two-factor-input";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Switch } from "@/components/ui/switch";
import { Separator } from "@/components/ui/separator";
import { formatDistanceToNow } from "date-fns";
import { cn } from "@/lib/utils";
import {
  ArrowLeft,
  CheckCircle2,
  ChevronDown,
  Circle,
  Fingerprint,
  Globe,
  Key,
  Link2,
  LogOut,
  Mail,
  Moon,
  Bell,
  Palette,
  Plus,
  Server,
  Shield,
  Sun,
  Trash2,
  User,
  Eye,
  EyeOff,
  Type,
  Image,
  Sparkles,
  Clock,
  MailCheck,
  Undo2,
  Languages,
  HardDrive,
  Zap,
  Pen,
} from "lucide-react";

function GoogleIcon({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" className={className} aria-hidden="true">
      <path d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92a5.06 5.06 0 0 1-2.2 3.32v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.1z" fill="#4285F4" />
      <path d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z" fill="#34A853" />
      <path d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z" fill="#FBBC05" />
      <path d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z" fill="#EA4335" />
    </svg>
  );
}

// ── Section wrapper ──
function Section({ children, className }: { children: React.ReactNode; className?: string }) {
  return <section className={cn("space-y-2", className)}>{children}</section>;
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return <p className="px-1 text-[11px] font-medium uppercase tracking-wider text-muted-foreground/60">{children}</p>;
}

function Card({ children, className }: { children: React.ReactNode; className?: string }) {
  return <div className={cn("rounded-xl border border-border bg-card overflow-hidden", className)}>{children}</div>;
}

function Row({
  children,
  onClick,
  className,
  last,
}: {
  children: React.ReactNode;
  onClick?: () => void;
  className?: string;
  last?: boolean;
}) {
  return (
    <div
      onClick={onClick}
      className={cn(
        "flex items-center gap-3 px-4 py-3 min-h-[44px]",
        !last && "border-b border-border/50",
        onClick && "cursor-pointer active:bg-accent/30 transition-colors",
        className
      )}
    >
      {children}
    </div>
  );
}

function RowLabel({ children, sub }: { children: React.ReactNode; sub?: string }) {
  return (
    <div className="flex-1 min-w-0">
      <p className="text-[13px] text-foreground">{children}</p>
      {sub && <p className="text-[11px] text-muted-foreground/70 mt-0.5">{sub}</p>}
    </div>
  );
}

function RowIcon({ icon: Icon, color }: { icon: React.ElementType; color?: string }) {
  return (
    <div className={cn("flex h-7 w-7 items-center justify-center rounded-md shrink-0", color || "bg-muted")}>
      <Icon className="h-3.5 w-3.5" />
    </div>
  );
}

// ── Tabs ──
type Tab = "account" | "connections" | "server" | "compose" | "appearance" | "notifications" | "security";

export default function SettingsPage() {
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const logout = useAuthStore((s) => s.logout);
  const enable2FA = useAuthStore((s) => s.enable2FA);
  const disable2FA = useAuthStore((s) => s.disable2FA);
  const addPasskey = useAuthStore((s) => s.addPasskey);
  const removePasskey = useAuthStore((s) => s.removePasskey);

  const [tab, setTab] = useState<Tab>("account");
  const [showSetup2FA, setShowSetup2FA] = useState(false);
  const [passkeyName, setPasskeyName] = useState("");
  const [showAddPasskey, setShowAddPasskey] = useState(false);

  // ── Mock settings state ──
  const [displayName, setDisplayName] = useState(user?.name ?? "Fenil Sonani");
  const [signature, setSignature] = useState(
    `<p style="color:#888;font-size:13px;">Fenil Sonani</p><p style="color:#666;font-size:12px;">fenil@fenilsonani.com</p>`
  );
  const [signatureEnabled, setSignatureEnabled] = useState(true);
  const [signatureOnReply, setSignatureOnReply] = useState(false);
  const [googleConnected, setGoogleConnected] = useState(false);
  const [imapHost, setImapHost] = useState("mail.fenilsonani.com");
  const [imapPort, setImapPort] = useState("993");
  const [smtpHost, setSmtpHost] = useState("mail.fenilsonani.com");
  const [smtpPort, setSmtpPort] = useState("465");
  const [theme, setTheme] = useState<"dark" | "light" | "system">("dark");
  const [density, setDensity] = useState<"default" | "compact" | "comfortable">("default");
  const [notifEnabled, setNotifEnabled] = useState(true);
  const [notifSound, setNotifSound] = useState(true);
  const [notifPreview, setNotifPreview] = useState(true);
  const [notifFilter, setNotifFilter] = useState("primary");
  const [undoSend, setUndoSend] = useState(true);
  const [undoDelay, setUndoDelay] = useState("10");
  const [defaultReply, setDefaultReply] = useState<"reply" | "reply-all">("reply");
  const [readReceipts, setReadReceipts] = useState(false);
  const [loadImages, setLoadImages] = useState<"always" | "ask" | "never">("always");
  const [aiSuggestions, setAiSuggestions] = useState(true);
  const [aiSummary, setAiSummary] = useState(true);
  const [language, setLanguage] = useState("English");
  const [snippets, setSnippets] = useState(true);
  const [threadView, setThreadView] = useState(true);
  const [swipeLeft, setSwipeLeft] = useState<"trash" | "archive" | "flag">("trash");
  const [swipeRight, setSwipeRight] = useState<"trash" | "archive" | "flag">("archive");
  const [activeSignature, setActiveSignature] = useState(0);
  const [signatures] = useState([
    { id: 0, name: "Default", html: `Fenil Sonani\nfenil@fenilsonani.com` },
    { id: 1, name: "Work", html: `Fenil Sonani — Engineer\nfenilsonani.com` },
  ]);

  if (!user) return null;

  const tabs: { id: Tab; label: string; icon: React.ElementType }[] = [
    { id: "account", label: "Account", icon: User },
    { id: "connections", label: "Connections", icon: Link2 },
    { id: "server", label: "Server", icon: Server },
    { id: "compose", label: "Compose", icon: Pen },
    { id: "appearance", label: "Appearance", icon: Palette },
    { id: "notifications", label: "Notifications", icon: Bell },
    { id: "security", label: "Security", icon: Shield },
  ];

  return (
    <div className="h-full overflow-y-auto bg-background">
      <div className="mx-auto max-w-xl px-4 py-5 pb-20 space-y-5">

        {/* ── Header ── */}
        <div className="flex items-center gap-3">
          <button
            onClick={() => router.push("/mail/inbox")}
            className="flex h-9 w-9 items-center justify-center rounded-lg text-muted-foreground hover:bg-accent transition-colors"
          >
            <ArrowLeft className="h-4 w-4" />
          </button>
          <h1 className="text-base font-semibold">Settings</h1>
        </div>

        {/* ── Tab bar ── */}
        <div className="flex gap-1 overflow-x-auto -mx-4 px-4 pb-1 scrollbar-none">
          {tabs.map((t) => (
            <button
              key={t.id}
              onClick={() => setTab(t.id)}
              className={cn(
                "flex items-center gap-1.5 shrink-0 rounded-lg px-3 py-1.5 text-[13px] transition-colors",
                tab === t.id
                  ? "bg-accent text-foreground font-medium"
                  : "text-muted-foreground hover:bg-accent/40"
              )}
            >
              <t.icon className="h-3.5 w-3.5" />
              {t.label}
            </button>
          ))}
        </div>

        {/* ════════════════════════════════════════════
            ACCOUNT
        ════════════════════════════════════════════ */}
        {tab === "account" && (
          <div className="space-y-5">
            <Section>
              <SectionLabel>Profile</SectionLabel>
              <Card>
                <div className="flex items-center gap-3 px-4 py-4 border-b border-border/50">
                  <div className="flex h-12 w-12 items-center justify-center rounded-full bg-primary/15 text-lg font-semibold text-primary">
                    {user.name.charAt(0)}
                  </div>
                  <div className="flex-1 min-w-0">
                    <p className="text-[13px] font-medium">{user.name}</p>
                    <p className="text-[11px] text-muted-foreground">{user.email}</p>
                  </div>
                  <Button variant="ghost" size="sm" className="text-[12px] text-primary h-7">
                    Edit
                  </Button>
                </div>
                <Row>
                  <RowLabel sub="How others see you">Display name</RowLabel>
                  <Input
                    value={displayName}
                    onChange={(e) => setDisplayName(e.target.value)}
                    className="w-40 h-8 text-[13px] text-right border-none bg-transparent focus-visible:ring-0"
                  />
                </Row>
                <Row last>
                  <RowLabel sub="Where replies go">Reply-to address</RowLabel>
                  <span className="text-[13px] text-muted-foreground">{user.email}</span>
                </Row>
              </Card>
            </Section>

            <Section>
              <SectionLabel>Storage</SectionLabel>
              <Card>
                <Row last>
                  <RowIcon icon={HardDrive} color="bg-blue-500/15 text-blue-400" />
                  <RowLabel sub="238 MB of 5 GB used">Mailbox storage</RowLabel>
                  <div className="w-20 h-1.5 rounded-full bg-muted overflow-hidden">
                    <div className="h-full w-[5%] rounded-full bg-primary" />
                  </div>
                </Row>
              </Card>
            </Section>

            <Button
              variant="ghost"
              onClick={() => { logout(); router.push("/login"); }}
              className="w-full h-11 gap-2 text-[13px] text-destructive hover:text-destructive hover:bg-destructive/10 rounded-xl"
            >
              <LogOut className="h-4 w-4" />
              Sign out
            </Button>
          </div>
        )}

        {/* ════════════════════════════════════════════
            CONNECTIONS
        ════════════════════════════════════════════ */}
        {tab === "connections" && (
          <div className="space-y-5">
            <Section>
              <SectionLabel>Email accounts</SectionLabel>
              <Card>
                <Row>
                  <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-indigo-500/15 shrink-0">
                    <Mail className="h-4 w-4 text-indigo-400" />
                  </div>
                  <RowLabel sub={user.email}>Self-hosted</RowLabel>
                  <span className="flex items-center gap-1 text-[11px] text-green-400">
                    <CheckCircle2 className="h-3 w-3" />
                    Active
                  </span>
                </Row>
                <Row>
                  <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-white/5 shrink-0">
                    <GoogleIcon className="h-4 w-4" />
                  </div>
                  <RowLabel sub={googleConnected ? "fenil@gmail.com" : "Import mail, contacts, calendar"}>
                    Google
                  </RowLabel>
                  <Button
                    variant={googleConnected ? "ghost" : "outline"}
                    size="sm"
                    onClick={() => setGoogleConnected(!googleConnected)}
                    className={cn("text-[12px] h-7", googleConnected && "text-muted-foreground")}
                  >
                    {googleConnected ? "Disconnect" : "Connect"}
                  </Button>
                </Row>
                <Row last>
                  <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-blue-500/10 shrink-0">
                    <Globe className="h-4 w-4 text-blue-400" />
                  </div>
                  <RowLabel sub="Outlook, Hotmail, Live">Microsoft</RowLabel>
                  <Button variant="outline" size="sm" className="text-[12px] h-7">
                    Connect
                  </Button>
                </Row>
              </Card>
            </Section>

            <Section>
              <SectionLabel>Import</SectionLabel>
              <Card>
                <Row last onClick={() => {}}>
                  <RowIcon icon={Plus} color="bg-muted text-muted-foreground" />
                  <RowLabel sub="IMAP, POP3, or mbox file">Add email account</RowLabel>
                  <ChevronDown className="h-3.5 w-3.5 text-muted-foreground/40 -rotate-90" />
                </Row>
              </Card>
            </Section>
          </div>
        )}

        {/* ════════════════════════════════════════════
            SERVER
        ════════════════════════════════════════════ */}
        {tab === "server" && (
          <div className="space-y-5">
            <Section>
              <SectionLabel>Incoming (IMAP)</SectionLabel>
              <Card>
                <Row>
                  <RowLabel>Server</RowLabel>
                  <Input value={imapHost} onChange={(e) => setImapHost(e.target.value)}
                    className="w-48 h-7 text-[12px] font-mono text-right border-none bg-transparent focus-visible:ring-0" />
                </Row>
                <Row>
                  <RowLabel>Port</RowLabel>
                  <Input value={imapPort} onChange={(e) => setImapPort(e.target.value)}
                    className="w-20 h-7 text-[12px] font-mono text-right border-none bg-transparent focus-visible:ring-0" />
                </Row>
                <Row last>
                  <RowLabel>Security</RowLabel>
                  <span className="text-[12px] font-mono text-muted-foreground">SSL/TLS</span>
                </Row>
              </Card>
            </Section>

            <Section>
              <SectionLabel>Outgoing (SMTP)</SectionLabel>
              <Card>
                <Row>
                  <RowLabel>Server</RowLabel>
                  <Input value={smtpHost} onChange={(e) => setSmtpHost(e.target.value)}
                    className="w-48 h-7 text-[12px] font-mono text-right border-none bg-transparent focus-visible:ring-0" />
                </Row>
                <Row>
                  <RowLabel>Port</RowLabel>
                  <Input value={smtpPort} onChange={(e) => setSmtpPort(e.target.value)}
                    className="w-20 h-7 text-[12px] font-mono text-right border-none bg-transparent focus-visible:ring-0" />
                </Row>
                <Row last>
                  <RowLabel>Security</RowLabel>
                  <span className="text-[12px] font-mono text-muted-foreground">SSL/TLS</span>
                </Row>
              </Card>
            </Section>

            <Section>
              <SectionLabel>DNS & Deliverability</SectionLabel>
              <Card>
                {[
                  { label: "SPF", value: "v=spf1 ip4:170.187.145.193 ~all" },
                  { label: "DKIM", value: "fenilsonani.com · RSA 2048" },
                  { label: "DMARC", value: "p=quarantine; rua=mailto:dmarc@fenilsonani.com" },
                  { label: "rDNS", value: "mail.fenilsonani.com" },
                ].map((r, i, arr) => (
                  <Row key={r.label} last={i === arr.length - 1}>
                    <CheckCircle2 className="h-4 w-4 text-green-400 shrink-0" />
                    <div className="flex-1 min-w-0">
                      <p className="text-[13px] font-medium">{r.label}</p>
                      <p className="text-[11px] font-mono text-muted-foreground/60 truncate">{r.value}</p>
                    </div>
                  </Row>
                ))}
              </Card>
            </Section>
          </div>
        )}

        {/* ════════════════════════════════════════════
            COMPOSE & SIGNATURES
        ════════════════════════════════════════════ */}
        {tab === "compose" && (
          <div className="space-y-5">
            <Section>
              <SectionLabel>Behavior</SectionLabel>
              <Card>
                <Row>
                  <RowIcon icon={Undo2} color="bg-orange-500/15 text-orange-400" />
                  <RowLabel sub="Hold send to allow cancellation">Undo send</RowLabel>
                  <Switch checked={undoSend} onCheckedChange={setUndoSend} />
                </Row>
                {undoSend && (
                  <Row>
                    <RowLabel>Cancel window</RowLabel>
                    <div className="flex items-center gap-2">
                      {["5", "10", "20", "30"].map((s) => (
                        <button
                          key={s}
                          onClick={() => setUndoDelay(s)}
                          className={cn(
                            "h-7 min-w-[36px] rounded-md px-2 text-[12px] transition-colors",
                            undoDelay === s
                              ? "bg-primary text-primary-foreground"
                              : "bg-muted text-muted-foreground hover:bg-accent"
                          )}
                        >
                          {s}s
                        </button>
                      ))}
                    </div>
                  </Row>
                )}
                <Row>
                  <RowIcon icon={Mail} color="bg-blue-500/15 text-blue-400" />
                  <RowLabel sub="When clicking reply">Default action</RowLabel>
                  <button
                    onClick={() => setDefaultReply(defaultReply === "reply" ? "reply-all" : "reply")}
                    className="text-[12px] text-primary"
                  >
                    {defaultReply === "reply" ? "Reply" : "Reply All"}
                  </button>
                </Row>
                <Row>
                  <RowIcon icon={MailCheck} color="bg-green-500/15 text-green-400" />
                  <RowLabel sub="Ask senders for delivery confirmation">Read receipts</RowLabel>
                  <Switch checked={readReceipts} onCheckedChange={setReadReceipts} />
                </Row>
                <Row last>
                  <RowIcon icon={Image} color="bg-purple-500/15 text-purple-400" />
                  <RowLabel sub="Load remote images in emails">Remote images</RowLabel>
                  <button
                    onClick={() => {
                      const next = loadImages === "always" ? "ask" : loadImages === "ask" ? "never" : "always";
                      setLoadImages(next);
                    }}
                    className="text-[12px] text-primary capitalize"
                  >
                    {loadImages}
                  </button>
                </Row>
              </Card>
            </Section>

            <Section>
              <div className="flex items-center justify-between px-1">
                <SectionLabel>Signatures</SectionLabel>
                <Switch checked={signatureEnabled} onCheckedChange={setSignatureEnabled} />
              </div>
              {signatureEnabled && (
                <Card>
                  {signatures.map((sig, i) => (
                    <Row
                      key={sig.id}
                      last={i === signatures.length - 1}
                      onClick={() => setActiveSignature(sig.id)}
                      className={cn(activeSignature === sig.id && "bg-accent/40")}
                    >
                      {activeSignature === sig.id ? (
                        <CheckCircle2 className="h-4 w-4 text-primary shrink-0" />
                      ) : (
                        <Circle className="h-4 w-4 text-muted-foreground/30 shrink-0" />
                      )}
                      <div className="flex-1 min-w-0">
                        <p className="text-[13px] font-medium">{sig.name}</p>
                        <p className="text-[11px] text-muted-foreground/60 truncate font-mono">{sig.html}</p>
                      </div>
                      <Button variant="ghost" size="sm" className="text-[12px] text-primary h-7"
                        onClick={(e) => e.stopPropagation()}>
                        Edit
                      </Button>
                    </Row>
                  ))}
                </Card>
              )}
              {signatureEnabled && (
                <Card>
                  <Row>
                    <RowLabel sub="Append signature when replying or forwarding">Include on replies</RowLabel>
                    <Switch checked={signatureOnReply} onCheckedChange={setSignatureOnReply} />
                  </Row>
                  <Row last onClick={() => {}}>
                    <Plus className="h-4 w-4 text-muted-foreground shrink-0" />
                    <RowLabel>Create new signature</RowLabel>
                    <ChevronDown className="h-3.5 w-3.5 text-muted-foreground/40 -rotate-90" />
                  </Row>
                </Card>
              )}
            </Section>

            <Section>
              <SectionLabel>AI features</SectionLabel>
              <Card>
                <Row>
                  <RowIcon icon={Sparkles} color="bg-violet-500/15 text-violet-400" />
                  <RowLabel sub="Smart replies and compose assist">AI suggestions</RowLabel>
                  <Switch checked={aiSuggestions} onCheckedChange={setAiSuggestions} />
                </Row>
                <Row last>
                  <RowIcon icon={Type} color="bg-violet-500/15 text-violet-400" />
                  <RowLabel sub="Summarize long threads automatically">Thread summaries</RowLabel>
                  <Switch checked={aiSummary} onCheckedChange={setAiSummary} />
                </Row>
              </Card>
            </Section>
          </div>
        )}

        {/* ════════════════════════════════════════════
            APPEARANCE
        ════════════════════════════════════════════ */}
        {tab === "appearance" && (
          <div className="space-y-5">
            <Section>
              <SectionLabel>Theme</SectionLabel>
              <div className="grid grid-cols-3 gap-2">
                {([
                  { id: "light" as const, label: "Light", icon: Sun },
                  { id: "dark" as const, label: "Dark", icon: Moon },
                  { id: "system" as const, label: "Auto", icon: Palette },
                ]).map((opt) => (
                  <button
                    key={opt.id}
                    onClick={() => setTheme(opt.id)}
                    className={cn(
                      "flex flex-col items-center gap-2 rounded-xl border p-4 text-[13px] transition-colors",
                      theme === opt.id
                        ? "border-primary bg-primary/5 text-foreground font-medium"
                        : "border-border bg-card text-muted-foreground hover:bg-accent/30"
                    )}
                  >
                    <opt.icon className="h-5 w-5" />
                    {opt.label}
                  </button>
                ))}
              </div>
            </Section>

            <Section>
              <SectionLabel>Density</SectionLabel>
              <div className="grid grid-cols-3 gap-2">
                {([
                  { id: "compact" as const, label: "Compact" },
                  { id: "default" as const, label: "Default" },
                  { id: "comfortable" as const, label: "Relaxed" },
                ]).map((opt) => (
                  <button
                    key={opt.id}
                    onClick={() => setDensity(opt.id)}
                    className={cn(
                      "flex flex-col items-center gap-1.5 rounded-xl border p-3 text-[13px] transition-colors",
                      density === opt.id
                        ? "border-primary bg-primary/5 text-foreground font-medium"
                        : "border-border bg-card text-muted-foreground hover:bg-accent/30"
                    )}
                  >
                    {/* Mini preview bars */}
                    <div className="flex flex-col gap-[3px] w-full">
                      {[1, 2, 3].map((n) => (
                        <div key={n} className={cn(
                          "w-full rounded-sm bg-muted-foreground/15",
                          opt.id === "compact" ? "h-1.5" : opt.id === "default" ? "h-2" : "h-2.5"
                        )} />
                      ))}
                    </div>
                    <span className="text-[11px]">{opt.label}</span>
                  </button>
                ))}
              </div>
            </Section>

            <Section>
              <SectionLabel>Mail list</SectionLabel>
              <Card>
                <Row>
                  <RowLabel sub="Show preview of email body">Snippets</RowLabel>
                  <Switch checked={snippets} onCheckedChange={setSnippets} />
                </Row>
                <Row last>
                  <RowLabel sub="Group replies as conversations">Thread view</RowLabel>
                  <Switch checked={threadView} onCheckedChange={setThreadView} />
                </Row>
              </Card>
            </Section>

            <Section>
              <SectionLabel>Swipe actions</SectionLabel>
              <Card>
                <Row>
                  <RowLabel>Swipe right</RowLabel>
                  <button
                    onClick={() => {
                      const opts: typeof swipeRight[] = ["archive", "trash", "flag"];
                      setSwipeRight(opts[(opts.indexOf(swipeRight) + 1) % opts.length]);
                    }}
                    className="text-[12px] text-primary capitalize"
                  >
                    {swipeRight}
                  </button>
                </Row>
                <Row last>
                  <RowLabel>Swipe left</RowLabel>
                  <button
                    onClick={() => {
                      const opts: typeof swipeLeft[] = ["trash", "archive", "flag"];
                      setSwipeLeft(opts[(opts.indexOf(swipeLeft) + 1) % opts.length]);
                    }}
                    className="text-[12px] text-primary capitalize"
                  >
                    {swipeLeft}
                  </button>
                </Row>
              </Card>
            </Section>

            <Section>
              <SectionLabel>Language</SectionLabel>
              <Card>
                <Row last>
                  <RowIcon icon={Languages} color="bg-teal-500/15 text-teal-400" />
                  <RowLabel>Display language</RowLabel>
                  <span className="text-[12px] text-muted-foreground">{language}</span>
                </Row>
              </Card>
            </Section>
          </div>
        )}

        {/* ════════════════════════════════════════════
            NOTIFICATIONS
        ════════════════════════════════════════════ */}
        {tab === "notifications" && (
          <div className="space-y-5">
            <Section>
              <SectionLabel>Alerts</SectionLabel>
              <Card>
                <Row>
                  <RowIcon icon={Bell} color="bg-red-500/15 text-red-400" />
                  <RowLabel sub="Desktop and mobile push">Notifications</RowLabel>
                  <Switch checked={notifEnabled} onCheckedChange={setNotifEnabled} />
                </Row>
                <Row>
                  <RowLabel sub="Play alert sound">Sound</RowLabel>
                  <Switch checked={notifSound} onCheckedChange={setNotifSound} />
                </Row>
                <Row last>
                  <RowLabel sub="Show sender and subject">Preview</RowLabel>
                  <Switch checked={notifPreview} onCheckedChange={setNotifPreview} />
                </Row>
              </Card>
            </Section>

            <Section>
              <SectionLabel>Filter</SectionLabel>
              <Card>
                {[
                  { id: "all", label: "All new mail" },
                  { id: "primary", label: "Primary inbox only" },
                  { id: "starred", label: "Starred contacts" },
                  { id: "none", label: "None" },
                ].map((opt, i, arr) => (
                  <Row key={opt.id} last={i === arr.length - 1} onClick={() => setNotifFilter(opt.id)}>
                    {notifFilter === opt.id ? (
                      <CheckCircle2 className="h-4 w-4 text-primary shrink-0" />
                    ) : (
                      <Circle className="h-4 w-4 text-muted-foreground/30 shrink-0" />
                    )}
                    <RowLabel>{opt.label}</RowLabel>
                  </Row>
                ))}
              </Card>
            </Section>

            <Section>
              <SectionLabel>Schedule</SectionLabel>
              <Card>
                <Row last>
                  <RowIcon icon={Clock} color="bg-amber-500/15 text-amber-400" />
                  <RowLabel sub="Pause notifications during set hours">Do Not Disturb</RowLabel>
                  <Switch />
                </Row>
              </Card>
            </Section>
          </div>
        )}

        {/* ════════════════════════════════════════════
            SECURITY
        ════════════════════════════════════════════ */}
        {tab === "security" && (
          <div className="space-y-5">
            <Section>
              <SectionLabel>Authentication</SectionLabel>
              <Card>
                <Row last>
                  <RowIcon icon={Key} color="bg-amber-500/15 text-amber-400" />
                  <RowLabel sub={user.email}>
                    Signed in via <span className="capitalize">{user.authMethod}</span>
                  </RowLabel>
                </Row>
              </Card>
            </Section>

            <Section>
              <SectionLabel>Two-factor authentication</SectionLabel>
              <Card>
                <Row last={!showSetup2FA}>
                  <RowIcon icon={Shield} color={user.twoFactorEnabled ? "bg-green-500/15 text-green-400" : "bg-muted text-muted-foreground"} />
                  <RowLabel sub="Require code on every sign-in">
                    {user.twoFactorEnabled ? "Enabled" : "Disabled"}
                  </RowLabel>
                  <Switch
                    checked={user.twoFactorEnabled}
                    onCheckedChange={(checked) => {
                      if (checked) setShowSetup2FA(true);
                      else disable2FA();
                    }}
                  />
                </Row>
                {showSetup2FA && (
                  <div className="px-4 py-4 space-y-4 border-t border-border/50">
                    <div className="flex justify-center">
                      <div className="w-36 h-36 bg-white rounded-lg flex items-center justify-center">
                        <svg viewBox="0 0 100 100" className="w-28 h-28">
                          {Array.from({ length: 10 }, (_, row) =>
                            Array.from({ length: 10 }, (_, col) => (
                              <rect key={`${row}-${col}`} x={col * 10} y={row * 10} width="10" height="10"
                                fill={(row + col) % 3 === 0 || (row * col) % 5 === 0 ? "#000" : "#fff"} />
                            ))
                          )}
                        </svg>
                      </div>
                    </div>
                    <p className="text-[11px] text-center text-muted-foreground">
                      Scan with your authenticator app
                    </p>
                    <TwoFactorInput onComplete={() => { enable2FA(); setShowSetup2FA(false); }} />
                  </div>
                )}
              </Card>
            </Section>

            <Section>
              <SectionLabel>Passkeys</SectionLabel>
              <Card>
                {user.passkeys.length === 0 && !showAddPasskey && (
                  <Row last={false}>
                    <p className="text-[13px] text-muted-foreground/60">No passkeys registered</p>
                  </Row>
                )}
                {user.passkeys.map((pk, i) => (
                  <Row key={pk.id} last={i === user.passkeys.length - 1 && !showAddPasskey}>
                    <RowIcon icon={Fingerprint} color="bg-muted text-muted-foreground" />
                    <RowLabel sub={`Added ${formatDistanceToNow(new Date(pk.createdAt), { addSuffix: true })}`}>
                      {pk.name}
                    </RowLabel>
                    <button
                      onClick={() => removePasskey(pk.id)}
                      className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground hover:text-destructive hover:bg-destructive/10 transition-colors"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  </Row>
                ))}
                {showAddPasskey ? (
                  <div className="flex items-center gap-2 px-4 py-3">
                    <Input
                      placeholder="Passkey name"
                      value={passkeyName}
                      onChange={(e) => setPasskeyName(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter" && passkeyName.trim()) {
                          addPasskey(passkeyName.trim());
                          setPasskeyName("");
                          setShowAddPasskey(false);
                        }
                      }}
                      className="h-8 text-[13px]"
                      autoFocus
                    />
                    <Button size="sm" className="h-8 text-[12px]" onClick={() => {
                      if (passkeyName.trim()) { addPasskey(passkeyName.trim()); setPasskeyName(""); setShowAddPasskey(false); }
                    }}>Add</Button>
                    <Button size="sm" variant="ghost" className="h-8 text-[12px]" onClick={() => {
                      setShowAddPasskey(false); setPasskeyName("");
                    }}>Cancel</Button>
                  </div>
                ) : (
                  <Row last onClick={() => setShowAddPasskey(true)}>
                    <Plus className="h-4 w-4 text-muted-foreground shrink-0" />
                    <RowLabel>Add passkey</RowLabel>
                    <ChevronDown className="h-3.5 w-3.5 text-muted-foreground/40 -rotate-90" />
                  </Row>
                )}
              </Card>
            </Section>

            <Section>
              <SectionLabel>Privacy</SectionLabel>
              <Card>
                <Row>
                  <RowIcon icon={Eye} color="bg-muted text-muted-foreground" />
                  <RowLabel sub="Block tracking pixels in emails">Block trackers</RowLabel>
                  <Switch defaultChecked />
                </Row>
                <Row last>
                  <RowIcon icon={EyeOff} color="bg-muted text-muted-foreground" />
                  <RowLabel sub="Strip metadata from attachments">Sanitize attachments</RowLabel>
                  <Switch />
                </Row>
              </Card>
            </Section>
          </div>
        )}

      </div>
    </div>
  );
}
