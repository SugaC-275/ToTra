import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { apiClient } from '../../api/client'

interface EOYMonthlySpend {
  month: string
  total_usd: number
}

interface EOYBudgetForecast {
  tenant_id: string
  history: EOYMonthlySpend[]
  forecasted_eoy_usd: number
  generated_at: string
}

interface CostBenchmark {
  year_month: string
  tenant_per_user_usd: number
  p25: number
  p50: number
  p75: number
  tenant_count: number
  percentile_rank: number
  insufficient_data: boolean
}

interface ModelROI {
  model: string
  total_requests: number
  usd_per_1k_requests: number
}

interface ModelCostItem {
  model: string
  model_tier: 'cheap' | 'standard' | 'premium'
  total_usd: number
  total_scu: number
  request_count: number
}

interface DeptCostItem {
  department: string
  total_usd: number
  request_count: number
  user_count: number
}

interface CostBreakdown {
  total_usd: number
  total_scu: number
  by_model: ModelCostItem[]
  by_department: DeptCostItem[]
}

interface WasteItem {
  waste_type: string
  description: string
  estimated_savings_usd: number
  affected_requests: number
}

interface Suggestion {
  priority: 'high' | 'medium' | 'low'
  action: string
  detail: string
  savings_usd: number
}

const tierColors: Record<string, string> = {
  cheap: 'bg-green-100 text-green-700',
  standard: 'bg-blue-100 text-blue-700',
  premium: 'bg-purple-100 text-purple-700',
}

const priorityColors: Record<string, string> = {
  high: 'bg-red-100 text-red-700',
  medium: 'bg-yellow-100 text-yellow-700',
  low: 'bg-gray-100 text-gray-700',
}

