"use client";

import { useState, useEffect, useReducer, useCallback, useRef } from "react";
import { api } from "@/lib/api";
import { useAdvancedMode } from "@/lib/advanced-mode";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import {
  CheckCircle2, Loader2, AlertTriangle, ArrowRight, ArrowLeft,
  Server, Globe, User, Shield, Settings, Inbox, Zap, Layers, Copy, Check,
  Monitor, RefreshCw, Lock, FileKey, Key, Webhook, FileCode2, Send,
  Users, ListTodo, Sparkles, BarChart3, ShieldAlert, Terminal, Eye, EyeOff,
} from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type Step = 0 | 1 | 2 | 3 | 4 | 5 | 6;

interface WizardState {
  preset: "email_only" | "api_only" | "full";
  domain: string;
  mailHostname: string;
  adminEmail: string;
  adminPassword: string;
  adminPasswordConfirm: string;
  tlsMode: "auto" | "manual";
  tlsEmail: string;
  tlsCertPath: string;
  tlsKeyPath: string;
  advancedMode: boolean;
}

type WizardAction =
  | { type: "SET_FIELD"; field: keyof WizardState; value: string }
  | { type: "SET_PRESET"; value: WizardState["preset"] }
  | { type: "SET_TLS_MODE"; value: WizardState["tlsMode"] }
  | { type: "SET_ADVANCED"; value: boolean };

interface PreflightResult {
  [key: string]: { status: string; service?: string };
}

interface DNSRecord {
  type: string;
  host: string;
  value: string;
  status: "ok" | "missing" | "checking";
  priority?: string;
}

interface DNSResults {
  mx?: { status: string; expected: string; found?: string[] };
  a_record?: { status: string; host: string; ips?: string[] };
  spf?: { status: string; record?: string; expected?: string };
  dmarc?: { status: string; record?: string; expected?: string };
}

interface InstallStep {
  label: string;
  status: "pending" | "running" | "done" | "error";
}

// ---------------------------------------------------------------------------
// Preset config — what each preset enables/shows
// ---------------------------------------------------------------------------

const presetConfig = {
  email_only: {
    label: "Email Hosting",
    icon: Inbox,
    desc: "Full mailboxes with IMAP, SMTP, calendar, and webmail",
    badges: ["IMAP", "SMTP", "CalDAV", "Webmail"],
    features: [
      { icon: Users, label: "User mailboxes", on: true },
      { icon: Globe, label: "Domain management", on: true },
      { icon: Sparkles, label: "Mail filters (Sieve)", on: true },
      { icon: ListTodo, label: "Queue management", on: true },
      { icon: ShieldAlert, label: "Security tools", on: true },
      { icon: Key, label: "API keys", on: false },
      { icon: Webhook, label: "Webhooks", on: false },
      { icon: FileCode2, label: "Templates", on: false },
    ],
    domainHint: "Your users will send and receive email at this domain.",
    adminHint: "This will be your first mailbox and admin login.",
    installSteps: [
      "Creating directories",
      "Initializing database",
      "Configuring domain",
      "Generating DKIM keys",
      "Creating admin mailbox",
      "Configuring TLS",
      "Starting IMAP & SMTP",
    ],
    successTitle: "Your mail server is ready!",
    successHint: "Start by creating user mailboxes and verifying DNS.",
    nextSteps: ["Create user accounts", "Set up webmail client", "Configure mail filters"],
  },
  api_only: {
    label: "Email API",
    icon: Zap,
    desc: "Send transactional emails via REST API with tracking",
    badges: ["REST API", "Webhooks", "Templates", "Tracking"],
    features: [
      { icon: Key, label: "API keys", on: true },
      { icon: Webhook, label: "Webhooks", on: true },
      { icon: FileCode2, label: "Email templates", on: true },
      { icon: Send, label: "Send logs & tracking", on: true },
      { icon: BarChart3, label: "Analytics", on: true },
      { icon: Globe, label: "Domain management", on: true },
      { icon: Users, label: "User mailboxes", on: false },
      { icon: Sparkles, label: "Mail filters", on: false },
    ],
    domainHint: "Emails will be sent from this domain. SPF and DKIM are critical for deliverability.",
    adminHint: "This account manages API keys and monitors delivery.",
    installSteps: [
      "Creating directories",
      "Initializing database",
      "Configuring domain",
      "Generating DKIM keys",
      "Creating admin account",
      "Configuring TLS",
      "Starting API server",
    ],
    successTitle: "Your email API is ready!",
    successHint: "Create an API key to start sending emails.",
    nextSteps: ["Create your first API key", "Set up a webhook endpoint", "Build an email template"],
  },
  full: {
    label: "Full Platform",
    icon: Layers,
    desc: "Everything: email hosting, API, mailing lists, and more",
    badges: ["Everything"],
    features: [
      { icon: Users, label: "User mailboxes", on: true },
      { icon: Key, label: "API keys", on: true },
      { icon: Webhook, label: "Webhooks", on: true },
      { icon: FileCode2, label: "Email templates", on: true },
      { icon: Send, label: "Send logs & tracking", on: true },
      { icon: Globe, label: "Domain management", on: true },
      { icon: Sparkles, label: "Mail filters (Sieve)", on: true },
      { icon: ShieldAlert, label: "Security tools", on: true },
    ],
    domainHint: "This domain powers both mailboxes and API sending.",
    adminHint: "Full admin access to mailboxes, API, and system settings.",
    installSteps: [
      "Creating directories",
      "Initializing database",
      "Configuring domain",
      "Generating DKIM keys",
      "Creating admin account",
      "Configuring TLS",
      "Starting all services",
    ],
    successTitle: "Your mail platform is ready!",
    successHint: "You have full access to hosting, API, and more.",
    nextSteps: ["Create user accounts", "Generate an API key", "Set up webhooks"],
  },
} as const;

// ---------------------------------------------------------------------------
// Reducer
// ---------------------------------------------------------------------------

function wizardReducer(state: WizardState, action: WizardAction): WizardState {
  switch (action.type) {
    case "SET_FIELD":
      return { ...state, [action.field]: action.value };
    case "SET_PRESET":
      return { ...state, preset: action.value };
    case "SET_TLS_MODE":
      return { ...state, tlsMode: action.value };
    case "SET_ADVANCED":
      return { ...state, advancedMode: action.value };
    default:
      return state;
  }
}

