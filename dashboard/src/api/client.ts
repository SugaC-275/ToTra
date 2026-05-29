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

export const updateModelPricing = async (
  id: string,
  pricing: { price_per_m_input: number; price_per_m_output: number }
) => {
  const { data } = await apiClient.put(`/api/admin/models/${id}/pricing`, pricing);
  return data;
};

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
  price_per_m_input?: number | null;
  price_per_m_output?: number | null;
  // Compliance tags (migration 064)
  hipaa_eligible?: boolean;
  govcloud?: boolean;
  fedramp_auth?: boolean;
  data_region?: string;
  compliance_notes?: string;
}
export interface CreateModelPayload {
  name: string;
  provider: string;
  base_url: string;
  api_key: string;
  scu_rate: number;
}

export interface ModelComplianceTagsPayload {
  hipaa_eligible?: boolean;
  govcloud?: boolean;
  fedramp_auth?: boolean;
  data_region?: string;
  compliance_notes?: string;
}

export const updateModelComplianceTags = async (
  id: string,
  tags: ModelComplianceTagsPayload
): Promise<ModelConfig> => {
  const { data } = await apiClient.patch<ModelConfig>(`/api/admin/models/${id}/compliance`, tags);
  return data;
};
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


export interface WebhookConfig {
  id: string;
  platform: string;
  event_weights: Record<string, number>;
  is_active: boolean;
}

// ---- Outbound Webhooks (ToTra notification system) ----

export interface OutboundWebhookConfig {
  id: string;
  name: string;
  url: string;
  events: string[];
  is_active: boolean;
  created_at: string;
}

export const listWebhooks = async (): Promise<OutboundWebhookConfig[]> => {
  const { data } = await apiClient.get<{ object: string; data: OutboundWebhookConfig[] }>("/v1/webhooks");
  return data.data;
};

export const createWebhook = async (payload: {
  name: string;
  url: string;
  secret: string;
  events: string[];
}): Promise<OutboundWebhookConfig> => {
  const { data } = await apiClient.post<OutboundWebhookConfig>("/v1/webhooks", payload);
  return data;
};

export const deleteWebhook = async (id: string): Promise<void> => {
  await apiClient.delete(`/v1/webhooks/${id}`);
};

export const testWebhook = async (id: string): Promise<void> => {
  await apiClient.post(`/v1/webhooks/${id}/test`);
};

export interface UserIntegration {
  id: string;
  platform: string;
  external_id: string;
  created_by: string;
}


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
  const { data } = await apiClient.post("/api/admin/hr/sync", formData);
  return data;
};

export interface AgentSession {
  id: string;
  tenant_id: string;
  user_id: string;
  user_name: string;
  conversation_id: string;
  loop_count: number;
  tool_call_count: number;
  is_dead_loop: boolean;
  last_seen_at: string;
  created_at: string;
}

export const getAdminAgentSessions = (month: string) =>
  apiClient.get<{ month: string; sessions: AgentSession[] }>(
    `/api/admin/agent-sessions?month=${month}`
  );

export const getMyAgentSessions = (month: string) =>
  apiClient.get<{ month: string; sessions: AgentSession[] }>(
    `/api/me/agent-sessions?month=${month}`
  );

export interface AuditEntry {
  id: number;
  tenant_id: string;
  record_type: string;
  record_id: string;
  record_hash: string;
  prev_hash: string;
  chain_hash: string;
  created_at: string;
}

export interface VerifyResult {
  valid: boolean;
  first_bad_id?: number;
}

export const getAuditLog = async (limit = 50): Promise<AuditEntry[]> => {
  const { data } = await apiClient.get(`/api/admin/audit-log?limit=${limit}`);
  return data;
};

export const verifyAuditChain = async (): Promise<VerifyResult> => {
  const { data } = await apiClient.get("/api/admin/audit-log/verify");
  return data;
};

// ---- GDPR & Compliance ----

export interface DeletionRequest {
  id: string;
  tenant_id: string;
  user_id: string;
  user_name?: string;
  user_email?: string;
  status: string;
  requested_at: string;
  processed_at?: string | null;
}

export interface ExportUsageRecord {
  request_at: string;
  model_name: string;
  prompt_tokens: number;
  completion_tokens: number;
  scu_cost: number;
  usd_cost: number;
  response_ms: number;
}

export interface DataExport {
  exported_at: string;
  user_id: string;
  usage_records: ExportUsageRecord[];
}