export default function CostCenterPage() {
  const [month, setMonth] = useState(() => new Date().toISOString().slice(0, 7))

  const { data: breakdown } = useQuery<CostBreakdown>({
    queryKey: ['cost-breakdown', month],
    queryFn: () => apiClient.get(`/api/admin/cost/breakdown?month=${month}`).then(r => r.data),
  })

  const { data: waste = [] } = useQuery<WasteItem[]>({
    queryKey: ['cost-waste', month],
    queryFn: () => apiClient.get(`/api/admin/cost/waste?month=${month}`).then(r => r.data),
  })

  const { data: suggestions = [] } = useQuery<Suggestion[]>({
    queryKey: ['cost-suggestions', month],
    queryFn: () => apiClient.get(`/api/admin/cost/suggestions?month=${month}`).then(r => r.data),
  })

  const { data: savings } = useQuery({
    queryKey: ["cost-savings", month],
    queryFn: () =>
      apiClient
        .get<{
          routing_event_count: number;
          routed_models: { original_model: string; routed_model: string; count: number }[];
          generated_at: string;
        }>(`/api/admin/cost/savings-report?month=${month}`)
        .then((r) => r.data),
  });

  const { data: budgetForecast, isLoading: budgetForecastLoading } = useQuery<EOYBudgetForecast>({
    queryKey: ['cost-budget-forecast'],
    queryFn: () => apiClient.get('/api/admin/cost/budget-forecast').then(r => r.data),
  })

  const { data: costBenchmark, isLoading: costBenchmarkLoading } = useQuery<CostBenchmark>({
    queryKey: ['cost-benchmark', month],
    queryFn: () => apiClient.get(`/api/admin/cost/benchmark?month=${month}`).then(r => r.data),
  })

  const { data: modelROI = [], isLoading: modelROILoading } = useQuery<ModelROI[]>({
    queryKey: ['cost-model-roi'],
    queryFn: () => apiClient.get('/api/admin/cost/model-roi').then(r => r.data),
  })

  const totalSavings = waste.reduce((sum, w) => sum + w.estimated_savings_usd, 0)

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">成本中心</h1>
        <input
          type="month"
          value={month}
          onChange={e => setMonth(e.target.value)}
          className="border rounded px-3 py-1 text-sm"
        />
      </div>

      {/* KPI cards */}
      <div className="grid grid-cols-3 gap-4">
        <div className="bg-white border rounded-xl p-4">
          <p className="text-sm text-gray-500">本月 AI 总支出</p>
          <p className="text-3xl font-bold">${breakdown?.total_usd?.toFixed(2) ?? '0.00'}</p>
        </div>
        <div className="bg-white border rounded-xl p-4">
          <p className="text-sm text-gray-500">可节省估算</p>
          <p className="text-3xl font-bold text-green-600">${totalSavings.toFixed(2)}</p>
        </div>
        <div className="bg-white border rounded-xl p-4">
          <p className="text-sm text-gray-500">优化建议数</p>
          <p className="text-3xl font-bold text-blue-600">{suggestions.length}</p>
        </div>
      </div>

      {/* Optimization suggestions */}
      {suggestions.length > 0 && (
        <div className="bg-white border rounded-xl p-4">
          <h2 className="font-semibold mb-3">优化建议</h2>
          <div className="space-y-3">
            {suggestions.map((s, i) => (
              <div key={i} className="flex items-start gap-3 p-3 bg-gray-50 rounded-lg">
                <span className={`px-2 py-0.5 rounded text-xs font-medium shrink-0 ${priorityColors[s.priority]}`}>
                  {s.priority}
                </span>
                <div className="flex-1">
                  <p className="font-medium text-sm">{s.action}</p>
                  <p className="text-xs text-gray-500 mt-0.5">{s.detail}</p>
                </div>
                <span className="text-green-600 font-mono text-sm shrink-0">
                  节省 ${s.savings_usd.toFixed(2)}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* By model table */}
      <div className="bg-white border rounded-xl p-4">
        <h2 className="font-semibold mb-3">模型成本分布</h2>
        <table className="w-full text-sm">
          <thead className="bg-gray-50">
            <tr>
              {['模型', '级别', '支出 (USD)', 'SCU', '请求数'].map(h => (
                <th key={h} className="px-3 py-2 text-left font-medium text-gray-600">{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {(breakdown?.by_model ?? []).map((m, i) => (
              <tr key={i} className="border-t">
                <td className="px-3 py-2 font-mono text-xs">{m.model}</td>
                <td className="px-3 py-2">
                  <span className={`px-2 py-0.5 rounded text-xs ${tierColors[m.model_tier]}`}>
                    {m.model_tier}
                  </span>
                </td>
                <td className="px-3 py-2 font-mono">${m.total_usd.toFixed(3)}</td>
                <td className="px-3 py-2 font-mono">{m.total_scu.toFixed(1)}</td>
                <td className="px-3 py-2">{m.request_count.toLocaleString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* By department table */}
      <div className="bg-white border rounded-xl p-4">
        <h2 className="font-semibold mb-3">部门成本分布</h2>
        <table className="w-full text-sm">
          <thead className="bg-gray-50">
            <tr>
              {['部门', '支出 (USD)', '请求数', '员工数'].map(h => (
                <th key={h} className="px-3 py-2 text-left font-medium text-gray-600">{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {(breakdown?.by_department ?? []).map((d, i) => (
              <tr key={i} className="border-t">
                <td className="px-3 py-2">{d.department}</td>
                <td className="px-3 py-2 font-mono">${d.total_usd.toFixed(3)}</td>
                <td className="px-3 py-2">{d.request_count.toLocaleString()}</td>
                <td className="px-3 py-2">{d.user_count}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Waste detection */}
      {waste.length > 0 && (
        <div className="bg-white border rounded-xl p-4">
          <h2 className="font-semibold mb-3">浪费检测</h2>
          <div className="space-y-2">
            {waste.map((w, i) => (
              <div
                key={i}
                className="flex items-center justify-between p-3 bg-orange-50 border border-orange-100 rounded-lg"
              >
                <div>
                  <p className="text-sm font-medium">{w.description}</p>
                  <p className="text-xs text-gray-500">影响请求数: {w.affected_requests.toLocaleString()}</p>
                </div>
                <span className="text-orange-600 font-mono font-medium text-sm">
                  浪费 ${w.estimated_savings_usd.toFixed(2)}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Auto-Routing Savings */}
      <div className="mt-8">
        <h2 className="text-lg font-semibold mb-4">Auto-Routing Events This Month</h2>
        <div className="mb-4 px-4 py-3 bg-green-50 border border-green-100 rounded-lg text-sm text-green-700">
          <span className="font-semibold">{savings?.routing_event_count ?? 0}</span> requests
          automatically downgraded to a cheaper model
        </div>
        {savings && savings.routed_models.length > 0 && (
          <div className="overflow-x-auto">
            <table className="w-full text-sm border rounded-lg overflow-hidden">
              <thead className="bg-gray-50 text-gray-500 text-xs uppercase">
                <tr>
                  <th className="px-4 py-2 text-left">Original Model</th>
                  <th className="px-4 py-2 text-left">Routed To</th>
                  <th className="px-4 py-2 text-right">Requests</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100">
                {savings.routed_models.map((row, i) => (
                  <tr key={i} className="hover:bg-gray-50">
                    <td className="px-4 py-2 font-mono text-xs">{row.original_model}</td>
                    <td className="px-4 py-2 font-mono text-xs text-green-600">{row.routed_model}</td>
                    <td className="px-4 py-2 text-right font-medium">{row.count}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {savings && savings.routed_models.length === 0 && (
          <p className="text-sm text-gray-400">No routing events recorded this month.</p>
        )}
      </div>

      {/* Budget Forecast (EOY Projection) */}
      <div className="bg-white border rounded-xl p-4">
        <h2 className="font-semibold mb-3">年度支出预测 (EOY Forecast)</h2>
        {budgetForecastLoading ? (
          <p className="text-sm text-gray-400">Loading...</p>
        ) : budgetForecast ? (
          <>
            <div className="mb-4">
              <p className="text-sm text-gray-500">预测全年总支出</p>
              <p className="text-3xl font-bold text-blue-600">
                ${budgetForecast.forecasted_eoy_usd.toFixed(2)}
              </p>
            </div>
            {budgetForecast.history.length > 0 ? (
              <table className="w-full text-sm">
                <thead className="bg-gray-50">
                  <tr>
                    {['月份', '支出 (USD)'].map(h => (
                      <th key={h} className="px-3 py-2 text-left font-medium text-gray-600">{h}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {budgetForecast.history.map((row, i) => (
                    <tr key={i} className="border-t">
                      <td className="px-3 py-2 font-mono text-xs">{row.month}</td>
                      <td className="px-3 py-2 font-mono">${row.total_usd.toFixed(2)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            ) : (
              <p className="text-sm text-gray-400">No spend history available.</p>
            )}
          </>
        ) : (
          <p className="text-sm text-gray-400">No forecast data available.</p>
        )}
      </div>

      {/* Cost Benchmark */}
      <div className="bg-white border rounded-xl p-4">
        <h2 className="font-semibold mb-3">成本基准对比 (Benchmark)</h2>
        {costBenchmarkLoading ? (
          <p className="text-sm text-gray-400">Loading...</p>
        ) : costBenchmark ? (
          costBenchmark.insufficient_data ? (
            <p className="text-sm text-gray-400">Insufficient tenant data for benchmark comparison.</p>
          ) : (
            <div className="space-y-3">
              <div className="grid grid-cols-2 gap-4">
                <div className="bg-gray-50 rounded-lg p-3">
                  <p className="text-xs text-gray-500">你的人均支出</p>
                  <p className="text-xl font-bold">${costBenchmark.tenant_per_user_usd.toFixed(2)}</p>
                </div>
                <div className="bg-gray-50 rounded-lg p-3">
                  <p className="text-xs text-gray-500">百分位排名</p>
                  <p className={`text-xl font-bold ${costBenchmark.percentile_rank > 75 ? 'text-red-600' : 'text-green-600'}`}>
                    {costBenchmark.percentile_rank.toFixed(1)}%
                  </p>
                  {costBenchmark.percentile_rank > 75 && (
                    <p className="text-xs text-red-500 mt-1">高于 75% 同行</p>
                  )}
                </div>
              </div>
              <div className="grid grid-cols-3 gap-3 text-center text-sm">
                <div className="bg-green-50 rounded-lg p-2">
                  <p className="text-xs text-gray-500">P25</p>
                  <p className="font-mono font-medium">${costBenchmark.p25.toFixed(2)}</p>
                </div>
                <div className="bg-blue-50 rounded-lg p-2">
                  <p className="text-xs text-gray-500">P50 (中位)</p>
                  <p className="font-mono font-medium">${costBenchmark.p50.toFixed(2)}</p>
                </div>
                <div className="bg-orange-50 rounded-lg p-2">
                  <p className="text-xs text-gray-500">P75</p>
                  <p className="font-mono font-medium">${costBenchmark.p75.toFixed(2)}</p>
                </div>
              </div>
              <p className="text-xs text-gray-400">基于 {costBenchmark.tenant_count} 个租户数据 · {costBenchmark.year_month}</p>
            </div>
          )
        ) : (
          <p className="text-sm text-gray-400">No benchmark data available.</p>
        )}
      </div>

      {/* Model ROI */}
      <div className="bg-white border rounded-xl p-4">
        <h2 className="font-semibold mb-3">模型 ROI 排名 (过去 30 天)</h2>
        {modelROILoading ? (
          <p className="text-sm text-gray-400">Loading...</p>
        ) : modelROI.length === 0 ? (
          <p className="text-sm text-gray-400">No models with ≥100 requests in last 30 days.</p>
        ) : (
          <table className="w-full text-sm">
            <thead className="bg-gray-50">
              <tr>
                {['模型', '请求总数', '每千次请求 USD'].map(h => (
                  <th key={h} className="px-3 py-2 text-left font-medium text-gray-600">{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {modelROI.map((row, i) => (
                <tr key={i} className="border-t">
                  <td className="px-3 py-2 font-mono text-xs">{row.model}</td>
                  <td className="px-3 py-2">{row.total_requests.toLocaleString()}</td>
                  <td className="px-3 py-2 font-mono">${row.usd_per_1k_requests.toFixed(4)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}