const initialState: WizardState = {
  preset: "full",
  domain: "",
  mailHostname: "",
  adminEmail: "",
  adminPassword: "",
  adminPasswordConfirm: "",
  tlsMode: "auto",
  tlsEmail: "",
  tlsCertPath: "",
  tlsKeyPath: "",
  advancedMode: false,
};

// ---------------------------------------------------------------------------
// Step metadata
// ---------------------------------------------------------------------------

const stepMeta: { label: string; icon: React.ComponentType<{ className?: string }> }[] = [
  { label: "Welcome", icon: Server },
  { label: "Environment", icon: Monitor },
  { label: "Preset", icon: Settings },
  { label: "Domain", icon: Globe },
  { label: "Admin", icon: User },
  { label: "TLS", icon: Shield },
  { label: "Review", icon: CheckCircle2 },
];

// ---------------------------------------------------------------------------
// Sub-components
// ---------------------------------------------------------------------------

function StepSidebar({
  current,
  setCurrent,
  state,
}: {
  current: Step;
  setCurrent: (s: Step) => void;
  state: WizardState;
}) {
  const progress = ((current) / (stepMeta.length - 1)) * 100;

  function sublabel(i: number): string | null {
    if (i === 2 && current > 2) return presetConfig[state.preset].label;
    if (i === 3 && current > 3 && state.domain) return state.domain;
    if (i === 5 && current > 5) return state.tlsMode === "auto" ? "Let's Encrypt" : "Manual";
    return null;
  }

  return (
    <aside className="hidden md:flex w-52 shrink-0 flex-col border-r border-border bg-sidebar">
      {/* Logo */}
      <div className="flex items-center gap-2 px-3 pt-2 pb-2">
        <div className="flex h-7 w-7 items-center justify-center rounded-md bg-primary/10">
          <Server className="h-3.5 w-3.5 text-primary" strokeWidth={2} />
        </div>
        <span className="text-[13px] font-semibold tracking-tight">Setup</span>
      </div>

      {/* Progress bar */}
      <div className="mx-3 h-[3px] rounded-full bg-muted overflow-hidden">
        <motion.div
          className="h-full rounded-full bg-primary"
          initial={{ width: 0 }}
          animate={{ width: `${progress}%` }}
          transition={{ duration: 0.3 }}
        />
      </div>

      <nav className="flex-1 px-2 pt-3 space-y-px">
        {stepMeta.map((step, i) => {
          const done = i < current;
          const active = i === current;
          const sub = sublabel(i);

          return (
            <button
              key={i}
              onClick={() => i <= current && setCurrent(i as Step)}
              className={`flex items-center gap-2 w-full rounded-md px-2 py-1.5 text-[13px] transition-colors duration-100 ${
                active
                  ? "bg-accent text-foreground font-medium"
                  : done
                  ? "text-muted-foreground hover:bg-accent/50 hover:text-foreground cursor-pointer"
                  : "text-muted-foreground/40 cursor-default"
              }`}
            >
              <span className={`flex h-4 w-4 shrink-0 items-center justify-center rounded text-[10px] font-medium ${
                done
                  ? "bg-emerald-500/15 text-emerald-500"
                  : active
                  ? "bg-primary/15 text-primary"
                  : "text-muted-foreground/30"
              }`}>
                {done ? <Check className="h-3 w-3" /> : i + 1}
              </span>
              <span className="truncate">{step.label}</span>
              {sub && (
                <span className="ml-auto text-[10px] text-muted-foreground/50 truncate max-w-[60px]">{sub}</span>
              )}
            </button>
          );
        })}
      </nav>

      {/* Preset indicator at bottom */}
      {current > 2 && (
        <div className="px-3 pb-3">
          <div className="rounded-md bg-muted/50 px-2.5 py-2">
            <p className="text-[10px] font-medium uppercase tracking-widest text-muted-foreground/50 mb-1">Mode</p>
            <div className="flex items-center gap-1.5">
              {(() => {
                const Icon = presetConfig[state.preset].icon;
                return <Icon className="h-3 w-3 text-primary" />;
              })()}
              <span className="text-[12px] font-medium">{presetConfig[state.preset].label}</span>
            </div>
          </div>
        </div>
      )}
    </aside>
  );
}

function MobileProgressBar({ current }: { current: Step }) {
  return (
    <div className="flex md:hidden items-center justify-between px-4 py-3 border-b border-border bg-sidebar">
      <span className="text-[12px] text-muted-foreground">
        Step {current + 1} of {stepMeta.length}
      </span>
      <div className="flex gap-1.5">
        {stepMeta.map((_, i) => (
          <div
            key={i}
            className={`h-1.5 w-1.5 rounded-full transition-colors ${
              i <= current ? "bg-primary" : "bg-muted"
            }`}
          />
        ))}
      </div>
    </div>
  );
}

function CheckRow({
  label,
  status,
  detail,
}: {
  label: string;
  status: "checking" | "available" | "connected" | "unavailable" | "error";
  detail?: string;
}) {
  return (
    <div className="activity-row flex items-center gap-3 rounded-md px-3 py-2.5">
      <div className="flex h-5 w-5 items-center justify-center">
        {status === "checking" ? (
          <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
        ) : status === "available" || status === "connected" ? (
          <CheckCircle2 className="h-4 w-4 text-emerald-500" />
        ) : (
          <AlertTriangle className="h-4 w-4 text-red-400" />
        )}
      </div>
      <div className="flex-1 min-w-0">
        <span className="text-[13px]">{label}</span>
      </div>
      <span className={`text-[12px] ${
        status === "available" || status === "connected" ? "text-emerald-500" :
        status === "checking" ? "text-muted-foreground" :
        "text-red-400"
      }`}>
        {detail || (status === "checking" ? "Checking..." : status === "available" ? "Available" : status === "connected" ? "Connected" : "Unavailable")}
      </span>
    </div>
  );
}

