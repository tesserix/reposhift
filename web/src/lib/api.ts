const BASE = "/api/platform/v1";

function getStoredToken(): string | null {
  if (typeof window === "undefined") return null;
  return localStorage.getItem("reposhift_token");
}

function setStoredToken(token: string) {
  localStorage.setItem("reposhift_token", token);
}

function clearStoredToken() {
  localStorage.removeItem("reposhift_token");
  localStorage.removeItem("reposhift_auth_mode");
}

function getAuthMode(): "saas" | "selfhosted" | null {
  if (typeof window === "undefined") return null;
  return localStorage.getItem("reposhift_auth_mode") as
    | "saas"
    | "selfhosted"
    | null;
}

function setAuthMode(mode: "saas" | "selfhosted") {
  localStorage.setItem("reposhift_auth_mode", mode);
}

async function request<T>(
  path: string,
  options: RequestInit = {}
): Promise<T> {
  const token = getStoredToken();
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...(options.headers as Record<string, string>),
  };
  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  const res = await fetch(`${BASE}${path}`, { ...options, headers });

  if (res.status === 401) {
    clearStoredToken();
    window.location.href = "/login";
    throw new Error("Unauthorized");
  }

  if (!res.ok) {
    const body = await res.text();
    throw new Error(`API error ${res.status}: ${body}`);
  }

  if (res.status === 204) return {} as T;
  return res.json();
}

export interface PlatformMode {
  mode: "saas" | "selfhosted";
  githubOAuthEnabled: boolean;
}

export const api = {
  // Platform mode detection (public endpoint, no auth required)
  async getMode(): Promise<PlatformMode> {
    const res = await fetch(`${BASE}/config/mode`);
    if (!res.ok) {
      // Default to saas if the endpoint is unavailable
      return { mode: "saas", githubOAuthEnabled: true };
    }
    return res.json();
  },

  // Auth — GitHub OAuth
  login() {
    window.location.href = `${BASE}/auth/github`;
  },

  async getToken(code: string, state: string): Promise<{ token: string }> {
    const data = await request<{ token: string }>("/auth/github/callback", {
      method: "POST",
      body: JSON.stringify({ code, state }),
    });
    setStoredToken(data.token);
    setAuthMode("saas");
    return data;
  },

  // Auth — Admin token (self-hosted)
  loginWithAdminToken(token: string) {
    setStoredToken(token);
    setAuthMode("selfhosted");
  },

  // Auth — common
  isAuthenticated(): boolean {
    return !!getStoredToken();
  },

  getAuthMode,

  logout() {
    clearStoredToken();
    window.location.href = "/login";
  },

  // Tenant
  getTenant() {
    return request<Tenant>("/tenant");
  },

  getMembers() {
    return request<Member[]>("/tenant/members");
  },

  // Secrets
  listSecrets() {
    return request<{ secrets: Secret[] }>("/secrets");
  },

  getSecret(name: string) {
    return request<{ secret: Secret }>(`/secrets/${name}`);
  },

  createSecret(name: string, secretType: string, data: Record<string, string>) {
    return request<{ message: string; name: string }>("/secrets", {
      method: "POST",
      body: JSON.stringify({ name, secretType, data }),
    });
  },

  updateSecret(name: string, secretType: string, data: Record<string, string>) {
    return request<{ message: string; name: string }>(`/secrets/${name}`, {
      method: "PUT",
      body: JSON.stringify({ name, secretType, data }),
    });
  },

  deleteSecret(name: string) {
    return request<void>(`/secrets/${name}`, { method: "DELETE" });
  },

  validateSecret(name: string) {
    return request<{ validation: SecretValidation }>(`/secrets/${name}/validate`, {
      method: "POST",
    });
  },

  // Migrations
  listMigrations(page = 1, limit = 20) {
    return request<MigrationList>(
      `/migrations?page=${page}&limit=${limit}`
    );
  },

  createMigration(data: CreateMigrationRequest) {
    return request<Migration>("/migrations", {
      method: "POST",
      body: JSON.stringify(data),
    });
  },

  getMigration(id: string) {
    return request<Migration>(`/migrations/${id}`);
  },

  deleteMigration(id: string) {
    return request<void>(`/migrations/${id}`, { method: "DELETE" });
  },

  pauseMigration(id: string) {
    return request<Migration>(`/migrations/${id}/pause`, { method: "POST" });
  },

  resumeMigration(id: string) {
    return request<Migration>(`/migrations/${id}/resume`, { method: "POST" });
  },

  cancelMigration(id: string) {
    return request<Migration>(`/migrations/${id}/cancel`, { method: "POST" });
  },

  retryMigration(id: string) {
    return request<Migration>(`/migrations/${id}/retry`, { method: "POST" });
  },

  // Dashboard
  getDashboardStats() {
    return request<DashboardStats>("/dashboard/stats");
  },
};

// Types
export interface Tenant {
  id: string;
  name: string;
  slug: string;
  plan: string;
  created_at: string;
}

export interface Member {
  id: string;
  username: string;
  email: string;
  role: string;
  avatar_url: string;
  joined_at: string;
}

export interface Secret {
  id: string;
  name: string;
  secretType: string;
  metadata?: Record<string, string>;
  created_at: string;
  updated_at: string;
}

export interface SecretValidation {
  name: string;
  secretType: string;
  valid: boolean;
  checks: SecretValidationCheck[];
}

export interface SecretValidationCheck {
  check: string;
  status: "passed" | "failed" | "warning" | "skipped";
  message: string;
}

export interface Migration {
  id: string;
  display_name: string;
  status: string;
  phase: string;
  progress: number;
  source_org: string;
  source_project: string;
  source_repos: string[];
  target_owner: string;
  ado_secret_id: string;
  github_secret_id: string;
  resources: MigrationResource[];
  created_at: string;
  updated_at: string;
  error?: string;
}

export interface MigrationResource {
  name: string;
  type: string;
  status: string;
  progress: number;
  error?: string;
}

export interface MigrationList {
  items: Migration[];
  total: number;
  page: number;
  limit: number;
}

export interface CreateMigrationRequest {
  displayName: string;
  sourceOrg: string;
  sourceProject: string;
  sourceRepos: string[];
  targetOwner: string;
  adoSecretName: string;
  githubSecretName: string;
  branchFilterMode?: "include" | "exclude" | "";
  branches?: string[];
}

export interface DashboardStats {
  total_migrations: number;
  completed: number;
  failed: number;
  in_progress: number;
  total_repos_migrated: number;
}
