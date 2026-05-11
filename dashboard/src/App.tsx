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
import { DepartmentReportPage } from "./pages/admin/DepartmentReportPage";
import { IPAllowlistPage } from "./pages/admin/IPAllowlistPage";
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
            <Route path="admin/dashboard" element={<ProtectedRoute adminOnly><DashboardPage /></ProtectedRoute>} />
            <Route path="admin/users" element={<ProtectedRoute adminOnly><UsersPage /></ProtectedRoute>} />
            <Route path="admin/models" element={<ProtectedRoute adminOnly><ModelsPage /></ProtectedRoute>} />
            <Route path="admin/quota" element={<QuotaPage />} />
            <Route path="admin/kpi" element={<ProtectedRoute adminOnly><KpiPage /></ProtectedRoute>} />
            <Route path="admin/integrations" element={<ProtectedRoute adminOnly><IntegrationsPage /></ProtectedRoute>} />
            <Route path="admin/reports" element={<ProtectedRoute adminOnly><DepartmentReportPage /></ProtectedRoute>} />
            <Route path="admin/ip-allowlist" element={<ProtectedRoute adminOnly><IPAllowlistPage /></ProtectedRoute>} />
            <Route path="me" element={<MyUsagePage />} />
          </Route>
          <Route path="*" element={<Navigate to="/login" replace />} />
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  );
}
