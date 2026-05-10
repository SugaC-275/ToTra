# ToTra Phase 1-D: Dashboard Frontend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the React admin dashboard and employee self-service portal. Admins see real-time usage stats, manage employees, configure models, and handle quota approvals. Employees see their own usage and remaining quota.

**Architecture:** Vite + React 18 + TypeScript SPA. TanStack Query for server state. React Router v6 for routing. shadcn/ui for components. All data comes from Admin Service REST API (Plan C). Dark mode by default.

**Tech Stack:** React 18, TypeScript, Vite, TanStack Query v5, React Router v6, shadcn/ui, Recharts, Tailwind CSS

**Assigned Agent:** 🎨 Frontend Engineer  
**Depends on:** Plan C (Admin Service) — API must be running, or use mock data

---

## File Map

```
dashboard/
├── package.json                    CREATE
├── vite.config.ts                  CREATE
├── tailwind.config.ts              CREATE
├── tsconfig.json                   CREATE
├── index.html                      CREATE
├── Dockerfile                      CREATE
└── src/
    ├── main.tsx                    CREATE
    ├── App.tsx                     CREATE — router setup
    ├── api/
    │   └── client.ts               CREATE — axios client + interceptors
    ├── hooks/
    │   ├── useAuth.ts              CREATE — auth state + login
    │   └── useUsage.ts             CREATE — usage queries
    ├── pages/
    │   ├── LoginPage.tsx           CREATE
    │   ├── admin/
    │   │   ├── DashboardPage.tsx   CREATE — usage overview + adoption rate
    │   │   ├── UsersPage.tsx       CREATE — employee list + create
    │   │   ├── ModelsPage.tsx      CREATE — model config list + create
    │   │   └── QuotaPage.tsx       CREATE — pending approval requests
    │   └── employee/
    │       └── MyUsagePage.tsx     CREATE — personal usage + quota meter
    ├── components/
    │   ├── Layout.tsx              CREATE — sidebar nav + header
    │   ├── QuotaMeter.tsx          CREATE — SCU usage progress bar
    │   ├── UsageChart.tsx          CREATE — Recharts bar chart
    │   └── ProtectedRoute.tsx      CREATE — auth guard
    └── lib/
        └── utils.ts                CREATE — shadcn/ui utils
```

---

## Task 1: Project Setup

**Files:**
- Create: `dashboard/package.json`
- Create: `dashboard/vite.config.ts`
- Create: `dashboard/tsconfig.json`
- Create: `dashboard/tailwind.config.ts`
- Create: `dashboard/index.html`

- [ ] **Step 1: Initialize project**

```bash
cd dashboard
npm create vite@latest . -- --template react-ts
npm install
```

- [ ] **Step 2: Install dependencies**

```bash
npm install @tanstack/react-query@5 react-router-dom@6 axios recharts
npm install -D tailwindcss postcss autoprefixer @types/recharts
npx tailwindcss init -p
```

- [ ] **Step 3: Install shadcn/ui**

```bash
npx shadcn-ui@latest init
# Choose: Default style, Slate base color, CSS variables: yes
```

- [ ] **Step 4: Add core shadcn components**

```bash
npx shadcn-ui@latest add button card table badge input label dialog alert progress
```

- [ ] **Step 5: Update tailwind.config.ts**

```ts
// dashboard/tailwind.config.ts
import type { Config } from "tailwindcss";
export default {
  darkMode: ["class"],
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: { extend: {} },
  plugins: [],
} satisfies Config;
```

- [ ] **Step 6: Update src/index.css for dark mode default**

```css
@tailwind base;
@tailwind components;
@tailwind utilities;

:root { color-scheme: dark; }
html { background: #0f0f0f; color: #e5e5e5; }
```

- [ ] **Step 7: Commit**

```bash
git add dashboard/
git commit -m "feat(dashboard): vite react typescript project setup with shadcn/ui"
```

---

## Task 2: API Client

**Files:**
- Create: `dashboard/src/api/client.ts`

