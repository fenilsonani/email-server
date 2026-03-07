const API_BASE = process.env.NEXT_PUBLIC_API_URL || "/admin/api";

interface APIResponse<T = unknown> {
  success: boolean;
  data?: T;
  error?: string;
  meta?: {
    page: number;
    page_size: number;
    total_count: number;
    total_pages: number;
  };
}

function buildURL(path: string, params?: Record<string, string>): string {
  let url = `${API_BASE}${path}`;
  if (params) {
    const sp = new URLSearchParams(params);
    url += `?${sp.toString()}`;
  }
  return url;
}

async function parseJSON<T>(res: Response): Promise<APIResponse<T>> {
  const ct = res.headers.get("content-type") || "";
  if (!ct.includes("application/json")) {
    return { success: false, error: `Unexpected response (${res.status})` };
  }
  const text = await res.text();
  try {
    return JSON.parse(text);
  } catch {
    return { success: false, error: `Invalid JSON response (${res.status})` };
  }
}

class AdminAPI {
  private csrfToken: string | null = null;

  private async getCSRFToken(): Promise<string> {
    if (this.csrfToken) return this.csrfToken;
    try {
      const res = await fetch(`${API_BASE}/auth/csrf`, { credentials: "include" });
      const data = await parseJSON<{ token: string }>(res);
      if (data.success && data.data?.token) {
        this.csrfToken = data.data.token;
      }
    } catch {
      // CSRF fetch failed — continue without token
    }
    return this.csrfToken || "";
  }

  async get<T>(path: string, params?: Record<string, string>): Promise<APIResponse<T>> {
    try {
      const url = buildURL(path, params);
      const res = await fetch(url, { credentials: "include" });
      if (res.status === 401) {
        return { success: false, error: "Unauthorized" };
      }
      return await parseJSON<T>(res);
    } catch {
      return { success: false, error: "Network error" };
    }
  }

  async post<T>(path: string, body?: unknown): Promise<APIResponse<T>> {
    try {
      const csrf = await this.getCSRFToken();
      const res = await fetch(`${API_BASE}${path}`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          ...(csrf ? { "X-CSRF-Token": csrf } : {}),
        },
        credentials: "include",
        body: body ? JSON.stringify(body) : undefined,
      });
      if (res.status === 401) {
        return { success: false, error: "Unauthorized" };
      }
      return await parseJSON<T>(res);
    } catch {
      return { success: false, error: "Network error" };
    }
  }

  async put<T>(path: string, body?: unknown): Promise<APIResponse<T>> {
    try {
      const csrf = await this.getCSRFToken();
      const res = await fetch(`${API_BASE}${path}`, {
        method: "PUT",
        headers: {
          "Content-Type": "application/json",
          ...(csrf ? { "X-CSRF-Token": csrf } : {}),
        },
        credentials: "include",
        body: body ? JSON.stringify(body) : undefined,
      });
      if (res.status === 401) {
        return { success: false, error: "Unauthorized" };
      }
      return await parseJSON<T>(res);
    } catch {
      return { success: false, error: "Network error" };
    }
  }

  async delete<T>(path: string): Promise<APIResponse<T>> {
    try {
      const csrf = await this.getCSRFToken();
      const res = await fetch(`${API_BASE}${path}`, {
        method: "DELETE",
        headers: { ...(csrf ? { "X-CSRF-Token": csrf } : {}) },
        credentials: "include",
      });
      if (res.status === 401) {
        return { success: false, error: "Unauthorized" };
      }
      return await parseJSON<T>(res);
    } catch {
      return { success: false, error: "Network error" };
    }
  }

  clearCSRF() {
    this.csrfToken = null;
  }
}

export const api = new AdminAPI();