function DNSRecordRow({ record }: { record: DNSRecord }) {
  const [copied, setCopied] = useState(false);

  const copy = () => {
    navigator.clipboard.writeText(record.value);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div className="activity-row grid grid-cols-[50px_1fr_1fr_70px] items-center gap-2 px-3 py-2.5 text-[12px]">
      <span className="font-mono font-medium text-foreground">{record.type}</span>
      <span className="text-muted-foreground truncate">{record.host}</span>
      <div className="flex items-center gap-1.5 min-w-0">
        <span className="font-mono text-foreground truncate">{record.value}</span>
        <button
          onClick={copy}
          className="shrink-0 p-0.5 rounded hover:bg-accent transition-colors"
          title="Copy"
        >
          {copied ? (
            <Check className="h-3 w-3 text-emerald-500" />
          ) : (
            <Copy className="h-3 w-3 text-muted-foreground" />
          )}
        </button>
      </div>
      <div className="flex justify-end">
        {record.status === "checking" ? (
          <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />
        ) : record.status === "ok" ? (
          <CheckCircle2 className="h-3.5 w-3.5 text-emerald-500" />
        ) : (
          <AlertTriangle className="h-3.5 w-3.5 text-amber-500" />
        )}
      </div>
    </div>
  );
}

function PasswordStrengthBar({ password }: { password: string }) {
  const score = [
    password.length >= 8,
    /[A-Z]/.test(password),
    /[0-9]/.test(password),
    /[^A-Za-z0-9]/.test(password),
  ].filter(Boolean).length;

  if (!password) return null;

  const color = score <= 2 ? "bg-red-500" : score === 3 ? "bg-amber-500" : "bg-emerald-500";
  const labels = ["", "Weak", "Weak", "Good", "Strong"];

  return (
    <div className="space-y-1 mt-2">
      <div className="flex gap-1">
        {[1, 2, 3, 4].map((i) => (
          <div
            key={i}
            className={`h-1 flex-1 rounded-full transition-colors ${
              i <= score ? color : "bg-muted"
            }`}
          />
        ))}
      </div>
      <span className={`text-[10px] ${score <= 2 ? "text-red-400" : score === 3 ? "text-amber-400" : "text-emerald-400"}`}>
        {labels[score]}
      </span>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Main Component
// ---------------------------------------------------------------------------

export default function SetupPage() {
  const [current, setCurrent] = useState<Step>(0);
  const [needsSetup, setNeedsSetup] = useState<boolean | null>(null);
  const [state, dispatch] = useReducer(wizardReducer, initialState);
  const advancedMode = useAdvancedMode();

  // Environment check
  const [preflight, setPreflight] = useState<PreflightResult | null>(null);
  const [preflightLoading, setPreflightLoading] = useState(false);

  // DNS
  const [dnsResults, setDnsResults] = useState<DNSResults | null>(null);
  const [dnsLastChecked, setDnsLastChecked] = useState<number | null>(null);
  const dnsInterval = useRef<ReturnType<typeof setInterval> | null>(null);

  // Install
  const [installSteps, setInstallSteps] = useState<InstallStep[]>([]);
  const [installing, setInstalling] = useState(false);
  const [installResult, setInstallResult] = useState<{ success: boolean; admin_email?: string } | null>(null);
  const [installError, setInstallError] = useState("");

  // Password
  const [confirmTouched, setConfirmTouched] = useState(false);
  const [showPassword, setShowPassword] = useState(false);

  // Current preset config
  const preset = presetConfig[state.preset];

  // ---------------------------------------------------------------------------
  // Setup status check
  // ---------------------------------------------------------------------------

  useEffect(() => {
    advancedMode.hydrate();
    api.get<{ needs_setup: boolean }>("/v1/setup/status").then((res) => {
      if (res.success && res.data) {
        setNeedsSetup(res.data.needs_setup);
        if (!res.data.needs_setup) window.location.href = "/admin/";
      }
    });
  }, []);

  // ---------------------------------------------------------------------------
  // Auto-derive fields
  // ---------------------------------------------------------------------------

  useEffect(() => {
    if (state.domain) {
      dispatch({ type: "SET_FIELD", field: "mailHostname", value: `mail.${state.domain}` });
      if (!state.adminEmail) {
        dispatch({ type: "SET_FIELD", field: "adminEmail", value: `admin@${state.domain}` });
      }
    }
  }, [state.domain]);

  // ---------------------------------------------------------------------------
  // Preflight
  // ---------------------------------------------------------------------------

  const runPreflight = useCallback(async () => {
    setPreflightLoading(true);
    const res = await api.post<PreflightResult>("/v1/setup/preflight", {});
    if (res.success && res.data) setPreflight(res.data);
    setPreflightLoading(false);
  }, []);

  useEffect(() => {
    if (current === 1 && !preflight) runPreflight();
  }, [current, preflight, runPreflight]);

  // ---------------------------------------------------------------------------
  // DNS
  // ---------------------------------------------------------------------------

  const checkDNS = useCallback(async () => {
    if (!state.domain) return;
    const res = await api.post<DNSResults>("/v1/setup/check-dns", {
      domain: state.domain,
      mail_hostname: state.mailHostname,
    });
    if (res.success && res.data) {
      setDnsResults(res.data);
      setDnsLastChecked(Date.now());
    }
  }, [state.domain, state.mailHostname]);

  useEffect(() => {
    if (current === 3 && state.domain) {
      checkDNS();
      dnsInterval.current = setInterval(checkDNS, 30000);
    }
    return () => {
      if (dnsInterval.current) clearInterval(dnsInterval.current);
    };
  }, [current, state.domain, checkDNS]);

  // DNS records — filtered by preset
  const dnsRecords: DNSRecord[] = state.domain ? [
    // MX only relevant for email_only and full (receiving mail)
    ...(state.preset !== "api_only" ? [{
      type: "MX",
      host: state.domain,
      value: `mail.${state.domain}`,
      status: (dnsResults?.mx?.status === "ok" ? "ok" : dnsResults ? "missing" : "checking") as DNSRecord["status"],
      priority: "10",
    }] : []),
    {
      type: "A",
      host: `mail.${state.domain}`,
      value: "your-server-ip",
      status: (dnsResults?.a_record?.status === "ok" ? "ok" : dnsResults ? "missing" : "checking") as DNSRecord["status"],
    },
    {
      type: "TXT",
      host: state.domain,
      value: "v=spf1 mx -all",
      status: (dnsResults?.spf?.status === "ok" ? "ok" : dnsResults ? "missing" : "checking") as DNSRecord["status"],
    },
    {
      type: "TXT",
      host: `_dmarc.${state.domain}`,
      value: `v=DMARC1; p=quarantine; rua=mailto:postmaster@${state.domain}`,
      status: (dnsResults?.dmarc?.status === "ok" ? "ok" : dnsResults ? "missing" : "checking") as DNSRecord["status"],
    },
  ] : [];

  // ---------------------------------------------------------------------------
  // Install
  // ---------------------------------------------------------------------------

  const install = async () => {
    if (state.adminPassword !== state.adminPasswordConfirm) {
      setInstallError("Passwords do not match");
      return;
    }

    // Apply advanced mode preference
    if (state.advancedMode) advancedMode.toggle();

    setInstalling(true);
    setInstallError("");

    const steps: InstallStep[] = preset.installSteps.map((l) => ({ label: l, status: "pending" }));
    setInstallSteps(steps);

    const apiPromise = api.post<{ success: boolean; admin_email: string }>("/v1/setup/install", {
      domain: state.domain,
      mail_hostname: state.mailHostname,
      admin_email: state.adminEmail,
      admin_password: state.adminPassword,
      preset: state.preset,
      tls_mode: state.tlsMode,
      tls_email: state.tlsEmail,
    });

    let idx = 0;
    const tickInterval = setInterval(() => {
      setInstallSteps((prev) => {
        const next = [...prev];
        if (idx > 0 && idx <= next.length) next[idx - 1] = { ...next[idx - 1], status: "done" };
        if (idx < next.length) next[idx] = { ...next[idx], status: "running" };
        return next;
      });
      idx++;
      if (idx > steps.length) clearInterval(tickInterval);
    }, 600);

    const res = await apiPromise;
    clearInterval(tickInterval);

    if (res.success && res.data) {
      setInstallSteps((prev) => prev.map((s) => ({ ...s, status: "done" as const })));
      setTimeout(() => {
        setInstallResult(res.data!);
        setInstalling(false);
      }, 400);
    } else {
      setInstallSteps((prev) =>
        prev.map((s) =>
          s.status === "running" ? { ...s, status: "error" as const } : s
        )
      );
      setInstallError(res.error || "Installation failed");
      setInstalling(false);
    }
  };

  // ---------------------------------------------------------------------------
  // Navigation
  // ---------------------------------------------------------------------------

  const canNext = (): boolean => {
    switch (current) {
      case 3: return state.domain.length > 0;
      case 4: return state.adminEmail.length > 0 && state.adminPassword.length >= 8 && state.adminPassword === state.adminPasswordConfirm;
      default: return true;
    }
  };

  const goNext = () => { if (current < 6 && canNext()) setCurrent((current + 1) as Step); };
  const goBack = () => { if (current > 0) setCurrent((current - 1) as Step); };

  // Time ago ticker
  const [, forceUpdate] = useState(0);
  useEffect(() => {
    if (!dnsLastChecked) return;
    const t = setInterval(() => forceUpdate((n) => n + 1), 1000);
    return () => clearInterval(t);
  }, [dnsLastChecked]);
  const timeAgo = dnsLastChecked ? Math.round((Date.now() - dnsLastChecked) / 1000) : null;

  // ---------------------------------------------------------------------------
  // Loading
  // ---------------------------------------------------------------------------

  if (needsSetup === null) {
    return (
      <div className="flex h-screen items-center justify-center bg-background">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground/50" />
      </div>
    );
  }

  // ---------------------------------------------------------------------------
  // Success screen — preset-specific
  // ---------------------------------------------------------------------------

  if (installResult?.success) {
    return (
      <div className="flex h-screen items-center justify-center bg-background relative">
        <div className="absolute inset-0 overflow-hidden">
          <div className="absolute left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 w-[600px] h-[600px] rounded-full bg-primary/[0.04] blur-[100px]" />
        </div>
        <motion.div
          initial={{ opacity: 0, scale: 0.9 }}
          animate={{ opacity: 1, scale: 1 }}
          transition={{ duration: 0.5, ease: "easeOut" }}
          className="relative max-w-md text-center space-y-6"
        >
          <motion.div
            initial={{ scale: 0 }}
            animate={{ scale: 1 }}
            transition={{ delay: 0.2, duration: 0.5, type: "spring", stiffness: 200 }}
            className="mx-auto flex h-20 w-20 items-center justify-center rounded-full bg-emerald-500/10 ring-1 ring-emerald-500/20"
          >
            <CheckCircle2 className="h-10 w-10 text-emerald-500" />
          </motion.div>
          <div>
            <h1 className="text-2xl font-semibold tracking-tight">{preset.successTitle}</h1>
            <p className="text-sm text-muted-foreground mt-2">{preset.successHint}</p>
            <p className="text-sm text-muted-foreground mt-1">
              Sign in with <span className="font-mono text-foreground">{installResult.admin_email}</span>
            </p>
          </div>

          {/* Preset-specific next steps */}
          <div className="rounded-xl border border-border bg-card p-4 text-left">
            <p className="text-[11px] font-medium uppercase tracking-widest text-muted-foreground/50 mb-3">Next steps</p>
            <div className="space-y-2">
              {preset.nextSteps.map((step, i) => (
                <div key={i} className="flex items-center gap-2 text-[13px]">
                  <span className="flex h-5 w-5 items-center justify-center rounded-full bg-primary/10 text-[10px] font-bold text-primary">
                    {i + 1}
                  </span>
                  <span>{step}</span>
                </div>
              ))}
            </div>
          </div>

          <a href="/admin/login/">
            <Button className="h-10 px-6 text-[13px] font-medium">
              Open Dashboard <ArrowRight className="h-4 w-4 ml-1" />
            </Button>
          </a>
        </motion.div>
      </div>
    );
  }

  const dnsVerifiedCount = dnsRecords.filter((r) => r.status === "ok").length;

  // ---------------------------------------------------------------------------
  // Render
  // ---------------------------------------------------------------------------

  return (
    <div className="flex h-screen bg-background">
      <StepSidebar current={current} setCurrent={setCurrent} state={state} />

      <div className="flex-1 flex flex-col min-w-0">
        <MobileProgressBar current={current} />

        <div className="flex-1 overflow-auto">
          <AnimatePresence mode="wait">
            <motion.div
              key={current}
              initial={{ opacity: 0, y: 8 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, y: -8 }}
              transition={{ duration: 0.2 }}
              className="max-w-2xl mx-auto w-full p-8"
            >
              {/* ==================== Step 0: Welcome ==================== */}
              {current === 0 && (
                <div className="flex flex-col items-center justify-center min-h-[60vh] text-center">
                  <div className="relative mb-8">
                    <div className="absolute inset-0 -m-20 rounded-full bg-primary/[0.04] blur-[100px]" />
                    <div className="relative flex h-16 w-16 items-center justify-center rounded-2xl bg-card border border-border login-glow">
                      <Server className="h-7 w-7 text-foreground" strokeWidth={1.5} />
                    </div>
                  </div>
                  <h1 className="text-3xl font-semibold tracking-tight mb-2">Your self-hosted email platform</h1>
                  <p className="text-muted-foreground text-[15px] mb-10">Production-ready. Open source. Yours.</p>

                  <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 w-full max-w-lg mb-10">
                    {[
                      { icon: Inbox, title: "Email Hosting", desc: "IMAP, SMTP, calendars" },
                      { icon: Zap, title: "Transactional API", desc: "REST, webhooks, templates" },
                      { icon: Layers, title: "Full Platform", desc: "Everything included" },
                    ].map((tile) => (
                      <div key={tile.title} className="stat-card rounded-xl border border-border bg-card p-4 text-left">
                        <tile.icon className="h-5 w-5 text-muted-foreground mb-2" />
                        <p className="text-[13px] font-medium">{tile.title}</p>
                        <p className="text-[11px] text-muted-foreground mt-0.5">{tile.desc}</p>
                      </div>
                    ))}
                  </div>

                  <Button onClick={goNext} className="h-10 px-6 text-[13px] font-medium">
                    Get Started <ArrowRight className="h-4 w-4 ml-1" />
                  </Button>
                </div>
              )}

              {/* ==================== Step 1: Environment ==================== */}
              {current === 1 && (
                <div className="space-y-6">
                  <div>
                    <h1 className="text-xl font-semibold tracking-tight">Environment Check</h1>
                    <p className="text-sm text-muted-foreground mt-1">Verifying your system is ready for installation.</p>
                  </div>

                  <div className="rounded-xl border border-border bg-card divide-y divide-border overflow-hidden">
                    {preflight ? (
                      Object.entries(preflight).map(([key, val]) => {
                        const labels: Record<string, string> = {
                          port_25: "Port 25 (SMTP)",
                          port_587: "Port 587 (Submission)",
                          port_993: "Port 993 (IMAPS)",
                          redis: "Redis",
                          database: "Database",
                        };
                        return (
                          <CheckRow
                            key={key}
                            label={labels[key] || key}
                            status={val.status as "available" | "connected" | "unavailable"}
                            detail={val.service}
                          />
                        );
                      })
                    ) : (
                      ["Port 25 (SMTP)", "Port 587 (Submission)", "Port 993 (IMAPS)", "Redis", "Database"].map((l) => (
                        <CheckRow key={l} label={l} status="checking" />
                      ))
                    )}
                  </div>

                  {preflight && Object.values(preflight).some((v) => v.status !== "available" && v.status !== "connected") && (
                    <div className="rounded-lg border border-amber-500/30 bg-amber-500/5 px-4 py-3 text-[12px] text-amber-400 flex items-start gap-2">
                      <AlertTriangle className="h-4 w-4 shrink-0 mt-0.5" />
                      <span>Some checks failed. You can continue if you&apos;re in a dev or Docker environment.</span>
                    </div>
                  )}

                  <button
                    onClick={runPreflight}
                    disabled={preflightLoading}
                    className="flex items-center gap-1.5 text-[12px] text-muted-foreground hover:text-foreground transition-colors"
                  >
                    <RefreshCw className={`h-3 w-3 ${preflightLoading ? "animate-spin" : ""}`} />
                    Re-run checks
                  </button>
                </div>
              )}

              {/* ==================== Step 2: Preset ==================== */}
              {current === 2 && (
                <div className="space-y-6">
                  <div>
                    <h1 className="text-xl font-semibold tracking-tight">Choose Your Setup</h1>
                    <p className="text-sm text-muted-foreground mt-1">Pick a starting point — you can enable any feature later in Settings.</p>
                  </div>

                  <div className="grid gap-3">
                    {(["email_only", "api_only", "full"] as const).map((id) => {
                      const p = presetConfig[id];
                      const selected = state.preset === id;
                      return (
                        <button
                          key={id}
                          onClick={() => dispatch({ type: "SET_PRESET", value: id })}
                          className={`relative text-left rounded-xl border p-5 transition-all duration-150 ${
                            selected
                              ? "border-primary bg-primary/[0.08] ring-2 ring-primary/30"
                              : "border-border bg-card hover:bg-accent/30"
                          }`}
                        >
                          {/* Selected checkmark */}
                          {selected && (
                            <div className="absolute top-3 right-3 flex h-5 w-5 items-center justify-center rounded-full bg-primary">
                              <Check className="h-3 w-3 text-primary-foreground" />
                            </div>
                          )}
                          <div className="flex items-start gap-3">
                            <div className={`flex h-9 w-9 items-center justify-center rounded-lg shrink-0 ${
                              selected ? "bg-primary/15" : "bg-muted"
                            }`}>
                              <p.icon className={`h-4.5 w-4.5 ${selected ? "text-primary" : "text-muted-foreground"}`} />
                            </div>
                            <div className="flex-1 min-w-0 pr-5">
                              <p className={`text-[14px] font-medium ${selected ? "text-foreground" : ""}`}>{p.label}</p>
                              <p className="text-[12px] text-muted-foreground mt-0.5">{p.desc}</p>
                              <div className="flex flex-wrap gap-1.5 mt-3">
                                {p.badges.map((b) => (
                                  <Badge key={b} variant={selected ? "default" : "secondary"} className="text-[10px] h-[18px] px-1.5">{b}</Badge>
                                ))}
                              </div>
                            </div>
                          </div>
                        </button>
                      );
                    })}
                  </div>

                  {/* Feature breakdown for selected preset */}
                  <div className="rounded-xl border border-border bg-card overflow-hidden">
                    <div className="px-4 py-2.5 border-b border-border bg-muted/30 flex items-center justify-between">
                      <p className="text-[12px] font-medium text-muted-foreground">
                        Enabled with {preset.label}
                      </p>
                      <p className="text-[10px] text-muted-foreground/50">
                        All features available in Settings
                      </p>
                    </div>
                    <div className="grid grid-cols-2 gap-px bg-border">
                      {preset.features.map((f) => (
                        <div key={f.label} className="flex items-center gap-2 px-3 py-2 bg-card">
                          <f.icon className={`h-3.5 w-3.5 shrink-0 ${f.on ? "text-foreground" : "text-muted-foreground/25"}`} />
                          <span className={`text-[12px] ${f.on ? "text-foreground" : "text-muted-foreground/35"}`}>
                            {f.label}
                          </span>
                          {f.on ? (
                            <Check className="h-3 w-3 text-emerald-500 ml-auto shrink-0" />
                          ) : (
                            <span className="text-[9px] text-muted-foreground/30 ml-auto">later</span>
                          )}
                        </div>
                      ))}
                    </div>
                  </div>
                </div>
              )}

              {/* ==================== Step 3: Domain ==================== */}
              {current === 3 && (
                <div className="space-y-6">
                  <div>
                    <h1 className="text-xl font-semibold tracking-tight">Domain Configuration</h1>
                    <p className="text-sm text-muted-foreground mt-1">{preset.domainHint}</p>
                  </div>

                  <div className="space-y-4">
                    <div className="space-y-1.5">
                      <Label htmlFor="domain" className="text-[12px] text-muted-foreground font-normal">
                        Primary Domain
                      </Label>
                      <Input
                        id="domain"
                        placeholder="example.com"
                        value={state.domain}
                        onChange={(e) => dispatch({ type: "SET_FIELD", field: "domain", value: e.target.value })}
                        className="h-9 text-[13px] bg-background/50 border-border placeholder:text-muted-foreground/40"
                        autoFocus
                      />
                    </div>
                    <div className="space-y-1.5">
                      <Label htmlFor="mailhost" className="text-[12px] text-muted-foreground font-normal">
                        Mail Hostname
                      </Label>
                      <Input
                        id="mailhost"
                        value={state.mailHostname}
                        onChange={(e) => dispatch({ type: "SET_FIELD", field: "mailHostname", value: e.target.value })}
                        className="h-9 text-[13px] bg-background/50 border-border"
                      />
                    </div>
                  </div>

                  {state.domain && (
                    <div className="space-y-3">
                      <div className="flex items-center justify-between">
                        <h3 className="text-[13px] font-medium">Required DNS Records</h3>
                        {timeAgo !== null && (
                          <span className="text-[11px] text-muted-foreground">
                            Last checked {timeAgo}s ago
                          </span>
                        )}
                      </div>

                      {/* Preset-specific DNS hint */}
                      {state.preset === "api_only" && (
                        <p className="text-[11px] text-muted-foreground bg-muted/50 rounded-md px-3 py-2">
                          SPF and DMARC are critical for API email deliverability. Without them, sent emails may land in spam.
                        </p>
                      )}

                      <div className="rounded-xl border border-border bg-card overflow-hidden">
                        <div className="grid grid-cols-[50px_1fr_1fr_70px] gap-2 px-3 py-2 text-[11px] text-muted-foreground font-medium border-b border-border bg-muted/30">
                          <span>Type</span>
                          <span>Host</span>
                          <span>Value</span>
                          <span className="text-right">Status</span>
                        </div>
                        {dnsRecords.map((record, i) => (
                          <DNSRecordRow key={i} record={record} />
                        ))}
                      </div>

                      <p className="text-[11px] text-muted-foreground">
                        DNS propagation can take up to 48 hours. You can configure these later.
                      </p>
                    </div>
                  )}
                </div>
              )}

              {/* ==================== Step 4: Admin ==================== */}
              {current === 4 && (
                <div className="space-y-6">
                  <div>
                    <h1 className="text-xl font-semibold tracking-tight">Admin Account</h1>
                    <p className="text-sm text-muted-foreground mt-1">{preset.adminHint}</p>
                  </div>

                  <div className="space-y-4">
                    <div className="space-y-1.5">
                      <Label htmlFor="adminEmail" className="text-[12px] text-muted-foreground font-normal">
                        Admin Email
                      </Label>
                      <Input
                        id="adminEmail"
                        type="email"
                        value={state.adminEmail}
                        onChange={(e) => dispatch({ type: "SET_FIELD", field: "adminEmail", value: e.target.value })}
                        className="h-9 text-[13px] bg-background/50 border-border"
                      />
                    </div>
                    <div className="space-y-1.5">
                      <Label htmlFor="adminPass" className="text-[12px] text-muted-foreground font-normal">
                        Password
                      </Label>
                      <div className="relative">
                        <Input
                          id="adminPass"
                          type={showPassword ? "text" : "password"}
                          value={state.adminPassword}
                          onChange={(e) => dispatch({ type: "SET_FIELD", field: "adminPassword", value: e.target.value })}
                          placeholder="Min 8 characters"
                          className="h-9 text-[13px] bg-background/50 border-border placeholder:text-muted-foreground/40 pr-9"
                        />
                        <button
                          type="button"
                          onClick={() => setShowPassword(!showPassword)}
                          className="absolute right-2 top-1/2 -translate-y-1/2 p-0.5 text-muted-foreground hover:text-foreground transition-colors"
                        >
                          {showPassword ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                        </button>
                      </div>
                      <PasswordStrengthBar password={state.adminPassword} />
                    </div>
                    <div className="space-y-1.5">
                      <Label htmlFor="adminPassConfirm" className="text-[12px] text-muted-foreground font-normal">
                        Confirm Password
                      </Label>
                      <Input
                        id="adminPassConfirm"
                        type={showPassword ? "text" : "password"}
                        value={state.adminPasswordConfirm}
                        onChange={(e) => dispatch({ type: "SET_FIELD", field: "adminPasswordConfirm", value: e.target.value })}
                        onBlur={() => setConfirmTouched(true)}
                        className="h-9 text-[13px] bg-background/50 border-border"
                      />
                      {confirmTouched && state.adminPasswordConfirm && (
                        <div className="flex items-center gap-1 mt-1">
                          {state.adminPassword === state.adminPasswordConfirm ? (
                            <>
                              <Check className="h-3 w-3 text-emerald-500" />
                              <span className="text-[11px] text-emerald-500">Passwords match</span>
                            </>
                          ) : (
                            <span className="text-[11px] text-red-400">Passwords do not match</span>
                          )}
                        </div>
                      )}
                    </div>
                  </div>
                </div>
              )}

              {/* ==================== Step 5: TLS ==================== */}
              {current === 5 && (
                <div className="space-y-6">
                  <div>
                    <h1 className="text-xl font-semibold tracking-tight">TLS Certificates</h1>
                    <p className="text-sm text-muted-foreground mt-1">
                      {state.preset === "api_only"
                        ? "Encrypt your API endpoints and outbound email."
                        : "Encrypt IMAP, SMTP, and admin panel connections."}
                    </p>
                  </div>

                  <div className="grid gap-3">
                    {([
                      { id: "auto" as const, icon: Lock, title: "Automatic (Let's Encrypt)", desc: "Free certificates, automatically renewed" },
                      { id: "manual" as const, icon: FileKey, title: "Manual", desc: "Provide your own certificate files" },
                    ]).map((opt) => (
                      <button
                        key={opt.id}
                        onClick={() => dispatch({ type: "SET_TLS_MODE", value: opt.id })}
                        className={`stat-card text-left rounded-xl border p-5 transition-all ${
                          state.tlsMode === opt.id
                            ? "border-primary bg-primary/5 ring-1 ring-primary/20"
                            : "border-border bg-card hover:border-border/80"
                        }`}
                      >
                        <div className="flex items-center gap-3">
                          <div className={`flex h-9 w-9 items-center justify-center rounded-lg ${
                            state.tlsMode === opt.id ? "bg-primary/10" : "bg-muted"
                          }`}>
                            <opt.icon className={`h-4.5 w-4.5 ${state.tlsMode === opt.id ? "text-primary" : "text-muted-foreground"}`} />
                          </div>
                          <div>
                            <p className="text-[14px] font-medium">{opt.title}</p>
                            <p className="text-[12px] text-muted-foreground mt-0.5">{opt.desc}</p>
                          </div>
                        </div>
                      </button>
                    ))}
                  </div>

                  <AnimatePresence mode="wait">
                    {state.tlsMode === "auto" && (
                      <motion.div
                        key="auto"
                        initial={{ opacity: 0, height: 0 }}
                        animate={{ opacity: 1, height: "auto" }}
                        exit={{ opacity: 0, height: 0 }}
                        transition={{ duration: 0.2 }}
                        className="overflow-hidden"
                      >
                        <div className="space-y-3 pt-2">
                          <div className="space-y-1.5">
                            <Label htmlFor="tlsEmail" className="text-[12px] text-muted-foreground font-normal">
                              Email for expiry notices
                            </Label>
                            <Input
                              id="tlsEmail"
                              type="email"
                              value={state.tlsEmail}
                              onChange={(e) => dispatch({ type: "SET_FIELD", field: "tlsEmail", value: e.target.value })}
                              placeholder="admin@example.com"
                              className="h-9 text-[13px] bg-background/50 border-border placeholder:text-muted-foreground/40"
                            />
                          </div>
                          <p className="text-[11px] text-muted-foreground">
                            Port 80 must be accessible for HTTP-01 challenge validation.
                          </p>
                        </div>
                      </motion.div>
                    )}
                    {state.tlsMode === "manual" && (
                      <motion.div
                        key="manual"
                        initial={{ opacity: 0, height: 0 }}
                        animate={{ opacity: 1, height: "auto" }}
                        exit={{ opacity: 0, height: 0 }}
                        transition={{ duration: 0.2 }}
                        className="overflow-hidden"
                      >
                        <div className="space-y-4 pt-2">
                          <div className="space-y-1.5">
                            <Label htmlFor="certPath" className="text-[12px] text-muted-foreground font-normal">
                              Certificate Path
                            </Label>
                            <Input
                              id="certPath"
                              value={state.tlsCertPath}
                              onChange={(e) => dispatch({ type: "SET_FIELD", field: "tlsCertPath", value: e.target.value })}
                              placeholder="/etc/ssl/certs/mail.pem"
                              className="h-9 text-[13px] bg-background/50 border-border font-mono placeholder:text-muted-foreground/40"
                            />
                          </div>
                          <div className="space-y-1.5">
                            <Label htmlFor="keyPath" className="text-[12px] text-muted-foreground font-normal">
                              Private Key Path
                            </Label>
                            <Input
                              id="keyPath"
                              value={state.tlsKeyPath}
                              onChange={(e) => dispatch({ type: "SET_FIELD", field: "tlsKeyPath", value: e.target.value })}
                              placeholder="/etc/ssl/private/mail.key"
                              className="h-9 text-[13px] bg-background/50 border-border font-mono placeholder:text-muted-foreground/40"
                            />
                          </div>
                        </div>
                      </motion.div>
                    )}
                  </AnimatePresence>
                </div>
              )}

              {/* ==================== Step 6: Review ==================== */}
              {current === 6 && !installing && !installResult && (
                <div className="space-y-6">
                  <div>
                    <h1 className="text-xl font-semibold tracking-tight">Review & Install</h1>
                    <p className="text-sm text-muted-foreground mt-1">Confirm your settings and start the installation.</p>
                  </div>

                  {/* Settings summary */}
                  <div className="rounded-xl border border-border bg-card divide-y divide-border overflow-hidden">
                    {[
                      { label: "Preset", value: preset.label },
                      { label: "Domain", value: state.domain },
                      { label: "Mail Hostname", value: state.mailHostname },
                      { label: "Admin Email", value: state.adminEmail },
                      { label: "TLS", value: state.tlsMode === "auto" ? "Let's Encrypt (auto)" : "Manual certificates" },
                    ].map((row) => (
                      <div key={row.label} className="flex items-center justify-between px-4 py-3">
                        <span className="text-[12px] text-muted-foreground">{row.label}</span>
                        <span className="text-[13px] font-medium font-mono">{row.value}</span>
                      </div>
                    ))}
                  </div>

                  {/* Enabled features summary */}
                  <div className="rounded-xl border border-border bg-card p-4">
                    <div className="flex items-center justify-between mb-3">
                      <p className="text-[11px] font-medium uppercase tracking-widest text-muted-foreground/50">
                        Enabled features
                      </p>
                      <p className="text-[10px] text-muted-foreground/40">Change anytime in Settings</p>
                    </div>
                    <div className="flex flex-wrap gap-x-4 gap-y-1.5">
                      {preset.features.filter((f) => f.on).map((f) => (
                        <div key={f.label} className="flex items-center gap-1.5 text-[12px]">
                          <Check className="h-3 w-3 text-emerald-500" />
                          <span>{f.label}</span>
                        </div>
                      ))}
                    </div>
                    {preset.features.some((f) => !f.on) && (
                      <p className="text-[11px] text-muted-foreground/40 mt-2.5">
                        + {preset.features.filter((f) => !f.on).map((f) => f.label).join(", ")} can be enabled later
                      </p>
                    )}
                  </div>

                  {/* DNS status */}
                  {dnsResults && (
                    <div className="flex items-center gap-2 text-[12px] text-muted-foreground">
                      {dnsVerifiedCount === dnsRecords.length ? (
                        <CheckCircle2 className="h-3.5 w-3.5 text-emerald-500" />
                      ) : (
                        <AlertTriangle className="h-3.5 w-3.5 text-amber-500" />
                      )}
                      <span>{dnsVerifiedCount}/{dnsRecords.length} DNS records verified</span>
                    </div>
                  )}

                  {/* Advanced mode toggle */}
                  <div className="rounded-xl border border-border bg-card p-4">
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-2">
                        <Terminal className="h-4 w-4 text-muted-foreground" />
                        <div>
                          <p className="text-[13px] font-medium">Advanced Mode</p>
                          <p className="text-[11px] text-muted-foreground">
                            Show logs, DNS tools, mail filters, backup, and other power-user options in the sidebar.
                          </p>
                        </div>
                      </div>
                      <button
                        onClick={() => dispatch({ type: "SET_ADVANCED", value: !state.advancedMode })}
                        className={`relative inline-flex h-5 w-9 shrink-0 cursor-pointer items-center rounded-full border-2 border-transparent transition-colors ${
                          state.advancedMode ? "bg-primary" : "bg-muted"
                        }`}
                      >
                        <span className={`pointer-events-none block h-3.5 w-3.5 rounded-full bg-background shadow-sm transition-transform ${
                          state.advancedMode ? "translate-x-4" : "translate-x-0.5"
                        }`} />
                      </button>
                    </div>
                  </div>

                  {installError && (
                    <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-[12px] text-red-400">
                      {installError}
                    </div>
                  )}

                  <Button
                    onClick={install}
                    disabled={installing}
                    className="w-full h-10 text-[13px] font-medium"
                  >
                    Install & Start
                  </Button>
                </div>
              )}

              {/* ==================== Install Progress ==================== */}
              {current === 6 && installing && (
                <div className="space-y-6">
                  <div>
                    <h1 className="text-xl font-semibold tracking-tight">Installing {preset.label}...</h1>
                    <p className="text-sm text-muted-foreground mt-1">Setting up your mail server. This will take a moment.</p>
                  </div>

                  <div className="space-y-1">
                    {installSteps.map((step, i) => (
                      <div key={i} className="flex items-center gap-3 px-3 py-2.5">
                        <div className="flex h-5 w-5 items-center justify-center">
                          {step.status === "pending" && (
                            <div className="h-2 w-2 rounded-full border border-muted-foreground/30" />
                          )}
                          {step.status === "running" && (
                            <Loader2 className="h-4 w-4 animate-spin text-primary" />
                          )}
                          {step.status === "done" && (
                            <motion.div
                              initial={{ scale: 0 }}
                              animate={{ scale: 1 }}
                              transition={{ type: "spring", stiffness: 300, damping: 20 }}
                            >
                              <CheckCircle2 className="h-4 w-4 text-emerald-500" />
                            </motion.div>
                          )}
                          {step.status === "error" && (
                            <AlertTriangle className="h-4 w-4 text-red-400" />
                          )}
                        </div>
                        <span className={`text-[13px] ${
                          step.status === "pending" ? "text-muted-foreground/50" :
                          step.status === "running" ? "text-foreground font-medium" :
                          step.status === "done" ? "text-foreground" :
                          "text-red-400"
                        }`}>
                          {step.label}
                        </span>
                      </div>
                    ))}
                  </div>

                  {installError && (
                    <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-[12px] text-red-400">
                      {installError}
                    </div>
                  )}
                </div>
              )}
            </motion.div>
          </AnimatePresence>
        </div>

        {/* Navigation bar */}
        {current !== 0 && !installing && !installResult && (
          <div className="border-t border-border px-8 py-4 flex items-center justify-between">
            <Button
              variant="ghost"
              onClick={goBack}
              disabled={current <= 1}
              className="text-[13px] text-muted-foreground h-9"
            >
              <ArrowLeft className="h-4 w-4 mr-1" /> Back
            </Button>
            {current !== 6 && (
              <Button
                onClick={goNext}
                disabled={!canNext()}
                className="h-9 text-[13px] font-medium px-5"
              >
                Next <ArrowRight className="h-4 w-4 ml-1" />
              </Button>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