export const getDataRetention = () =>
  apiClient.get<{ data_retention_months: number }>("/api/admin/data-retention");

export const setDataRetention = (months: number) =>
  apiClient.put<{ data_retention_months: number }>("/api/admin/data-retention", {
    data_retention_months: months,
  });

export const runRetentionCleanup = () =>
  apiClient.post<{ deleted_count: number }>("/api/admin/data-retention/run");

export const listDeletionRequests = () =>
  apiClient.get<{ requests: DeletionRequest[] }>("/api/admin/data-deletion-requests");

export const approveDeletionRequest = (id: string) =>
  apiClient.post<{ status: string }>(`/api/admin/data-deletion-requests/${id}/approve`);

export const rejectDeletionRequest = (id: string) =>
  apiClient.post<{ status: string }>(`/api/admin/data-deletion-requests/${id}/reject`);

export const exportMyData = () =>
  apiClient.get<DataExport>("/api/me/data-export");

export const createDeletionRequest = () =>
  apiClient.post<DeletionRequest>("/api/me/data-deletion-request");

export const downloadMyDataExport = async (): Promise<void> => {
  const { data } = await exportMyData();
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: "application/json" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `totra-data-export-${new Date().toISOString().slice(0, 10)}.json`;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
};

// ---- SIEM ----

export interface SIEMConfig {
  id: string;
  tenant_id: string;
  name: string;
  endpoint_url: string;
  event_types: string[];
  is_active: boolean;
  created_at: string;
}

export interface DeliveryLogRow {
  id: number;
  event_type: string;
  status: string;
  attempts: number;
  created_at: string;
}

export const listSIEMConfigs = async (): Promise<{ configs: SIEMConfig[] }> => {
  const { data } = await apiClient.get("/api/admin/siem/configs");
  return data;
};

export const createSIEMConfig = async (payload: {
  name: string;
  endpoint_url: string;
  api_key: string;
  event_types: string[];
}): Promise<SIEMConfig> => {
  const { data } = await apiClient.post("/api/admin/siem/configs", payload);
  return data;
};

export const deleteSIEMConfig = async (id: string): Promise<void> => {
  await apiClient.delete(`/api/admin/siem/configs/${id}`);
};

export const sendSIEMTest = async (id: string): Promise<void> => {
  await apiClient.post(`/api/admin/siem/configs/${id}/test`);
};

export const getSIEMDeliveryLog = async (): Promise<{ log: DeliveryLogRow[] }> => {
  const { data } = await apiClient.get("/api/admin/siem/delivery-log?limit=50");
  return data;
};

// ---- Alert Delivery Configs ----

