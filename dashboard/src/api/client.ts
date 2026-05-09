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
