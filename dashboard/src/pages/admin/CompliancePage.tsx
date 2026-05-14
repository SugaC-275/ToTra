import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { apiClient } from '../../api/client'

interface Violation {
  id: number
  user_name: string
  user_email: string
  department: string
  pii_type: string
  action: string
  occurred_at: string
}

interface RiskScore {
  user_name: string
  user_email: string
  department: string
  violation_count: number
  risk_score: number
  risk_level: 'low' | 'medium' | 'high' | 'critical'
}

interface ComplianceReport {
  year_month: string
  total_violations: number
  unique_users: number
  top_pii_types: { pii_type: string; count: number }[]
  high_risk_users: RiskScore[]
}

const riskColors: Record<string, string> = {
  low: 'bg-green-100 text-green-800',
  medium: 'bg-yellow-100 text-yellow-800',
  high: 'bg-orange-100 text-orange-800',
  critical: 'bg-red-100 text-red-800',
}

export default function CompliancePage() {
  const [month, setMonth] = useState(() => new Date().toISOString().slice(0, 7))

  const { data: report } = useQuery<ComplianceReport>({
    queryKey: ['compliance-report', month],
    queryFn: () => apiClient.get(`/api/admin/compliance/report?month=${month}`).then(r => r.data),
  })

  const { data: violations = [] } = useQuery<Violation[]>({
    queryKey: ['compliance-violations', month],
    queryFn: () => apiClient.get(`/api/admin/compliance/violations?month=${month}&limit=100`).then(r => r.data),
  })

  const { data: riskScores = [] } = useQuery<RiskScore[]>({
    queryKey: ['compliance-risk', month],
    queryFn: () => apiClient.get(`/api/admin/compliance/risk-scores?month=${month}`).then(r => r.data),
  })

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">合规中心</h1>
        <input
          type="month"
          value={month}
          onChange={e => setMonth(e.target.value)}
          className="border rounded px-3 py-1 text-sm"
        />
      </div>

      <div className="grid grid-cols-3 gap-4">
        <div className="bg-white border rounded-xl p-4">
          <p className="text-sm text-gray-500">本月违规次数</p>
          <p className="text-3xl font-bold text-red-600">{report?.total_violations ?? 0}</p>
        </div>
        <div className="bg-white border rounded-xl p-4">
          <p className="text-sm text-gray-500">涉及员工数</p>
          <p className="text-3xl font-bold text-orange-500">{report?.unique_users ?? 0}</p>
        </div>
        <div className="bg-white border rounded-xl p-4">
          <p className="text-sm text-gray-500">高风险用户</p>
          <p className="text-3xl font-bold text-purple-600">{report?.high_risk_users?.length ?? 0}</p>
        </div>
      </div>

      <div className="bg-white border rounded-xl p-4">
        <h2 className="font-semibold mb-3">员工风险评分</h2>
        <table className="w-full text-sm">
          <thead className="bg-gray-50">
            <tr>
              {['姓名', '邮箱', '部门', '违规次数', '风险分', '风险等级'].map(h => (
                <th key={h} className="px-3 py-2 text-left font-medium text-gray-600">{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {riskScores.map((u, i) => (
              <tr key={i} className="border-t">
                <td className="px-3 py-2">{u.user_name}</td>
                <td className="px-3 py-2 text-gray-500">{u.user_email}</td>
                <td className="px-3 py-2">{u.department}</td>
                <td className="px-3 py-2">{u.violation_count}</td>
                <td className="px-3 py-2 font-mono">{u.risk_score}</td>
                <td className="px-3 py-2">
                  <span className={`px-2 py-0.5 rounded-full text-xs font-medium ${riskColors[u.risk_level]}`}>
                    {u.risk_level}
                  </span>
                </td>
              </tr>
            ))}
            {riskScores.length === 0 && (
              <tr>
                <td colSpan={6} className="px-3 py-4 text-center text-gray-400">本月无违规记录</td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      <div className="bg-white border rounded-xl p-4">
        <h2 className="font-semibold mb-3">违规事件明细</h2>
        <table className="w-full text-sm">
          <thead className="bg-gray-50">
            <tr>
              {['时间', '姓名', '部门', 'PII 类型', '处理方式'].map(h => (
                <th key={h} className="px-3 py-2 text-left font-medium text-gray-600">{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {violations.map(v => (
              <tr key={v.id} className="border-t">
                <td className="px-3 py-2 text-gray-500 font-mono text-xs">{v.occurred_at}</td>
                <td className="px-3 py-2">{v.user_name}</td>
                <td className="px-3 py-2">{v.department}</td>
                <td className="px-3 py-2">
                  <span className="bg-red-50 text-red-700 px-2 py-0.5 rounded text-xs">{v.pii_type}</span>
                </td>
                <td className="px-3 py-2">{v.action}</td>
              </tr>
            ))}
            {violations.length === 0 && (
              <tr>
                <td colSpan={5} className="px-3 py-4 text-center text-gray-400">本月无违规记录</td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}