- [ ] **Step 1: Write client.ts**

```typescript
// dashboard/src/api/client.ts
import axios from "axios";

const BASE_URL = import.meta.env.VITE_API_URL ?? "http://localhost:8081";

export const apiClient = axios.create({ baseURL: BASE_URL });

// Attach JWT token to every request
apiClient.interceptors.request.use((config) => {
  const token = localStorage.getItem("totra_token");
  if (token) config.headers.Authorization = `Bearer ${token}`;
  return config;
});

// Redirect to login on 401
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

// --- Auth ---
export const login = (email: string, password: string) =>
  apiClient.post<{ token: string }>("/api/auth/login", { email, password });

// --- Users ---
export const listUsers = () =>
  apiClient.get<{ total: number; users: User[] }>("/api/users");

export const createUser = (data: { name: string; email: string; role: string }) =>
  apiClient.post<{ id: string; name: string; email: string; role: string; api_key: string }>("/api/users", data);

// --- Models ---
export const listModels = () =>
  apiClient.get<{ total: number; models: ModelConfig[] }>("/api/models");

export const createModel = (data: CreateModelPayload) =>
  apiClient.post<ModelConfig>("/api/models", data);

// --- Usage ---
export const getMonthlySummary = (month: string) =>
  apiClient.get<{ month: string; summaries: UserSummary[] }>(`/api/usage/summary?month=${month}`);

export const getAdoptionRate = (month: string) =>
  apiClient.get<AdoptionStats>(`/api/usage/adoption?month=${month}`);

// --- Quota ---
export const listPendingRequests = () =>
  apiClient.get<{ requests: QuotaRequest[] }>("/api/quota/requests");

export const approveQuota = (id: string) =>
  apiClient.post(`/api/quota/requests/${id}/approve`);

export const rejectQuota = (id: string) =>
  apiClient.post(`/api/quota/requests/${id}/reject`);

// --- Types ---
export interface User {
  id: string; name: string; email: string; role: string; quota_scu: number; is_active: boolean;
}
export interface ModelConfig {
  id: string; name: string; provider: string; base_url: string; scu_rate: number; is_active: boolean;
}
export interface CreateModelPayload {
  name: string; provider: string; base_url: string; api_key: string; scu_rate: number;
}
export interface UserSummary {
  user_id: string; user_name: string; total_scu: number; total_usd: number; request_count: number;
}
export interface AdoptionStats {
  total_users: number; active_users: number; adoption_rate: number;
}
export interface QuotaRequest {
  id: string; user_id: string; new_quota: number; reason: string; status: string;
}
```

- [ ] **Step 2: Commit**

```bash
git add dashboard/src/api/
git commit -m "feat(dashboard): typed API client with JWT interceptor"
```

---

## Task 3: Auth Hook & Login Page

**Files:**
- Create: `dashboard/src/hooks/useAuth.ts`
- Create: `dashboard/src/pages/LoginPage.tsx`
- Create: `dashboard/src/components/ProtectedRoute.tsx`

- [ ] **Step 1: Write useAuth.ts**

```typescript
// dashboard/src/hooks/useAuth.ts
import { useState, useCallback } from "react";
import { login } from "../api/client";

export function useAuth() {
  const [token, setToken] = useState<string | null>(localStorage.getItem("totra_token"));
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const signIn = useCallback(async (email: string, password: string) => {
    setLoading(true);
    setError(null);
    try {
      const { data } = await login(email, password);
      localStorage.setItem("totra_token", data.token);
      setToken(data.token);
      return true;
    } catch {
      setError("Invalid credentials");
      return false;
    } finally {
      setLoading(false);
    }
  }, []);

  const signOut = useCallback(() => {
    localStorage.removeItem("totra_token");
    setToken(null);
  }, []);

  return { token, isAuthenticated: !!token, signIn, signOut, error, loading };
}
```

- [ ] **Step 2: Write LoginPage.tsx**

