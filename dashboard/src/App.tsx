import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Layout } from "./components/Layout";
import { ProtectedRoute } from "./components/ProtectedRoute";
import { ErrorBoundary } from "./components/ErrorBoundary";
import { LoginPage } from "./pages/LoginPage";
import { DashboardPage } from "./pages/admin/DashboardPage";
import { UsersPage } from "./pages/admin/UsersPage";
import { ModelsPage } from "./pages/admin/ModelsPage";
import { QuotaPage } from "./pages/admin/QuotaPage";
import { IntegrationsPage } from "./pages/admin/IntegrationsPage";
import { DepartmentReportPage } from "./pages/admin/DepartmentReportPage";
import { IPAllowlistPage } from "./pages/admin/IPAllowlistPage";
import BotConfigPage from "./pages/admin/BotConfigPage";
import HRSyncPage from "./pages/admin/HRSyncPage";
import { AgentTrackingPage } from "./pages/admin/AgentTrackingPage";
import AuditLogPage from "./pages/admin/AuditLogPage";
import GDPRPage from "./pages/admin/GDPRPage";
import CompliancePage from "./pages/admin/CompliancePage";
import ComplianceChecklistPage from "./pages/admin/ComplianceChecklistPage";
import ComplianceAnomalyPage from "./pages/admin/ComplianceAnomalyPage";
import PolicyRulesPage from "./pages/admin/PolicyRulesPage";
import CostCenterPage from "./pages/admin/CostCenterPage";
import ProcurementPage from "./pages/admin/ProcurementPage";
import SIEMPage from "./pages/admin/SIEMPage";
import { SSOPage } from "./pages/admin/SSOPage";
import DataRetentionPage from "./pages/admin/DataRetentionPage";
import AlertConfigPage from "./pages/admin/AlertConfigPage";
import { MyUsagePage } from "./pages/employee/MyUsagePage";
import { SelfServicePage } from "./pages/employee/SelfServicePage";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, staleTime: 30_000 } },
});

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <ErrorBoundary>
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
            <Route path="admin/integrations" element={<ProtectedRoute adminOnly><IntegrationsPage /></ProtectedRoute>} />
            <Route path="admin/reports" element={<ProtectedRoute adminOnly><DepartmentReportPage /></ProtectedRoute>} />
            <Route path="admin/ip-allowlist" element={<ProtectedRoute adminOnly><IPAllowlistPage /></ProtectedRoute>} />
            <Route path="admin/bot-configs" element={<ProtectedRoute adminOnly><BotConfigPage /></ProtectedRoute>} />
            <Route path="admin/hr-sync" element={<ProtectedRoute adminOnly><HRSyncPage /></ProtectedRoute>} />
            <Route path="admin/agent-tracking" element={<ProtectedRoute adminOnly><AgentTrackingPage /></ProtectedRoute>} />
            <Route path="admin/audit-log" element={<ProtectedRoute adminOnly><AuditLogPage /></ProtectedRoute>} />
            <Route path="admin/gdpr" element={<ProtectedRoute adminOnly><GDPRPage /></ProtectedRoute>} />
            <Route path="admin/compliance" element={<ProtectedRoute adminOnly><CompliancePage /></ProtectedRoute>} />
            <Route path="admin/compliance/checklist" element={<ProtectedRoute adminOnly><ComplianceChecklistPage /></ProtectedRoute>} />
            <Route path="admin/compliance/anomalies" element={<ProtectedRoute adminOnly><ComplianceAnomalyPage /></ProtectedRoute>} />
            <Route path="admin/compliance/policy-rules" element={<ProtectedRoute adminOnly><PolicyRulesPage /></ProtectedRoute>} />
            <Route path="admin/cost" element={<ProtectedRoute adminOnly><CostCenterPage /></ProtectedRoute>} />
            <Route path="admin/cost/procurement" element={<ProtectedRoute adminOnly><ProcurementPage /></ProtectedRoute>} />
            <Route path="admin/siem" element={<ProtectedRoute adminOnly><SIEMPage /></ProtectedRoute>} />
            <Route path="admin/sso" element={<ProtectedRoute adminOnly><SSOPage /></ProtectedRoute>} />
            <Route path="admin/data-retention" element={<ProtectedRoute adminOnly><DataRetentionPage /></ProtectedRoute>} />
            <Route path="admin/alert-configs" element={<ProtectedRoute adminOnly><AlertConfigPage /></ProtectedRoute>} />
            <Route path="me" element={<MyUsagePage />} />
            <Route path="me/self-service" element={<SelfServicePage />} />
          </Route>
          <Route path="*" element={<Navigate to="/login" replace />} />
          </Routes>
        </BrowserRouter>
      </ErrorBoundary>
    </QueryClientProvider>
  );
}
