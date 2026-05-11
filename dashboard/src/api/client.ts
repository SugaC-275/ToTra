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
export interface DeptSummary {
  department: string;
  user_count: number;
  active_users: number;
  total_scu: number;
  total_usd: number;
  request_count: number;
}

export interface BudgetForecast {
  month: string;
  current_scu: number;
  current_usd: number;
  days_elapsed: number;
  days_in_month: number;
  projected_scu: number;
  projected_usd: number;
  prior_month_scu: number;
  trend_pct: number;
}

export interface InactiveUser {
  user_id: string;
  name: string;
  email: string;
  department: string;
  job_role: string;
  active_days: number;
  last_active_at: string | null;
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

export interface AIQSubmetrics {
  output_density: number;
  usage_consistency: number;
  task_depth: number;
  cost_efficiency: number;
  active_days: number;
  working_days: number;
}

export const getMyKPISubmetrics = (month: string) =>
  apiClient.get<{ month: string; metrics: AIQSubmetrics | null }>(
    `/api/me/kpi/submetrics?month=${month}`
  );

export const getMyKPIInsights = (month: string) =>
  apiClient.get<{ insight: string; month: string }>(
    `/api/me/kpi/insights?month=${month}`
  );

export const getMyProfile = () =>
  apiClient.get<UserProfile>("/api/me/profile");

export const updateMyProfile = (data: UserProfile) =>
  apiClient.patch<{ status: string }>("/api/me/profile", data);

// ---- Ops Reports ----

export const getDepartmentSummary = (month: string) =>
  apiClient.get<{ month: string; departments: DeptSummary[] }>(
    `/api/admin/usage/department?month=${month}`
  );

export const exportDepartmentCSV = async (month: string): Promise<void> => {
  const token = localStorage.getItem("totra_token");
  const res = await fetch(
    `${BASE_URL}/api/admin/usage/department/export?month=${month}`,
    { headers: { Authorization: `Bearer ${token ?? ""}` } }
  );
  if (!res.ok) throw new Error("Export failed");
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `cost-chargeback-${month}.csv`;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
};

export const getBudgetForecast = (month: string) =>
  apiClient.get<BudgetForecast>(`/api/admin/usage/forecast?month=${month}`);

export const getInactiveUsers = (month: string, maxDays = 3) =>
  apiClient.get<{ month: string; max_days: number; users: InactiveUser[] }>(
    `/api/admin/usage/inactive?month=${month}&max_days=${maxDays}`
  );

// ---- IP Allowlist ----

export interface IPAllowlistEntry {
  id: string;
  cidr: string;
  label: string;
  created_at: string;
}

export const listIPAllowlist = () =>
  apiClient.get<{ entries: IPAllowlistEntry[] }>("/api/admin/ip-allowlist");

export const addIPAllowlistEntry = (cidr: string, label: string) =>
  apiClient.post<IPAllowlistEntry>("/api/admin/ip-allowlist", { cidr, label });

export const deleteIPAllowlistEntry = (id: string) =>
  apiClient.delete<{ status: string }>(`/api/admin/ip-allowlist/${id}`);

// ---- Bot Notifications ----

export interface BotConfig {
  id: string;
  tenant_id: string;
  platform: string;
  label: string;
  enabled: boolean;
  created_at: string;
}

export const listBotConfigs = async (): Promise<{ configs: BotConfig[] }> => {
  const { data } = await apiClient.get("/api/admin/bot-configs");
  return data;
};

export const addBotConfig = async (payload: {
  platform: string;
  webhook_url: string;
  label: string;
}): Promise<BotConfig> => {
  const { data } = await apiClient.post("/api/admin/bot-configs", payload);
  return data;
};

export const deleteBotConfig = async (id: string): Promise<void> => {
  await apiClient.delete(`/api/admin/bot-configs/${id}`);
};

export const sendKPISummary = async (month: string): Promise<void> => {
  await apiClient.post(`/api/admin/bot-configs/send-summary?month=${month}`);
};

export const sendTestBotMessage = async (id: string): Promise<void> => {
  await apiClient.post(`/api/admin/bot-configs/${id}/test`);
};

// ---- HR Sync ----

export interface SyncResult {
  created: number;
  updated: number;
  skipped: number;
  errors: number;
}

export const syncHRCSV = async (file: File): Promise<SyncResult> => {
  const formData = new FormData();
  formData.append("file", file);
  const { data } = await apiClient.post("/api/admin/hr/sync", formData, {
    headers: { "Content-Type": "multipart/form-data" },
  });
  return data;
};