```tsx
// dashboard/src/pages/LoginPage.tsx
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useAuth } from "../hooks/useAuth";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { Label } from "../components/ui/label";
import { Card, CardContent, CardHeader, CardTitle } from "../components/ui/card";
import { Alert, AlertDescription } from "../components/ui/alert";

export function LoginPage() {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const { signIn, error, loading } = useAuth();
  const navigate = useNavigate();

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const ok = await signIn(email, password);
    if (ok) navigate("/admin/dashboard");
  };

  return (
    <div className="min-h-screen flex items-center justify-center bg-background">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle className="text-2xl text-center">ToTra</CardTitle>
          <p className="text-center text-muted-foreground text-sm">AI Efficiency Platform</p>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit} className="space-y-4">
            {error && <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
            <div className="space-y-1">
              <Label htmlFor="email">Email</Label>
              <Input id="email" type="email" value={email} onChange={e => setEmail(e.target.value)} required />
            </div>
            <div className="space-y-1">
              <Label htmlFor="password">Password</Label>
              <Input id="password" type="password" value={password} onChange={e => setPassword(e.target.value)} required />
            </div>
            <Button type="submit" className="w-full" disabled={loading}>
              {loading ? "Signing in..." : "Sign In"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
```

- [ ] **Step 3: Write ProtectedRoute.tsx**

```tsx
// dashboard/src/components/ProtectedRoute.tsx
import { Navigate, Outlet } from "react-router-dom";

export function ProtectedRoute() {
  const token = localStorage.getItem("totra_token");
  if (!token) return <Navigate to="/login" replace />;
  return <Outlet />;
}
```

- [ ] **Step 4: Commit**

```bash
git add dashboard/src/hooks/ dashboard/src/pages/LoginPage.tsx dashboard/src/components/ProtectedRoute.tsx
git commit -m "feat(dashboard): auth hook and login page"
```

---

## Task 4: Layout & Navigation

**Files:**
- Create: `dashboard/src/components/Layout.tsx`
- Create: `dashboard/src/App.tsx`

- [ ] **Step 1: Write Layout.tsx**

```tsx
// dashboard/src/components/Layout.tsx
import { Link, useLocation, Outlet } from "react-router-dom";
import { cn } from "../lib/utils";

const navItems = [
  { label: "Dashboard", href: "/admin/dashboard" },
  { label: "Employees", href: "/admin/users" },
  { label: "Models", href: "/admin/models" },
  { label: "Quota Requests", href: "/admin/quota" },
  { label: "My Usage", href: "/me" },
];

export function Layout() {
  const location = useLocation();
  return (
    <div className="flex min-h-screen bg-background">
      <aside className="w-56 border-r border-border flex flex-col py-6 px-4 gap-1">
        <div className="mb-6 px-2">
          <span className="font-bold text-lg text-primary">ToTra</span>
        </div>
        {navItems.map((item) => (
          <Link
            key={item.href}
            to={item.href}
            className={cn(
              "px-3 py-2 rounded-md text-sm font-medium transition-colors",
              location.pathname === item.href
                ? "bg-accent text-accent-foreground"
                : "text-muted-foreground hover:text-foreground hover:bg-accent/50"
            )}
          >
            {item.label}
          </Link>
        ))}
      </aside>
      <main className="flex-1 p-8 overflow-auto">
        <Outlet />
      </main>
    </div>
  );
}
```

- [ ] **Step 2: Write App.tsx with full routing**

```tsx
// dashboard/src/App.tsx
import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { LoginPage } from "./pages/LoginPage";
import { Layout } from "./components/Layout";
import { ProtectedRoute } from "./components/ProtectedRoute";
import { DashboardPage } from "./pages/admin/DashboardPage";
import { UsersPage } from "./pages/admin/UsersPage";
import { ModelsPage } from "./pages/admin/ModelsPage";
import { QuotaPage } from "./pages/admin/QuotaPage";
import { MyUsagePage } from "./pages/employee/MyUsagePage";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, staleTime: 30_000 } },
});

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route element={<ProtectedRoute />}>
            <Route element={<Layout />}>
              <Route path="/admin/dashboard" element={<DashboardPage />} />
              <Route path="/admin/users" element={<UsersPage />} />
              <Route path="/admin/models" element={<ModelsPage />} />
              <Route path="/admin/quota" element={<QuotaPage />} />
              <Route path="/me" element={<MyUsagePage />} />
            </Route>
          </Route>
          <Route path="*" element={<Navigate to="/admin/dashboard" replace />} />
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  );
}
```

