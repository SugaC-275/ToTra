import { lazy, Suspense } from "react";
import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Layout } from "./components/Layout";
import { ProtectedRoute } from "./components/ProtectedRoute";
import { ErrorBoundary } from "./components/ErrorBoundary";

// Route components are code-split: each page loads on demand instead of
// shipping in the initial bundle.
const LoginPage = lazy(() => import("./pages/LoginPage").then((m) => ({ default: m.LoginPage })));
const DashboardPage = lazy(() => import("./pages/admin/DashboardPage").then((m) => ({ default: m.DashboardPage })));
const UsersPage = lazy(() => import("./pages/admin/UsersPage").then((m) => ({ default: m.UsersPage })));
const ModelsPage = lazy(() => import("./pages/admin/ModelsPage").then((m) => ({ default: m.ModelsPage })));
const QuotaPage = lazy(() => import("./pages/admin/QuotaPage").then((m) => ({ default: m.QuotaPage })));
const IntegrationsPage = lazy(() => import("./pages/admin/IntegrationsPage").then((m) => ({ default: m.IntegrationsPage })));
const DepartmentReportPage = lazy(() => import("./pages/admin/DepartmentReportPage").then((m) => ({ default: m.DepartmentReportPage })));
const IPAllowlistPage = lazy(() => import("./pages/admin/IPAllowlistPage").then((m) => ({ default: m.IPAllowlistPage })));
const BotConfigPage = lazy(() => import("./pages/admin/BotConfigPage"));
const HRSyncPage = lazy(() => import("./pages/admin/HRSyncPage"));
const AgentTrackingPage = lazy(() => import("./pages/admin/AgentTrackingPage").then((m) => ({ default: m.AgentTrackingPage })));
const AuditLogPage = lazy(() => import("./pages/admin/AuditLogPage"));
const GDPRPage = lazy(() => import("./pages/admin/GDPRPage"));
const CompliancePage = lazy(() => import("./pages/admin/CompliancePage"));
const ComplianceChecklistPage = lazy(() => import("./pages/admin/ComplianceChecklistPage"));
const ComplianceAnomalyPage = lazy(() => import("./pages/admin/ComplianceAnomalyPage"));
const PolicyRulesPage = lazy(() => import("./pages/admin/PolicyRulesPage"));
const CostCenterPage = lazy(() => import("./pages/admin/CostCenterPage"));
const ProcurementPage = lazy(() => import("./pages/admin/ProcurementPage"));
const SIEMPage = lazy(() => import("./pages/admin/SIEMPage"));
const SSOPage = lazy(() => import("./pages/admin/SSOPage").then((m) => ({ default: m.SSOPage })));
const DataRetentionPage = lazy(() => import("./pages/admin/DataRetentionPage"));
const AlertConfigPage = lazy(() => import("./pages/admin/AlertConfigPage"));
const MCPServersPage = lazy(() => import("./pages/admin/MCPServersPage").then((m) => ({ default: m.MCPServersPage })));
const PromptsPage = lazy(() => import("./pages/admin/PromptsPage").then((m) => ({ default: m.PromptsPage })));
const PromptPlaygroundPage = lazy(() => import("./pages/admin/PromptPlaygroundPage").then((m) => ({ default: m.PromptPlaygroundPage })));
const LogsPage = lazy(() => import("./pages/admin/LogsPage"));
const EvalsPage = lazy(() => import("./pages/admin/EvalsPage").then((m) => ({ default: m.EvalsPage })));
const DepartmentsPage = lazy(() => import("./pages/admin/DepartmentsPage").then((m) => ({ default: m.DepartmentsPage })));
const CachePage = lazy(() => import("./pages/admin/CachePage").then((m) => ({ default: m.CachePage })));
const BAAPage = lazy(() => import("./pages/admin/BAAPage"));
const ComplianceBundlesPage = lazy(() => import("./pages/admin/ComplianceBundlesPage"));
const MyUsagePage = lazy(() => import("./pages/employee/MyUsagePage").then((m) => ({ default: m.MyUsagePage })));
const SelfServicePage = lazy(() => import("./pages/employee/SelfServicePage").then((m) => ({ default: m.SelfServicePage })));

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, staleTime: 30_000 } },
});

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <ErrorBoundary>
        <BrowserRouter future={{ v7_startTransition: true }}>
          <Suspense
            fallback={
              <div className="flex min-h-screen items-center justify-center bg-zinc-950 text-zinc-500">
                Loading…
              </div>
            }
          >
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
                <Route path="admin/compliance/baa" element={<ProtectedRoute adminOnly><BAAPage /></ProtectedRoute>} />
                <Route path="admin/compliance/bundles" element={<ProtectedRoute adminOnly><ComplianceBundlesPage /></ProtectedRoute>} />
                <Route path="admin/cost" element={<ProtectedRoute adminOnly><CostCenterPage /></ProtectedRoute>} />
                <Route path="admin/cost/procurement" element={<ProtectedRoute adminOnly><ProcurementPage /></ProtectedRoute>} />
                <Route path="admin/siem" element={<ProtectedRoute adminOnly><SIEMPage /></ProtectedRoute>} />
                <Route path="admin/sso" element={<ProtectedRoute adminOnly><SSOPage /></ProtectedRoute>} />
                <Route path="admin/data-retention" element={<ProtectedRoute adminOnly><DataRetentionPage /></ProtectedRoute>} />
                <Route path="admin/alert-configs" element={<ProtectedRoute adminOnly><AlertConfigPage /></ProtectedRoute>} />
                <Route path="admin/mcp-servers" element={<ProtectedRoute adminOnly><MCPServersPage /></ProtectedRoute>} />
                <Route path="admin/prompts" element={<ProtectedRoute adminOnly><PromptsPage /></ProtectedRoute>} />
                <Route path="admin/prompt-playground" element={<ProtectedRoute adminOnly><PromptPlaygroundPage /></ProtectedRoute>} />
                <Route path="admin/logs" element={<ProtectedRoute adminOnly><LogsPage /></ProtectedRoute>} />
                <Route path="admin/evals" element={<ProtectedRoute adminOnly><EvalsPage /></ProtectedRoute>} />
                <Route path="admin/departments" element={<ProtectedRoute adminOnly><DepartmentsPage /></ProtectedRoute>} />
                <Route path="admin/cache" element={<ProtectedRoute adminOnly><CachePage /></ProtectedRoute>} />
                <Route path="me" element={<MyUsagePage />} />
                <Route path="me/self-service" element={<SelfServicePage />} />
              </Route>
              <Route path="*" element={<Navigate to="/login" replace />} />
            </Routes>
          </Suspense>
        </BrowserRouter>
      </ErrorBoundary>
    </QueryClientProvider>
  );
}
