import axios from "axios";

const BASE_URL = import.meta.env.VITE_API_URL ?? "http://localhost:8081";

export const apiClient = axios.create({ baseURL: BASE_URL });

apiClient.interceptors.request.use((config) => {
  const token = localStorage.getItem("totra_token");
  if (token) config.headers.Authorization = `Bearer ${token}`;
  return config;
});

apiClient.interceptors.response.use(
  (r) => r,
  (err) => {
    if (err.response?.status === 401) {
      localStorage.removeItem("totra_token");
      window.location.href = "/login";
    }
    return Promise.reject(err);
  }
);

export const login = (email: string, password: string) =>
  apiClient.post<{ token: string }>("/api/auth/login", { email, password });

export const listUsers = () =>
  apiClient.get<{ total: number; users: User[] }>("/api/users");

export const createUser = (data: { name: string; email: string; role: string }) =>
  apiClient.post<{ id: string; name: string; email: string; role: string; api_key: string }>("/api/users", data);

export const listModels = () =>
  apiClient.get<{ total: number; models: ModelConfig[] }>("/api/models");

export const createModel = (data: CreateModelPayload) =>
  apiClient.post<ModelConfig>("/api/models", data);

export const getMonthlySummary = (month: string) =>
  apiClient.get<{ month: string; summaries: UserSummary[] }>(`/api/usage/summary?month=${month}`);

export const getAdoptionRate = (month: string) =>
  apiClient.get<AdoptionStats>(`/api/usage/adoption?month=${month}`);

export const listPendingRequests = () =>
  apiClient.get<{ requests: QuotaRequest[] }>("/api/quota/requests");

export const approveQuota = (id: string) =>
  apiClient.post(`/api/quota/requests/${id}/approve`);

export const rejectQuota = (id: string) =>
  apiClient.post(`/api/quota/requests/${id}/reject`);

export interface User {
  id: string;
  name: string;
  email: string;
  role: string;
  quota_scu: number;
  is_active: boolean;
}
export interface ModelConfig {
  id: string;
  name: string;
  provider: string;
  base_url: string;
  scu_rate: number;
  is_active: boolean;
}
export interface CreateModelPayload {
  name: string;
  provider: string;
  base_url: string;
  api_key: string;
  scu_rate: number;
}
export interface UserSummary {
  user_id: string;
  user_name: string;
  total_scu: number;
  total_usd: number;
  request_count: number;
}
export interface AdoptionStats {
  total_users: number;
  active_users: number;
  adoption_rate: number;
}
export interface QuotaRequest {
  id: string;
  user_id: string;
  new_quota: number;
  reason: string;
  status: string;
}

// ---- Phase 2 types ----
export interface EfficiencySnapshot {
  id: string;
  user_id: string;
  user_name: string;
  year_month: string;
  total_scu: number;
  total_output_weight: number;
  efficiency_score: number;
  aiq_score: number;
  oss_score: number;
  gts_score: number;
  integration_level: number;
  anomaly_flagged: boolean;
  peer_group: string;
  rank: number;
  peer_count: number;
  snapshot_at: string;
}

export interface WebhookConfig {
  id: string;
  platform: string;
  event_weights: Record<string, number>;
  is_active: boolean;
}

export interface UserIntegration {
  id: string;
  platform: string;
  external_id: string;
  created_by: string;
}

export interface FuelTransaction {
  id: string;
  amount_scu: number;
  reason: string;
  tier: string;
  created_at: string;
}

export interface FuelSettings {
  top_10pct_bonus: number;
  top_25pct_bonus: number;
  top_50pct_bonus: number;
}

// ---- Phase 2 API functions ----
export const getKPISnapshots = (month: string) =>
  apiClient.get<{ month: string; snapshots: EfficiencySnapshot[] }>(
    `/api/kpi/snapshots?month=${month}`
  );

export const triggerKPISnapshot = (month: string) =>
  apiClient.post(`/api/admin/kpi/run?month=${month}`);

export const getKPIAnomalies = (month: string) =>
  apiClient.get<{ month: string; anomalies: EfficiencySnapshot[] }>(
    `/api/admin/kpi/anomalies?month=${month}`
  );

export const getMyKPI = () =>
  apiClient.get<{ snapshots: EfficiencySnapshot[] }>("/api/me/kpi");

export const getWebhookConfigs = () =>
  apiClient.get<{ configs: WebhookConfig[] }>("/api/integrations");

export const createWebhookConfig = (data: {
  platform: string;
  webhook_secret: string;
  event_weights?: Record<string, number>;
}) => apiClient.post<WebhookConfig>("/api/integrations", data);

export const getMyIntegrations = () =>
  apiClient.get<{ integrations: UserIntegration[] }>("/api/me/integrations");

export const bindIntegration = (platform: string, external_id: string) =>
  apiClient.post("/api/me/integrations", { platform, external_id });

export const getFuelSettings = () =>
  apiClient.get<FuelSettings>("/api/fuel/settings");

export const updateFuelSettings = (data: FuelSettings) =>
  apiClient.put("/api/fuel/settings", data);

export const getMyFuel = () =>
  apiClient.get<{ transactions: FuelTransaction[] }>("/api/me/fuel");

export const getKPIUserHistory = (user_id: string) =>
  apiClient.get<{ snapshots: EfficiencySnapshot[] }>(
    `/api/kpi/user-history?user_id=${user_id}`
  );

export const getMyQuota = () =>
  apiClient.get<{ quota_scu: number; used_scu: number }>("/api/me/quota");

export function getMyUID(): string {
  const token = localStorage.getItem("totra_token");
  if (!token) return "";
  try { return JSON.parse(atob(token.split(".")[1])).uid ?? ""; } catch { return ""; }
}

export interface UserProfile {
  job_role: string;
  department: string;
}

export const getMyProfile = () =>
  apiClient.get<UserProfile>("/api/me/profile");

export const updateMyProfile = (data: UserProfile) =>
  apiClient.patch<{ status: string }>("/api/me/profile", data);