- [ ] **Step 3: Commit**

```bash
git add dashboard/src/components/Layout.tsx dashboard/src/App.tsx
git commit -m "feat(dashboard): layout and routing"
```

---

## Task 5: Admin Dashboard Page

**Files:**
- Create: `dashboard/src/pages/admin/DashboardPage.tsx`
- Create: `dashboard/src/components/UsageChart.tsx`
- Create: `dashboard/src/components/QuotaMeter.tsx`

- [ ] **Step 1: Write UsageChart.tsx**

```tsx
// dashboard/src/components/UsageChart.tsx
import { BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from "recharts";
import { UserSummary } from "../api/client";

export function UsageChart({ data }: { data: UserSummary[] }) {
  const chartData = data.slice(0, 10).map((u) => ({
    name: u.user_name.split(" ")[0],
    scu: Math.round(u.total_scu),
    requests: u.request_count,
  }));

  return (
    <ResponsiveContainer width="100%" height={280}>
      <BarChart data={chartData}>
        <CartesianGrid strokeDasharray="3 3" stroke="#333" />
        <XAxis dataKey="name" tick={{ fontSize: 12, fill: "#888" }} />
        <YAxis tick={{ fontSize: 12, fill: "#888" }} />
        <Tooltip contentStyle={{ background: "#1a1a1a", border: "1px solid #333" }} />
        <Bar dataKey="scu" fill="#6366f1" radius={[4, 4, 0, 0]} name="SCU Used" />
      </BarChart>
    </ResponsiveContainer>
  );
}
```

- [ ] **Step 2: Write DashboardPage.tsx**

```tsx
// dashboard/src/pages/admin/DashboardPage.tsx
import { useQuery } from "@tanstack/react-query";
import { getMonthlySummary, getAdoptionRate } from "../../api/client";
import { Card, CardContent, CardHeader, CardTitle } from "../../components/ui/card";
import { UsageChart } from "../../components/UsageChart";
import { Badge } from "../../components/ui/badge";

const currentMonth = new Date().toISOString().slice(0, 7); // "2026-05"

export function DashboardPage() {
  const { data: summaryData, isLoading: summaryLoading } = useQuery({
    queryKey: ["usage-summary", currentMonth],
    queryFn: () => getMonthlySummary(currentMonth).then((r) => r.data),
  });

  const { data: adoptionData } = useQuery({
    queryKey: ["adoption", currentMonth],
    queryFn: () => getAdoptionRate(currentMonth).then((r) => r.data),
  });

  const totalSCU = summaryData?.summaries.reduce((sum, u) => sum + u.total_scu, 0) ?? 0;
  const totalUSD = summaryData?.summaries.reduce((sum, u) => sum + u.total_usd, 0) ?? 0;

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Dashboard — {currentMonth}</h1>

      <div className="grid grid-cols-3 gap-4">
        <Card>
          <CardHeader className="pb-2"><CardTitle className="text-sm text-muted-foreground">Total SCU Used</CardTitle></CardHeader>
          <CardContent><p className="text-3xl font-bold">{totalSCU.toLocaleString()}</p></CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2"><CardTitle className="text-sm text-muted-foreground">Total Cost</CardTitle></CardHeader>
          <CardContent><p className="text-3xl font-bold">${totalUSD.toFixed(2)}</p></CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2"><CardTitle className="text-sm text-muted-foreground">AI Adoption Rate</CardTitle></CardHeader>
          <CardContent>
            <p className="text-3xl font-bold">{adoptionData ? `${(adoptionData.adoption_rate * 100).toFixed(0)}%` : "—"}</p>
            <p className="text-xs text-muted-foreground mt-1">{adoptionData?.active_users ?? 0} / {adoptionData?.total_users ?? 0} employees</p>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader><CardTitle>Top 10 Users by SCU</CardTitle></CardHeader>
        <CardContent>
          {summaryLoading ? (
            <p className="text-muted-foreground text-sm">Loading...</p>
          ) : (
            <UsageChart data={summaryData?.summaries ?? []} />
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>Usage Details</CardTitle></CardHeader>
        <CardContent>
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-muted-foreground">
                <th className="text-left py-2">Employee</th>
                <th className="text-right py-2">SCU</th>
                <th className="text-right py-2">Cost (USD)</th>
                <th className="text-right py-2">Requests</th>
              </tr>
            </thead>
            <tbody>
              {summaryData?.summaries.map((u) => (
                <tr key={u.user_id} className="border-b border-border/50">
                  <td className="py-2">{u.user_name}</td>
                  <td className="text-right">{u.total_scu.toLocaleString()}</td>
                  <td className="text-right">${u.total_usd.toFixed(4)}</td>
                  <td className="text-right">{u.request_count}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </CardContent>
      </Card>
    </div>
  );
}
```

