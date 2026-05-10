import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Layout } from "./components/Layout";
import { ProtectedRoute } from "./components/ProtectedRoute";
import { LoginPage } from "./pages/LoginPage";
import { DashboardPage } from "./pages/admin/DashboardPage";
import { UsersPage } from "./pages/admin/UsersPage";
import { ModelsPage } from "./pages/admin/ModelsPage";
import { QuotaPage } from "./pages/admin/QuotaPage";
import { KpiPage } from "./pages/admin/KpiPage";
import { IntegrationsPage } from "./pages/admin/IntegrationsPage";
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
          <Route
            path="/"
            element={
              <ProtectedRoute>
                <Layout />
              </ProtectedRoute>
            }
          >
            <Route index element={<Navigate to="/admin/dashboard" replace />} />
            <Route path="admin/dashboard" element={<DashboardPage />} />
            <Route path="admin/users" element={<UsersPage />} />
            <Route path="admin/models" element={<ModelsPage />} />
            <Route path="admin/quota" element={<QuotaPage />} />
            <Route path="admin/kpi" element={<KpiPage />} />
            <Route path="admin/integrations" element={<IntegrationsPage />} />
            <Route path="me" element={<MyUsagePage />} />
          </Route>
          <Route path="*" element={<Navigate to="/admin/dashboard" replace />} />
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  );
}
