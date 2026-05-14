import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
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

interface CostSettings {
  tenant_id: string
  monthly_budget_usd: number | null
  work_start_hour: number
  work_end_hour: number
  updated_at?: string
}

interface BudgetStatus {
  tenant_id: string
  year_month: string
  spent_usd: number
  monthly_budget_usd: number | null
  used_percent: number
  is_over_budget: boolean
  generated_at: string
}

interface HourlySpend {
  hour: number
  request_count: number
  spent_usd: number
  is_work_hour: boolean
}

interface OffHoursReport {
  tenant_id: string
  year_month: string
  work_start_hour: number
  work_end_hour: number
  total_usd: number
  off_hours_usd: number
  off_hours_percent: number
  hourly_breakdown: HourlySpend[]
  generated_at: string
}

interface SpenderEntry {
  user_id: string
  email: string
  current_usd: number
  previous_usd: number
  change_usd: number
}

interface TopSpendersReport {
  year_month: string
  entries: SpenderEntry[]
}

function TopSpendersTable({ entries }: { entries: SpenderEntry[] }) {
  if (!entries || entries.length === 0)
    return <p className="text-gray-400 text-sm">No spend data this month.</p>;

  return (
    <div className="overflow-x-auto">
      <table className="min-w-full text-sm">
        <thead>
          <tr className="text-left text-gray-400 border-b border-gray-700">
            <th className="pb-2 pr-4">User</th>
            <th className="pb-2 pr-4 text-right">This Month</th>
            <th className="pb-2 pr-4 text-right">Last Month</th>
            <th className="pb-2 text-right">Change</th>
          </tr>
        </thead>
        <tbody>
          {entries.map((e) => (
            <tr key={e.user_id} className="border-b border-gray-800">
              <td className="py-2 pr-4 font-mono text-xs text-gray-200">{e.email}</td>
              <td className="py-2 pr-4 text-right text-gray-100">
                ${e.current_usd.toFixed(2)}
              </td>
              <td className="py-2 pr-4 text-right text-gray-400">
                ${e.previous_usd.toFixed(2)}
              </td>
              <td
                className={`py-2 text-right font-semibold ${
                  e.change_usd > 0
                    ? "text-red-400"
                    : e.change_usd < 0
                    ? "text-green-400"
                    : "text-gray-400"
                }`}
              >
                {e.change_usd > 0 ? "+" : ""}
                {e.change_usd.toFixed(2)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function BudgetSettingsForm({ settings, onSaved }: { settings: CostSettings | undefined; onSaved: () => void }) {
  const queryClient = useQueryClient()
  const [budgetInput, setBudgetInput] = useState<string>(
    settings?.monthly_budget_usd != null ? String(settings.monthly_budget_usd) : ''
  )
  const [startHour, setStartHour] = useState<number>(settings?.work_start_hour ?? 9)
  const [endHour, setEndHour] = useState<number>(settings?.work_end_hour ?? 18)

  const mutation = useMutation({
    mutationFn: (payload: { monthly_budget_usd: number | null; work_start_hour: number; work_end_hour: number }) =>
      apiClient.put('/api/admin/cost/settings', payload).then(r => r.data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['cost-settings'] })
      queryClient.invalidateQueries({ queryKey: ['cost-budget-status'] })
      queryClient.invalidateQueries({ queryKey: ['cost-off-hours'] })
      onSaved()
    },
  })

  const handleSave = () => {
    const monthly_budget_usd = budgetInput.trim() === '' ? null : parseFloat(budgetInput)
    mutation.mutate({ monthly_budget_usd, work_start_hour: startHour, work_end_hour: endHour })
  }

  const hourOptions = Array.from({ length: 24 }, (_, i) => i)

  return (
    <div className="space-y-4">
      <div>
        <label className="block text-sm font-medium text-gray-700 mb-1">Monthly Budget (USD)</label>
        <input
          type="number"
          min="0"
          step="0.01"
          placeholder="No limit"
          value={budgetInput}
          onChange={e => setBudgetInput(e.target.value)}
          className="border rounded px-3 py-1.5 text-sm w-48"
        />
      </div>
      <div className="flex gap-4">
        <div>
          <label className="block text-sm font-medium text-gray-700 mb-1">Work Start Hour (UTC)</label>
          <select
            value={startHour}
            onChange={e => setStartHour(Number(e.target.value))}
            className="border rounded px-3 py-1.5 text-sm"
          >
            {hourOptions.map(h => (
              <option key={h} value={h}>{h}:00</option>
            ))}
          </select>
        </div>
        <div>
          <label className="block text-sm font-medium text-gray-700 mb-1">Work End Hour (UTC)</label>
          <select
            value={endHour}
            onChange={e => setEndHour(Number(e.target.value))}
            className="border rounded px-3 py-1.5 text-sm"
          >
            {hourOptions.map(h => (
              <option key={h} value={h}>{h}:00</option>
            ))}
          </select>
        </div>
      </div>
      {mutation.isError && (
        <p className="text-sm text-red-500">Save failed: {(mutation.error as Error).message}</p>
      )}
      <button
        onClick={handleSave}
        disabled={mutation.isPending}
        className="px-4 py-2 bg-blue-600 text-white text-sm rounded hover:bg-blue-700 disabled:opacity-50"
      >
        {mutation.isPending ? 'Saving...' : 'Save Settings'}
      </button>
    </div>
  )
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

  const { data: settings } = useQuery<CostSettings>({
    queryKey: ['cost-settings'],
    queryFn: () => apiClient.get('/api/admin/cost/settings').then(r => r.data),
  })

  const { data: budgetStatus, isLoading: budgetStatusLoading } = useQuery<BudgetStatus>({
    queryKey: ['cost-budget-status'],
    queryFn: () => apiClient.get('/api/admin/cost/budget-status').then(r => r.data),
  })

  const { data: offHoursReport, isLoading: offHoursLoading } = useQuery<OffHoursReport>({
    queryKey: ['cost-off-hours'],
    queryFn: () => apiClient.get(`/api/admin/cost/off-hours?month=${month}`).then(r => r.data),
  })

  const { data: topSpenders } = useQuery<TopSpendersReport>({
    queryKey: ["topSpenders"],
    queryFn: () =>
      apiClient
        .get<TopSpendersReport>("/api/admin/cost/top-spenders")
        .then((r) => r.data),
  })

  const alertCheckMutation = useMutation({
    mutationFn: () =>
      apiClient
        .post("/api/admin/cost/budget-alert/check")
        .then((r) => r.data),
  })

  const [settingsSaved, setSettingsSaved] = useState(false)

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

      {/* Budget Status */}
      <div className="bg-white border rounded-xl p-4">
        <h2 className="font-semibold mb-3">预算使用状态</h2>
        {budgetStatusLoading ? (
          <p className="text-sm text-gray-400">Loading...</p>
        ) : budgetStatus ? (
          budgetStatus.monthly_budget_usd == null ? (
            <p className="text-sm text-gray-400">No budget configured.</p>
          ) : (
            <div className="space-y-3">
              <div className="flex items-center gap-4">
                <div>
                  <p className="text-xs text-gray-500">本月支出</p>
                  <p className="text-2xl font-bold">${budgetStatus.spent_usd.toFixed(2)}</p>
                </div>
                <div className="text-gray-400">/</div>
                <div>
                  <p className="text-xs text-gray-500">月度预算</p>
                  <p className="text-2xl font-bold">${budgetStatus.monthly_budget_usd.toFixed(2)}</p>
                </div>
                {budgetStatus.is_over_budget && (
                  <span className="px-2 py-1 bg-red-100 text-red-700 text-xs font-semibold rounded-full">
                    Over budget!
                  </span>
                )}
              </div>
              <div className="w-full bg-gray-100 rounded-full h-3">
                <div
                  className={`h-3 rounded-full transition-all ${
                    budgetStatus.used_percent > 100
                      ? 'bg-red-500'
                      : budgetStatus.used_percent >= 80
                      ? 'bg-yellow-400'
                      : 'bg-green-500'
                  }`}
                  style={{ width: `${Math.min(budgetStatus.used_percent, 100)}%` }}
                />
              </div>
              <p className="text-xs text-gray-500">
                已使用 {budgetStatus.used_percent.toFixed(1)}% · {budgetStatus.year_month}
              </p>
            </div>
          )
        ) : (
          <p className="text-sm text-gray-400">No budget data available.</p>
        )}
      </div>

      {/* Budget Settings Form */}
      <div className="bg-white border rounded-xl p-4">
        <h2 className="font-semibold mb-3">预算与工作时段设置</h2>
        {settingsSaved && (
          <div className="mb-3 px-3 py-2 bg-green-50 border border-green-200 rounded text-sm text-green-700">
            Settings saved successfully.
          </div>
        )}
        <BudgetSettingsForm settings={settings} onSaved={() => setSettingsSaved(true)} />
      </div>

      {/* Off-Hours Waste */}
      <div className="bg-white border rounded-xl p-4">
        <h2 className="font-semibold mb-3">非工作时段 AI 用量</h2>
        {offHoursLoading ? (
          <p className="text-sm text-gray-400">Loading...</p>
        ) : offHoursReport ? (
          offHoursReport.total_usd === 0 ? (
            <p className="text-sm text-gray-400">No usage data for this period.</p>
          ) : (
            <div className="space-y-3">
              <div className="grid grid-cols-3 gap-4">
                <div className="bg-gray-50 rounded-lg p-3">
                  <p className="text-xs text-gray-500">Total Spend</p>
                  <p className="text-xl font-bold">${offHoursReport.total_usd.toFixed(2)}</p>
                </div>
                <div className="bg-orange-50 rounded-lg p-3">
                  <p className="text-xs text-orange-600">Off-Hours Spend</p>
                  <p className="text-xl font-bold text-orange-600">${offHoursReport.off_hours_usd.toFixed(2)}</p>
                </div>
                <div className="bg-orange-50 rounded-lg p-3">
                  <p className="text-xs text-orange-600">Off-Hours %</p>
                  <p className="text-xl font-bold text-orange-600">{offHoursReport.off_hours_percent.toFixed(1)}%</p>
                </div>
              </div>
              <p className="text-xs text-gray-400">
                Work hours: {offHoursReport.work_start_hour}:00–{offHoursReport.work_end_hour}:00 UTC · {offHoursReport.year_month}
              </p>
            </div>
          )
        ) : (
          <p className="text-sm text-gray-400">No off-hours data available.</p>
        )}
      </div>

      {/* Top Spenders */}
      <div className="bg-gray-800 rounded-xl p-6">
        <h2 className="text-lg font-semibold mb-4">
          Top Spenders — {topSpenders?.year_month ?? "…"}
        </h2>
        <TopSpendersTable entries={topSpenders?.entries ?? []} />
      </div>

      {/* Budget Alerts */}
      <div className="bg-gray-800 rounded-xl p-6">
        <h2 className="text-lg font-semibold mb-4">Budget Alerts</h2>
        <p className="text-sm text-gray-400 mb-4">
          Check current month spend against thresholds (50%, 80%, 100%) and send bot
          notifications for newly crossed thresholds.
        </p>
        <button
          onClick={() => alertCheckMutation.mutate()}
          disabled={alertCheckMutation.isPending}
          className="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 rounded-lg text-sm font-medium"
        >
          {alertCheckMutation.isPending ? "Checking…" : "Check Budget Alerts"}
        </button>
        {alertCheckMutation.isSuccess && (
          <p className="mt-3 text-green-400 text-sm">Done — alerts sent for any newly crossed thresholds.</p>
        )}
        {alertCheckMutation.isError && (
          <p className="mt-3 text-red-400 text-sm">Error checking alerts.</p>
        )}
      </div>
    </div>
  )
}