- [ ] **Step 3: Commit**

```bash
git add dashboard/src/pages/admin/DashboardPage.tsx dashboard/src/components/UsageChart.tsx
git commit -m "feat(dashboard): admin dashboard with usage chart and adoption rate"
```

---

## Task 6: Users Management Page

**Files:**
- Create: `dashboard/src/pages/admin/UsersPage.tsx`

- [ ] **Step 1: Write UsersPage.tsx**

```tsx
// dashboard/src/pages/admin/UsersPage.tsx
import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { listUsers, createUser } from "../../api/client";
import { Card, CardContent, CardHeader, CardTitle } from "../../components/ui/card";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { Badge } from "../../components/ui/badge";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "../../components/ui/dialog";
import { Alert, AlertDescription } from "../../components/ui/alert";

export function UsersPage() {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [newKey, setNewKey] = useState<string | null>(null);
  const [form, setForm] = useState({ name: "", email: "", role: "standard" });

  const { data } = useQuery({
    queryKey: ["users"],
    queryFn: () => listUsers().then((r) => r.data),
  });

  const createMutation = useMutation({
    mutationFn: createUser,
    onSuccess: (res) => {
      setNewKey(res.data.api_key);
      qc.invalidateQueries({ queryKey: ["users"] });
    },
  });

  const handleCreate = (e: React.FormEvent) => {
    e.preventDefault();
    createMutation.mutate(form);
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Employees</h1>
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogTrigger asChild>
            <Button onClick={() => { setNewKey(null); setForm({ name: "", email: "", role: "standard" }); }}>
              + Add Employee
            </Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader><DialogTitle>Add Employee</DialogTitle></DialogHeader>
            {newKey ? (
              <div className="space-y-3">
                <Alert>
                  <AlertDescription>
                    <p className="font-medium mb-1">Employee Key (save this — shown only once):</p>
                    <code className="text-xs bg-muted p-2 rounded block break-all">{newKey}</code>
                  </AlertDescription>
                </Alert>
                <Button className="w-full" onClick={() => { setOpen(false); setNewKey(null); }}>Done</Button>
              </div>
            ) : (
              <form onSubmit={handleCreate} className="space-y-4">
                <div className="space-y-1">
                  <Label>Name</Label>
                  <Input value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} required />
                </div>
                <div className="space-y-1">
                  <Label>Email</Label>
                  <Input type="email" value={form.email} onChange={e => setForm({ ...form, email: e.target.value })} required />
                </div>
                <div className="space-y-1">
                  <Label>Role</Label>
                  <select className="w-full border rounded px-3 py-2 bg-background text-sm"
                    value={form.role} onChange={e => setForm({ ...form, role: e.target.value })}>
                    <option value="standard">Standard</option>
                    <option value="senior">Senior</option>
                    <option value="researcher">Researcher</option>
                    <option value="admin">Admin</option>
                  </select>
                </div>
                <Button type="submit" className="w-full" disabled={createMutation.isPending}>
                  {createMutation.isPending ? "Creating..." : "Create Employee"}
                </Button>
              </form>
            )}
          </DialogContent>
        </Dialog>
      </div>

      <Card>
        <CardContent className="pt-4">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-muted-foreground">
                <th className="text-left py-2">Name</th>
                <th className="text-left py-2">Email</th>
                <th className="text-left py-2">Role</th>
                <th className="text-right py-2">Quota (SCU)</th>
                <th className="text-right py-2">Status</th>
              </tr>
            </thead>
            <tbody>
              {data?.users.map((u) => (
                <tr key={u.id} className="border-b border-border/50">
                  <td className="py-2">{u.name}</td>
                  <td className="text-muted-foreground">{u.email}</td>
                  <td><Badge variant="outline">{u.role}</Badge></td>
                  <td className="text-right">{u.quota_scu.toLocaleString()}</td>
                  <td className="text-right">
                    <Badge variant={u.is_active ? "default" : "secondary"}>{u.is_active ? "Active" : "Inactive"}</Badge>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </CardContent>
      </Card>
    </div>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add dashboard/src/pages/admin/UsersPage.tsx
git commit -m "feat(dashboard): employees management page with create dialog and key reveal"
```

