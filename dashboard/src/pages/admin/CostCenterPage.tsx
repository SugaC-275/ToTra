import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { apiClient } from '../../api/client'

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
    </div>
  )
}
