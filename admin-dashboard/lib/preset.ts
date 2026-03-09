import { create } from "zustand";
import { api } from "./api";

interface Preset {
  label: string;
  description: string;
  enabled_features: string[];
}

interface Org {
  id: number;
  name: string;
  slug: string;
  preset: string;
  owner_user_id: number;
  created_at: string;
}

interface PresetStore {
  currentOrg: Org | null;
  orgs: Org[];
  presets: Record<string, Preset>;
  loaded: boolean;
  load: () => Promise<void>;
  switchOrg: (orgId: number) => void;
  hasFeature: (feature: string) => boolean;
}

export const usePresetStore = create<PresetStore>((set, get) => ({
  currentOrg: null,
  orgs: [],
  presets: {},
  loaded: false,

  load: async () => {
    const [orgsRes, presetsRes] = await Promise.all([
      api.get<Org[]>("/v1/orgs"),
      api.get<Record<string, Preset>>("/v1/presets"),
    ]);

    const orgs = orgsRes.success && Array.isArray(orgsRes.data) ? orgsRes.data : [];
    const presets = presetsRes.success && presetsRes.data ? presetsRes.data : {};

    set({
      orgs,
      presets,
      currentOrg: orgs[0] || null,
      loaded: true,
    });
  },

  switchOrg: (orgId: number) => {
    const org = get().orgs.find((o) => o.id === orgId);
    if (org) set({ currentOrg: org });
  },

  hasFeature: (feature: string) => {
    const { currentOrg, presets } = get();
    if (!currentOrg) return true;
    const preset = presets[currentOrg.preset];
    if (!preset) return true;
    return preset.enabled_features.includes("*") || preset.enabled_features.includes(feature);
  },
}));