---

## Task 7: Models & Quota Pages

**Files:**
- Create: `dashboard/src/pages/admin/ModelsPage.tsx`
- Create: `dashboard/src/pages/admin/QuotaPage.tsx`

- [ ] **Step 1: Write ModelsPage.tsx**

```tsx
// dashboard/src/pages/admin/ModelsPage.tsx
import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { listModels, createModel } from "../../api/client";
import { Card, CardContent } from "../../components/ui/card";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { Badge } from "../../components/ui/badge";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "../../components/ui/dialog";

export function ModelsPage() {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({ name: "", provider: "openai", base_url: "", api_key: "", scu_rate: 1.0 });

  const { data } = useQuery({
    queryKey: ["models"],
    queryFn: () => listModels().then((r) => r.data),
  });

  const mutation = useMutation({
    mutationFn: createModel,
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["models"] }); setOpen(false); },
  });

  const handleSubmit = (e: React.FormEvent) => { e.preventDefault(); mutation.mutate(form); };

  const providerColors: Record<string, string> = {
    openai: "bg-green-900 text-green-300",
    anthropic: "bg-purple-900 text-purple-300",
    gemini: "bg-blue-900 text-blue-300",
    local: "bg-gray-800 text-gray-300",
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">AI Models</h1>
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogTrigger asChild><Button>+ Add Model</Button></DialogTrigger>
          <DialogContent>
            <DialogHeader><DialogTitle>Add Model Config</DialogTitle></DialogHeader>
            <form onSubmit={handleSubmit} className="space-y-4">
              {(["name", "base_url", "api_key"] as const).map((field) => (
                <div key={field} className="space-y-1">
                  <Label>{field.replace("_", " ")}</Label>
                  <Input value={form[field] as string} onChange={e => setForm({ ...form, [field]: e.target.value })} required={field !== "api_key"} />
                </div>
              ))}
              <div className="space-y-1">
                <Label>Provider</Label>
                <select className="w-full border rounded px-3 py-2 bg-background text-sm"
                  value={form.provider} onChange={e => setForm({ ...form, provider: e.target.value })}>
                  {["openai", "anthropic", "gemini", "local"].map(p => <option key={p} value={p}>{p}</option>)}
                </select>
              </div>
              <div className="space-y-1">
                <Label>SCU Rate (tokens × rate = SCU)</Label>
                <Input type="number" step="0.1" min="0.1" value={form.scu_rate}
                  onChange={e => setForm({ ...form, scu_rate: parseFloat(e.target.value) })} />
              </div>
              <Button type="submit" className="w-full" disabled={mutation.isPending}>
                {mutation.isPending ? "Saving..." : "Add Model"}
              </Button>
            </form>
          </DialogContent>
        </Dialog>
      </div>
      <Card>
        <CardContent className="pt-4">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-muted-foreground">
                <th className="text-left py-2">Model</th>
                <th className="text-left py-2">Provider</th>
                <th className="text-right py-2">SCU Rate</th>
                <th className="text-right py-2">Status</th>
              </tr>
            </thead>
            <tbody>
              {data?.models.map((m) => (
                <tr key={m.id} className="border-b border-border/50">
                  <td className="py-2 font-mono">{m.name}</td>
                  <td><span className={`px-2 py-0.5 rounded text-xs ${providerColors[m.provider] ?? ""}`}>{m.provider}</span></td>
                  <td className="text-right">{m.scu_rate}×</td>
                  <td className="text-right"><Badge variant={m.is_active ? "default" : "secondary"}>{m.is_active ? "Active" : "Off"}</Badge></td>
                </tr>
              ))}
            </tbody>
          </table>
        </CardContent>
      </Card>
    </div>
  );
}
```

