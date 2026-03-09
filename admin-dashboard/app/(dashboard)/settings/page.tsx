"use client";

import { useEffect, useState } from "react";
import { PageShell } from "@/components/shared/page-shell";
import { api } from "@/lib/api";
import { usePresetStore } from "@/lib/preset";
import { Building2, Users, Trash2, Plus, Loader2, Crown, Shield, User } from "lucide-react";
import { toast } from "sonner";

interface Org {
  id: number;
  name: string;
  slug: string;
  preset: string;
  owner_user_id: number;
  created_at: string;
}

interface Member {
  id: number;
  org_id: number;
  user_id: number;
  role: string;
  username: string;
  email: string;
  created_at: string;
}

interface Preset {
  label: string;
  description: string;
  enabled_features: string[];
}

const roleIcons: Record<string, React.ComponentType<{ className?: string }>> = {
  owner: Crown,
  admin: Shield,
  member: User,
};

function OrgSettingsContent() {
  const [org, setOrg] = useState<Org | null>(null);
  const [members, setMembers] = useState<Member[]>([]);
  const [presets, setPresets] = useState<Record<string, Preset>>({});
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [orgName, setOrgName] = useState("");
  const { currentOrg, load: reloadPresets } = usePresetStore();

  const loadOrg = async () => {
    if (!currentOrg) return;
    const [orgRes, membersRes, presetsRes] = await Promise.all([
      api.get<Org>(`/v1/orgs/${currentOrg.id}`),
      api.get<Member[]>(`/v1/orgs/${currentOrg.id}/members`),
      api.get<Record<string, Preset>>("/v1/presets"),
    ]);
    if (orgRes.success && orgRes.data) {
      setOrg(orgRes.data);
      setOrgName(orgRes.data.name);
    }
    if (membersRes.success && membersRes.data) setMembers(membersRes.data);
    if (presetsRes.success && presetsRes.data) setPresets(presetsRes.data);
    setLoading(false);
  };

  useEffect(() => {
    if (currentOrg) loadOrg();
  }, [currentOrg]);

  const updateOrg = async (updates: { name?: string; preset?: string }) => {
    if (!org) return;
    setSaving(true);
    const res = await api.put(`/v1/orgs/${org.id}`, updates);
    if (res.success) {
      toast.success("Organization updated");
      loadOrg();
      reloadPresets();
    } else {
      toast.error(res.error || "Failed to update");
    }
    setSaving(false);
  };

  const removeMember = async (userId: number) => {
    if (!org || !confirm("Remove this member?")) return;
    const res = await api.delete(`/v1/orgs/${org.id}/members/${userId}`);
    if (res.success) {
      toast.success("Member removed");
      loadOrg();
    } else {
      toast.error(res.error || "Failed to remove member");
    }
  };

  if (loading) {
    return <PageShell title="Organization"><div className="flex justify-center py-12"><Loader2 className="h-5 w-5 animate-spin text-muted-foreground" /></div></PageShell>;
  }

  if (!org) {
    return <PageShell title="Organization"><p className="text-muted-foreground text-[13px]">No organization found</p></PageShell>;
  }

  return (
    <PageShell title="Organization Settings" description={`Manage ${org.name}`}>
      {/* General */}
      <div className="rounded-lg border border-border bg-card p-4 space-y-4">
        <h3 className="text-[13px] font-semibold">General</h3>
        <div className="grid gap-4 sm:grid-cols-2">
          <div>
            <label className="text-[11px] font-medium text-muted-foreground uppercase">Name</label>
            <div className="flex gap-2 mt-1">
              <input
                value={orgName}
                onChange={(e) => setOrgName(e.target.value)}
                className="flex-1 rounded-md border border-border bg-background px-3 py-1.5 text-sm"
              />
              <button
                onClick={() => updateOrg({ name: orgName })}
                disabled={orgName === org.name || saving}
                className="rounded-md bg-primary px-3 py-1.5 text-[13px] font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
              >
                Save
              </button>
            </div>
          </div>
          <div>
            <label className="text-[11px] font-medium text-muted-foreground uppercase">Slug</label>
            <p className="mt-1 text-sm font-mono text-muted-foreground">{org.slug}</p>
          </div>
        </div>
      </div>

      {/* Platform Mode */}
      <div className="rounded-lg border border-border bg-card p-4 space-y-4">
        <h3 className="text-[13px] font-semibold">Platform Mode</h3>
        <p className="text-[12px] text-muted-foreground">Controls which features are visible in the dashboard.</p>
        <div className="grid gap-3 sm:grid-cols-3">
          {Object.entries(presets).map(([key, preset]) => (
            <button
              key={key}
              onClick={() => updateOrg({ preset: key })}
              className={`text-left rounded-lg border p-3 transition-colors ${
                org.preset === key ? "border-primary bg-primary/5" : "border-border hover:border-border/80"
              }`}
            >
              <p className="text-[13px] font-medium">{preset.label}</p>
              <p className="text-[11px] text-muted-foreground mt-0.5">{preset.description}</p>
            </button>
          ))}
        </div>
      </div>

      {/* Members */}
      <div className="rounded-lg border border-border bg-card p-4 space-y-4">
        <div className="flex items-center justify-between">
          <h3 className="text-[13px] font-semibold">Members</h3>
        </div>
        <div className="divide-y divide-border rounded-md border border-border">
          {members.map((m) => {
            const RoleIcon = roleIcons[m.role] || User;
            return (
              <div key={m.user_id} className="flex items-center justify-between px-4 py-2.5">
                <div className="flex items-center gap-3">
                  <div className="flex h-7 w-7 items-center justify-center rounded-full bg-muted text-[11px] font-medium">
                    {m.username.charAt(0).toUpperCase()}
                  </div>
                  <div>
                    <p className="text-[13px] font-medium">{m.email}</p>
                    <div className="flex items-center gap-1 text-[11px] text-muted-foreground">
                      <RoleIcon className="h-3 w-3" />
                      <span className="capitalize">{m.role}</span>
                    </div>
                  </div>
                </div>
                {m.role !== "owner" && (
                  <button
                    onClick={() => removeMember(m.user_id)}
                    className="rounded p-1.5 hover:bg-accent text-muted-foreground hover:text-red-400"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </button>
                )}
              </div>
            );
          })}
        </div>
      </div>
    </PageShell>
  );
}

export default function SettingsPage() {
  return <OrgSettingsContent />;
}
