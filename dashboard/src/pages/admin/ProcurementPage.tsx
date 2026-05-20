import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { apiClient } from "../../api/client";

interface DeptBudget {
  department: string;
  monthly_usd: number;
  annual_usd: number;
  growth_rate_percent: number;
}

interface QuarterlyBudget {
  quarter: string;
  usd: number;
}

interface BudgetPlan {
  tenant_id: string;
  plan_year: number;
  total_annual_usd: number;
  by_department: DeptBudget[];
  quarterly: QuarterlyBudget[];
  growth_rate_used_percent: number;
  generated_at: string;
}

interface ProviderSpend {
  provider: string;
  total_usd: number;
  request_count: number;
  share_percent: number;
  mom_growth_percent: number;
}

interface ModelSpend {
  model: string;
  provider: string;
  total_usd: number;
  request_count: number;
}

interface ProcurementReport {
  tenant_id: string;
  period_months: number;
  total_usd: number;
  by_provider: ProviderSpend[];
  top_models: ModelSpend[];
  peak_month_usd: number;
  peak_month: string;
  avg_monthly_usd: number;
  forecasted_eoy_usd: number;
  generated_at: string;
}

const PERIOD_OPTIONS = [3, 6, 12];

export default function ProcurementPage() {
  const [months, setMonths] = useState(6);
  const nextYear = new Date().getFullYear() + 1;

  const { data: budgetPlan } = useQuery<BudgetPlan>({
    queryKey: ["budget-plan", nextYear],
    queryFn: () =>
      apiClient
        .get<BudgetPlan>(`/api/admin/cost/budget-plan?year=${nextYear}`)
        .then((r) => r.data),
  });

  const { data, isLoading } = useQuery<ProcurementReport>({
    queryKey: ["procurement-report", months],
    queryFn: () =>
      apiClient
        .get<ProcurementReport>(`/api/admin/cost/procurement-report?months=${months}`)
        .then((r) => r.data),
  });

  if (isLoading) return <div className="p-8 text-gray-400">Loading procurement data…</div>;

  return (
    <div className="p-8 max-w-5xl mx-auto space-y-8">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold mb-1">Procurement Report</h1>
          <p className="text-gray-400 text-sm">
            Provider spend analysis to support vendor negotiation and budget planning.
          </p>
        </div>
        <div className="flex gap-2">
          {PERIOD_OPTIONS.map((m) => (
            <button
              key={m}
              onClick={() => setMonths(m)}
              className={`px-3 py-1.5 rounded-lg text-sm font-medium ${
                months === m
                  ? "bg-blue-600 text-white"
                  : "bg-gray-700 text-gray-300 hover:bg-gray-600"
              }`}
            >
              {m}M
            </button>
          ))}
        </div>
      </div>

      {/* Summary cards */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        {[
          { label: `Total (${months}M)`, value: `$${data?.total_usd.toFixed(2) ?? "—"}` },
          { label: "Avg / Month", value: `$${data?.avg_monthly_usd.toFixed(2) ?? "—"}` },
          { label: "Peak Month", value: data?.peak_month ?? "—", sub: `$${data?.peak_month_usd.toFixed(2) ?? "—"}` },
          { label: "EOY Forecast", value: `$${data?.forecasted_eoy_usd.toFixed(2) ?? "—"}` },
        ].map(({ label, value, sub }) => (
          <div key={label} className="bg-gray-800 rounded-xl p-4">
            <p className="text-xs text-gray-400 mb-1">{label}</p>
            <p className="text-xl font-bold text-gray-100">{value}</p>
            {sub && <p className="text-xs text-gray-500 mt-0.5">{sub}</p>}
          </div>
        ))}
      </div>

      {/* Provider breakdown */}
      <div className="bg-gray-800 rounded-xl p-6">
        <h2 className="text-lg font-semibold mb-4">Spend by Provider</h2>
        {!data?.by_provider.length ? (
          <p className="text-gray-400 text-sm">No provider data.</p>
        ) : (
          <div className="space-y-4">
            {data.by_provider.map((p) => (
              <div key={p.provider}>
                <div className="flex items-center justify-between mb-1">
                  <div className="flex items-center gap-3">
                    <span className="font-medium capitalize text-gray-200">{p.provider}</span>
                    <span className="text-xs text-gray-400">
                      {p.request_count.toLocaleString()} requests
                    </span>
                    {p.mom_growth_percent !== 0 && (
                      <span
                        className={`text-xs font-medium ${
                          p.mom_growth_percent > 0 ? "text-red-400" : "text-green-400"
                        }`}
                      >
                        {p.mom_growth_percent > 0 ? "+" : ""}
                        {p.mom_growth_percent.toFixed(1)}% MoM
                      </span>
                    )}
                  </div>
                  <div className="text-right">
                    <span className="font-semibold text-gray-100">${p.total_usd.toFixed(2)}</span>
                    <span className="text-xs text-gray-400 ml-2">({p.share_percent.toFixed(1)}%)</span>
                  </div>
                </div>
                <div className="w-full bg-gray-700 rounded-full h-2">
                  <div
                    className="bg-blue-500 h-2 rounded-full"
                    style={{ width: `${p.share_percent}%` }}
                  />
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Top models */}
      <div className="bg-gray-800 rounded-xl p-6">
        <h2 className="text-lg font-semibold mb-4">Top Models by Spend</h2>
        {!data?.top_models.length ? (
          <p className="text-gray-400 text-sm">No model data.</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm text-left">
              <thead>
                <tr className="text-gray-400 border-b border-gray-700">
                  <th className="pb-2 pr-4">Model</th>
                  <th className="pb-2 pr-4">Provider</th>
                  <th className="pb-2 pr-4 text-right">Requests</th>
                  <th className="pb-2 text-right">Total USD</th>
                </tr>
              </thead>
              <tbody>
                {data.top_models.map((m) => (
                  <tr key={m.model} className="border-b border-gray-700 last:border-0">
                    <td className="py-2 pr-4 font-mono text-xs text-gray-200">{m.model}</td>
                    <td className="py-2 pr-4 text-gray-400 capitalize">{m.provider}</td>
                    <td className="py-2 pr-4 text-right text-gray-300">
                      {m.request_count.toLocaleString()}
                    </td>
                    <td className="py-2 text-right font-semibold text-gray-100">
                      ${m.total_usd.toFixed(2)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* Budget Plan */}
      {budgetPlan && (
        <div className="bg-gray-800 rounded-xl p-6">
          <h2 className="text-lg font-semibold mb-1">
            AI Budget Plan — {budgetPlan.plan_year}
          </h2>
          <p className="text-sm text-gray-400 mb-4">
            Projected spend based on {budgetPlan.growth_rate_used_percent >= 0 ? "+" : ""}
            {budgetPlan.growth_rate_used_percent.toFixed(1)}%/month growth trend.
            Total annual estimate:{" "}
            <span className="text-gray-100 font-semibold">
              ${budgetPlan.total_annual_usd.toFixed(2)}
            </span>
          </p>

          <div className="grid grid-cols-2 md:grid-cols-4 gap-3 mb-6">
            {budgetPlan.quarterly.map((q) => (
              <div key={q.quarter} className="bg-gray-700 rounded-lg p-3 text-center">
                <p className="text-xs text-gray-400 mb-1">{q.quarter}</p>
                <p className="font-bold text-gray-100">${q.usd.toFixed(0)}</p>
              </div>
            ))}
          </div>

          {budgetPlan.by_department.length > 0 && (
            <>
              <h3 className="text-sm font-semibold text-gray-300 mb-3">By Department (monthly)</h3>
              <div className="overflow-x-auto">
                <table className="w-full text-sm text-left">
                  <thead>
                    <tr className="text-gray-400 border-b border-gray-700">
                      <th className="pb-2 pr-4">Department</th>
                      <th className="pb-2 pr-4 text-right">Monthly</th>
                      <th className="pb-2 text-right">Annual</th>
                    </tr>
                  </thead>
                  <tbody>
                    {budgetPlan.by_department.map((d) => (
                      <tr key={d.department} className="border-b border-gray-700 last:border-0">
                        <td className="py-2 pr-4 text-gray-200">{d.department}</td>
                        <td className="py-2 pr-4 text-right text-gray-300">${d.monthly_usd.toFixed(2)}</td>
                        <td className="py-2 text-right font-semibold text-gray-100">${d.annual_usd.toFixed(2)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </>
          )}
        </div>
      )}

      <p className="text-xs text-gray-500">
        Procurement data generated {data?.generated_at ? new Date(data.generated_at).toLocaleString() : "—"}
      </p>
    </div>
  );
}