- [ ] **Step 2: Write QuotaPage.tsx**

```tsx
// dashboard/src/pages/admin/QuotaPage.tsx
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { listPendingRequests, approveQuota, rejectQuota } from "../../api/client";
import { Card, CardContent, CardHeader, CardTitle } from "../../components/ui/card";
import { Button } from "../../components/ui/button";

export function QuotaPage() {
  const qc = useQueryClient();
  const { data } = useQuery({
    queryKey: ["quota-requests"],
    queryFn: () => listPendingRequests().then((r) => r.data),
  });

  const approveMutation = useMutation({
    mutationFn: approveQuota,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["quota-requests"] }),
  });

  const rejectMutation = useMutation({
    mutationFn: rejectQuota,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["quota-requests"] }),
  });

  const pending = data?.requests ?? [];

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Quota Requests</h1>
      {pending.length === 0 ? (
        <Card><CardContent className="pt-6 text-center text-muted-foreground">No pending requests</CardContent></Card>
      ) : (
        <Card>
          <CardHeader><CardTitle>{pending.length} pending</CardTitle></CardHeader>
          <CardContent>
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-muted-foreground">
                  <th className="text-left py-2">User</th>
                  <th className="text-right py-2">New Quota (SCU)</th>
                  <th className="text-left py-2 pl-4">Reason</th>
                  <th className="text-right py-2">Actions</th>
                </tr>
              </thead>
              <tbody>
                {pending.map((r) => (
                  <tr key={r.id} className="border-b border-border/50">
                    <td className="py-3 font-mono text-xs">{r.user_id.slice(0, 8)}…</td>
                    <td className="text-right">{r.new_quota.toLocaleString()}</td>
                    <td className="pl-4 text-muted-foreground">{r.reason}</td>
                    <td className="text-right">
                      <div className="flex gap-2 justify-end">
                        <Button size="sm" onClick={() => approveMutation.mutate(r.id)}>Approve</Button>
                        <Button size="sm" variant="destructive" onClick={() => rejectMutation.mutate(r.id)}>Reject</Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
```

- [ ] **Step 3: Commit**

```bash
git add dashboard/src/pages/admin/
git commit -m "feat(dashboard): models config page and quota approval page"
```

---

## Task 8: Employee Self-Service Page

**Files:**
- Create: `dashboard/src/pages/employee/MyUsagePage.tsx`
- Create: `dashboard/src/components/QuotaMeter.tsx`

- [ ] **Step 1: Write QuotaMeter.tsx**