export interface AlertDeliveryConfig {
  id: string;
  tenant_id: string;
  channel: "slack" | "email" | "webhook";
  destination: string;
  event_types: string[];
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export const listAlertConfigs = async (): Promise<{ configs: AlertDeliveryConfig[] }> => {
  const { data } = await apiClient.get("/api/admin/alert-configs/");
  return data;
};

export const createAlertConfig = async (payload: {
  channel: string;
  destination: string;
  event_types: string[];
  enabled?: boolean;
}): Promise<void> => {
  await apiClient.post("/api/admin/alert-configs/", payload);
};

export const deleteAlertConfig = async (id: string): Promise<void> => {
  await apiClient.delete(`/api/admin/alert-configs/${id}`);
};

export const sendTestAlertConfig = async (): Promise<void> => {
  await apiClient.post("/api/admin/alert-configs/test");
};

// ---- MCP Servers ----

export interface MCPServer {
  id: string;
  name: string;
  description: string;
  url: string;
  auth_type: "none" | "bearer";
  enabled: boolean;
  max_tool_calls: number;
  created_at: string;
  updated_at: string;
}

export interface CreateMCPServerPayload {
  name: string;
  description?: string;
  url: string;
  auth_type: "none" | "bearer";
  auth_token?: string;
  max_tool_calls?: number;
}

export interface UpdateMCPServerPayload {
  description?: string;
  url?: string;
  auth_type?: "none" | "bearer";
  auth_token?: string;
  enabled?: boolean;
  max_tool_calls?: number;
}

export interface MCPToolCallLog {
  id: number;
  server_name: string;
  tool_name: string;
  status_code: number;
  duration_ms: number;
  pii_detected: boolean;
  created_at: string;
}

export const listMCPServers = () =>
  apiClient.get<{ total: number; servers: MCPServer[] }>("/api/mcp-servers");

export const getMCPServer = (id: string) =>
  apiClient.get<MCPServer>(`/api/mcp-servers/${id}`);

export const createMCPServer = (data: CreateMCPServerPayload) =>
  apiClient.post<MCPServer>("/api/mcp-servers", data);

export const updateMCPServer = (id: string, data: UpdateMCPServerPayload) =>
  apiClient.put<MCPServer>(`/api/mcp-servers/${id}`, data);

export const deleteMCPServer = (id: string) =>
  apiClient.delete(`/api/mcp-servers/${id}`);

export const listMCPToolCalls = (limit = 50, offset = 0) =>
  apiClient.get<{ total: number; tool_calls: MCPToolCallLog[] }>(
    `/api/mcp-servers/tool-calls?limit=${limit}&offset=${offset}`
  );

// ---- Prompts ----

export interface PromptTemplate {
  id: string;
  name: string;
  version: number;
  content: string;
  variables: string[];
  model: string;
  tags: string[];
  is_active: boolean;
  created_at: string;
}

export const listPrompts = (limit = 100) =>
  apiClient.get<{ object: string; data: PromptTemplate[] }>(`/v1/prompts?limit=${limit}`);

export const savePrompt = (name: string, content: string) =>
  apiClient.post<PromptTemplate>("/v1/prompts", { name, content });

export const getPrompt = (name: string) =>
  apiClient.get<PromptTemplate>(`/v1/prompts/${encodeURIComponent(name)}`);

export const getPromptVersion = (name: string, version: number) =>
  apiClient.get<PromptTemplate>(`/v1/prompts/${encodeURIComponent(name)}/versions/${version}`);

export const renderPrompt = (name: string, variables: Record<string, string>) =>
  apiClient.post<{ prompt: string; version: number; rendered: string }>(
    `/v1/prompts/${encodeURIComponent(name)}/render`,
    { variables }
  );

// ---- Request Logs ----

export interface RequestLogItem {
  id: string;
  user_id: string;
  model: string;
  provider: string;
  status_code: number;
  latency_ms: number;
  prompt_tokens: number;
  completion_tokens: number;
  cost_usd: number;
  created_at: string;
  request_preview: string;
  response_preview: string;
}

export interface RequestLogDetail extends RequestLogItem {
  request_body: unknown;
  response_body: unknown;
}

export interface RequestLogsResponse {
  data: RequestLogItem[];
  total: number;
  limit: number;
  offset: number;
}

export interface RequestLogFilter {
  user_id?: string;
  model?: string;
  provider?: string;
  status?: string;
  search?: string;
  limit?: number;
  offset?: number;
}

export const listRequestLogs = async (f: RequestLogFilter = {}): Promise<RequestLogsResponse> => {
  const params = new URLSearchParams();
  if (f.user_id) params.set("user_id", f.user_id);
  if (f.model) params.set("model", f.model);
  if (f.provider) params.set("provider", f.provider);
  if (f.status) params.set("status", f.status);
  if (f.search) params.set("search", f.search);
  params.set("limit", String(f.limit ?? 50));
  params.set("offset", String(f.offset ?? 0));
  const { data } = await apiClient.get<RequestLogsResponse>(`/v1/logs?${params.toString()}`);
  return data;
};

export const getRequestLog = async (id: string): Promise<RequestLogDetail> => {
  const { data } = await apiClient.get<RequestLogDetail>(`/v1/logs/${id}`);
  return data;
};

// ---- Cost Estimation ----

export interface CostEstimate {
  model: string;
  estimated_prompt_tokens: number;
  max_completion_tokens: number;
  estimated_cost_usd: number;
  cost_per_million_input: number;
  cost_per_million_output: number;
  note: string;
}

export async function estimateCost(
  model: string,
  messages: Array<{ role: string; content: string }>,
  maxTokens?: number,
): Promise<CostEstimate> {
  const { data } = await apiClient.post<CostEstimate>("/v1/estimate", {
    model,
    messages,
    ...(maxTokens != null ? { max_tokens: maxTokens } : {}),
  });
  return data;
}

// ---- Failover / Fallback chain ----

export interface FailoverChain {
  model: string;
  chain: string[];
}

export const getFailoverChain = async (model: string): Promise<FailoverChain> => {
  const { data } = await apiClient.get<FailoverChain>(`/v1/failover/${encodeURIComponent(model)}`);
  return data;
};

export const setFailoverChain = async (model: string, chain: string[]): Promise<void> => {
  await apiClient.put(`/v1/failover/${encodeURIComponent(model)}`, { fallback_models: chain });
};

export const clearFailoverChain = async (model: string): Promise<void> => {
  await apiClient.delete(`/v1/failover/${encodeURIComponent(model)}`);
};

// ---- Cache Stats & Config ----

export interface CacheStats {
  tenant_id: string;
  period: string;
  exact_hits: number;
  semantic_hits: number;
  total_hits: number;
  estimated_savings_usd: number;
  avg_latency_saved_ms: number;
  top_saved_models: { model: string; hits: number; savings_usd: number }[];
}

export interface CacheConfig {
  tenant_id: string;
  exact_ttl_seconds: number;
  semantic_ttl_seconds: number;
  semantic_enabled: boolean;
}

export async function getCacheStats(period: string): Promise<CacheStats> {
  const { data } = await apiClient.get<CacheStats>(`/v1/cache/stats?period=${period}`);
  return data;
}

export async function clearTenantCache(): Promise<void> {
  await apiClient.delete("/v1/cache/clear");
}

export async function getCacheConfig(): Promise<CacheConfig> {
  const { data } = await apiClient.get<CacheConfig>("/v1/cache/config");
  return data;
}

export async function setCacheConfig(config: Omit<CacheConfig, "tenant_id">): Promise<void> {
  await apiClient.put("/v1/cache/config", config);
}

// ---- Compliance Report Export ----

export interface ComplianceReportChecklistItem {
  category: string;
  item: string;
  status: 'enabled' | 'disabled' | 'partial';
  detail: string;
}

export interface ComplianceReportData {
  generated_at: string;
  tenant_id: string;
  period_start: string;
  period_end: string;
  total_requests: number;
  blocked_requests: number;
  pii_detections: number;
  baa_violations: number;
  policy_violations: number;
  pii_breakdown: { type: string; count: number }[];
  siem_breakdown: { event_type: string; count: number }[];
  high_risk_systems: { system_name: string; request_count: number }[];
  checklist: ComplianceReportChecklistItem[];
}

export async function getComplianceReport(
  period: '30d' | '90d' | '1y',
  format: 'json' | 'csv',
): Promise<ComplianceReportData | Blob> {
  if (format === 'csv') {
    const token = localStorage.getItem('totra_token');
    const res = await fetch(
      `${BASE_URL}/v1/compliance/report?period=${period}&format=csv`,
      { headers: { Authorization: `Bearer ${token ?? ''}` } },
    );
    if (!res.ok) throw new Error('Failed to fetch compliance report CSV');
    return res.blob();
  }
  const { data } = await apiClient.get<ComplianceReportData>(
    `/v1/compliance/report?period=${period}&format=json`,
  );
  return data;
}

// ---- Departments ----

export interface Department {
  id: string;
  tenant_id: string;
  name: string;
  slug: string;
  budget_usd: number | null;
  rpm_limit: number | null;
  tpm_limit: number | null;
  is_active: boolean;
  created_at: string;
  spend_usd: number;
}

export interface DeptUser {
  id: string;
  name: string;
  email: string;
  role: string;
  is_active: boolean;
}

export const listDepartments = () =>
  apiClient.get<{ departments: Department[] }>("/v1/departments");

export const createDepartment = (data: { name: string; slug: string }) =>
  apiClient.post<Department>("/v1/departments", data);

export const getDepartment = (id: string) =>
  apiClient.get<Department>(`/v1/departments/${id}`);

export const setDepartmentBudget = (
  id: string,
  data: { budget_usd: number | null; rpm_limit: number | null; tpm_limit: number | null }
) => apiClient.put<{ status: string }>(`/v1/departments/${id}/budget`, data);

export const deleteDepartment = (id: string) =>
  apiClient.delete<{ status: string }>(`/v1/departments/${id}`);

export const listDepartmentUsers = (id: string) =>
  apiClient.get<{ users: DeptUser[] }>(`/v1/departments/${id}/users`);

export const assignUserToDepartment = (deptId: string, userId: string) =>
  apiClient.put<{ status: string }>(`/v1/departments/${deptId}/users/${userId}`, {});