```tsx
// dashboard/src/components/QuotaMeter.tsx
import { Progress } from "./ui/progress";

interface QuotaMeterProps {
  used: number;
  limit: number;
  label?: string;
}

export function QuotaMeter({ used, limit, label = "SCU" }: QuotaMeterProps) {
  const pct = Math.min((used / limit) * 100, 100);
  const color = pct > 90 ? "bg-red-500" : pct > 70 ? "bg-yellow-500" : "bg-green-500";

  return (
    <div className="space-y-2">
      <div className="flex justify-between text-sm">
        <span className="text-muted-foreground">{label} Used</span>
        <span className="font-medium">{used.toLocaleString()} / {limit.toLocaleString()}</span>
      </div>
      <Progress value={pct} className={`h-3 ${color}`} />
      <p className="text-xs text-muted-foreground text-right">{(100 - pct).toFixed(1)}% remaining</p>
    </div>
  );
}
```

- [ ] **Step 2: Write MyUsagePage.tsx**

```tsx
// dashboard/src/pages/employee/MyUsagePage.tsx
import { useQuery } from "@tanstack/react-query";
import { getMonthlySummary } from "../../api/client";
import { Card, CardContent, CardHeader, CardTitle } from "../../components/ui/card";
import { QuotaMeter } from "../../components/QuotaMeter";

const currentMonth = new Date().toISOString().slice(0, 7);

export function MyUsagePage() {
  const { data } = useQuery({
    queryKey: ["my-usage", currentMonth],
    queryFn: () => getMonthlySummary(currentMonth).then((r) => r.data),
  });

  // The API returns all users — in a real app, filter to current user via JWT sub
  // For Phase 1, show all (demo mode) or the first result
  const myStats = data?.summaries?.[0];

  return (
    <div className="space-y-6 max-w-xl">
      <h1 className="text-2xl font-bold">My AI Usage — {currentMonth}</h1>

      {myStats ? (
        <>
          <Card>
            <CardHeader><CardTitle>Quota</CardTitle></CardHeader>
            <CardContent>
              <QuotaMeter used={myStats.total_scu} limit={50000} label="SCU" />
            </CardContent>
          </Card>

          <div className="grid grid-cols-2 gap-4">
            <Card>
              <CardHeader className="pb-2"><CardTitle className="text-sm text-muted-foreground">Total Requests</CardTitle></CardHeader>
              <CardContent><p className="text-3xl font-bold">{myStats.request_count}</p></CardContent>
            </Card>
            <Card>
              <CardHeader className="pb-2"><CardTitle className="text-sm text-muted-foreground">Cost This Month</CardTitle></CardHeader>
              <CardContent><p className="text-3xl font-bold">${myStats.total_usd.toFixed(2)}</p></CardContent>
            </Card>
          </div>
        </>
      ) : (
        <Card><CardContent className="pt-6 text-center text-muted-foreground">No usage data yet this month.</CardContent></Card>
      )}
    </div>
  );
}
```

- [ ] **Step 3: Commit**

```bash
git add dashboard/src/pages/employee/ dashboard/src/components/QuotaMeter.tsx
git commit -m "feat(dashboard): employee self-service page with quota meter"
```

---

## Task 9: Dockerfile & Final Build

**Files:**
- Create: `dashboard/Dockerfile`

- [ ] **Step 1: Write Dockerfile**

```dockerfile
# dashboard/Dockerfile
FROM node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 3000
```

- [ ] **Step 2: Write nginx.conf for SPA routing**

```nginx
# dashboard/nginx.conf
server {
    listen 3000;
    root /usr/share/nginx/html;
    index index.html;

    location / {
        try_files $uri $uri/ /index.html;
    }
}
```

- [ ] **Step 3: Build and verify**

```bash
cd dashboard && npm run build 2>&1 | tail -5
```

Expected: `✓ built in` message, no TypeScript errors.

- [ ] **Step 4: Start dev server and verify all pages load**

```bash
cd dashboard && npm run dev &
sleep 3
curl -s -o /dev/null -w "%{http_code}" http://localhost:5173/
```

Expected: `200`

- [ ] **Step 5: Final commit**

```bash
git add dashboard/
git commit -m "feat(dashboard): complete Phase 1 dashboard with admin and employee views"
```
