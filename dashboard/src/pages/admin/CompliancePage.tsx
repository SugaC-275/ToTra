import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { apiClient } from '../../api/client'

interface RiskPoint {
  month: string;
  violations: number;
  requests: number;
  rate_per_1k: number;
  risk_score: number;
}

interface PIITypeStat {
  pii_type: string;
  count: number;
}

interface UserViolationStat {
  user_id: string;
  user_name: string;
  user_email: string;
  department: string;
  count: number;
}

interface ComplianceDigest {
  tenant_id: string;
  period: string;
  total_violations: number;
  total_requests: number;
  block_rate_per_1k: number;
  by_pii_type: PIITypeStat[];
  top_violators: UserViolationStat[];
}

interface ComplianceBenchmark {
  your_rate: number
  p25: number
  p50: number
  p75: number
  tenant_count: number
  percentile_rank: number
  insufficient_data: boolean
}

function BenchmarkCard() {
  const { data, isLoading } = useQuery<ComplianceBenchmark>({
    queryKey: ['compliance-benchmark'],
    queryFn: () => apiClient.get('/api/admin/compliance/benchmark').then(r => r.data),
  })

  if (isLoading) return <div className="bg-white border rounded-xl p-4 text-gray-400 text-sm">Loading benchmark…</div>

  if (!data) return null

  if (data.insufficient_data) {
    return (
      <div className="bg-white border rounded-xl p-4">
        <h2 className="font-semibold mb-2">Industry Benchmark</h2>
        <p className="text-sm text-gray-400">
          Insufficient data — need at least 3 tenants to compute benchmarks.
          ({data.tenant_count} tenant{data.tenant_count === 1 ? '' : 's'} active)
        </p>
      </div>
    )
  }

  return (
    <div className="bg-white border rounded-xl p-4">
      <h2 className="font-semibold mb-3">Industry Benchmark</h2>
      <p className="text-sm text-gray-500 mb-4">
        Violation rate (per 1,000 requests) across {data.tenant_count} tenants — last 30 days
      </p>
      <div className="grid grid-cols-4 gap-4 mb-4">
        <div className="bg-gray-50 rounded-lg p-3 text-center">
          <p className="text-xs text-gray-500 mb-1">Your Rate</p>
          <p className="text-xl font-bold text-indigo-600">{data.your_rate.toFixed(2)}</p>
        </div>
        <div className="bg-gray-50 rounded-lg p-3 text-center">
          <p className="text-xs text-gray-500 mb-1">P25</p>
          <p className="text-xl font-bold text-green-600">{data.p25.toFixed(2)}</p>
        </div>
        <div className="bg-gray-50 rounded-lg p-3 text-center">
          <p className="text-xs text-gray-500 mb-1">Median (P50)</p>
          <p className="text-xl font-bold text-yellow-600">{data.p50.toFixed(2)}</p>
        </div>
        <div className="bg-gray-50 rounded-lg p-3 text-center">
          <p className="text-xs text-gray-500 mb-1">P75</p>
          <p className="text-xl font-bold text-red-600">{data.p75.toFixed(2)}</p>
        </div>
      </div>
      <div className="flex items-center gap-2">
        <span className="text-sm text-gray-600">Your percentile rank:</span>
        <span
          className={`px-2 py-0.5 rounded-full text-xs font-medium ${
            data.percentile_rank <= 25
              ? 'bg-green-100 text-green-800'
              : data.percentile_rank <= 75
              ? 'bg-yellow-100 text-yellow-800'
              : 'bg-red-100 text-red-800'
          }`}
        >
          {data.percentile_rank.toFixed(1)}th percentile
        </span>
        <span className="text-xs text-gray-400">
          (lower = fewer violations than peers)
        </span>
      </div>
    </div>
  )
}

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

  const { data: riskTrend } = useQuery({
    queryKey: ["compliance-risk-trend"],
    queryFn: (): Promise<{ points: RiskPoint[]; tenant_id: string }> =>
      apiClient
        .get("/api/admin/compliance/risk-trend")
        .then((r) => r.data),
  });

  const { data: digest } = useQuery({
    queryKey: ["compliance-digest"],
    queryFn: (): Promise<ComplianceDigest> =>
      apiClient
        .get("/api/admin/compliance/digest")
        .then((r) => r.data),
  });

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">合规中心</h1>
        <div className="flex items-center gap-2">
          <input
            type="month"
            value={month}
            onChange={e => setMonth(e.target.value)}
            className="border rounded px-3 py-1 text-sm"
          />
          <a
            href={`/api/admin/compliance/audit-export?month=${month}`}
            download
            className="px-3 py-1.5 text-sm bg-gray-100 hover:bg-gray-200 rounded-lg font-medium"
          >
            Export CSV
          </a>
          <button
            onClick={() =>
              apiClient.get("/api/admin/compliance/soc2-report").then((r) => {
                const blob = new Blob([JSON.stringify(r.data, null, 2)], { type: "application/json" });
                const url = URL.createObjectURL(blob);
                const a = document.createElement("a");
                a.href = url;
                a.download = `soc2-report-${month}.json`;
                a.click();
                URL.revokeObjectURL(url);
              })
            }
            className="px-3 py-1.5 text-sm bg-blue-100 hover:bg-blue-200 text-blue-700 rounded-lg font-medium"
          >
            SOC 2 Report
          </button>
        </div>
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

      <BenchmarkCard />

      {/* Risk Score Trend */}
      <div className="mt-8 border rounded-lg p-6">
        <h2 className="text-lg font-semibold mb-4">Risk Score Trend (Last 6 Months)</h2>
        {!riskTrend || riskTrend.points.length === 0 ? (
          <p className="text-gray-400 text-sm">No historical data yet.</p>
        ) : (
          <div>
            <div className="flex items-end gap-2 h-24 mb-3">
              {riskTrend.points.map((p) => {
                const height = Math.max(p.risk_score, 2);
                const color =
                  p.risk_score >= 70
                    ? "bg-red-500"
                    : p.risk_score >= 40
                    ? "bg-yellow-400"
                    : "bg-green-500";
                return (
                  <div key={p.month} className="flex-1 flex flex-col items-center gap-1">
                    <span className="text-xs text-gray-500">{p.risk_score.toFixed(0)}</span>
                    <div
                      className={`w-full rounded-t ${color}`}
                      style={{ height: `${height}%` }}
                      title={`${p.month}: ${p.violations} violations / ${p.requests} requests`}
                    />
                  </div>
                );
              })}
            </div>
            <div className="flex gap-2">
              {riskTrend.points.map((p) => (
                <div key={p.month} className="flex-1 text-center text-xs text-gray-400">
                  {p.month.slice(5)}
                </div>
              ))}
            </div>
            <div className="flex gap-4 mt-3 text-xs text-gray-500">
              <span className="flex items-center gap-1">
                <span className="w-2 h-2 rounded-full bg-green-500 inline-block" /> Low (&lt;40)
              </span>
              <span className="flex items-center gap-1">
                <span className="w-2 h-2 rounded-full bg-yellow-400 inline-block" /> Medium (40–70)
              </span>
              <span className="flex items-center gap-1">
                <span className="w-2 h-2 rounded-full bg-red-500 inline-block" /> High (&gt;70)
              </span>
            </div>
          </div>
        )}
      </div>

      {/* Compliance Digest */}
      <div className="mt-6 border rounded-lg p-6">
        <h2 className="text-lg font-semibold mb-4">
          Monthly Digest
          {digest && <span className="ml-2 text-sm font-normal text-gray-400">{digest.period}</span>}
        </h2>
        {!digest ? (
          <p className="text-gray-400 text-sm">Loading…</p>
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
            <div className="space-y-2">
              <div className="flex justify-between text-sm">
                <span className="text-gray-500">Total violations blocked</span>
                <span className="font-semibold">{digest.total_violations.toLocaleString()}</span>
              </div>
              <div className="flex justify-between text-sm">
                <span className="text-gray-500">Total AI requests</span>
                <span className="font-semibold">{digest.total_requests.toLocaleString()}</span>
              </div>
              <div className="flex justify-between text-sm">
                <span className="text-gray-500">Block rate</span>
                <span className="font-semibold">{digest.block_rate_per_1k.toFixed(2)} / 1K req</span>
              </div>
              {digest.by_pii_type.length > 0 && (
                <div className="mt-3">
                  <p className="text-xs text-gray-500 mb-2 font-medium uppercase tracking-wide">By PII Type</p>
                  {digest.by_pii_type.map((t) => (
                    <div key={t.pii_type} className="flex justify-between text-sm py-0.5">
                      <span className="text-gray-600 font-mono text-xs">{t.pii_type}</span>
                      <span>{t.count}</span>
                    </div>
                  ))}
                </div>
              )}
            </div>
            <div>
              <p className="text-xs text-gray-500 mb-2 font-medium uppercase tracking-wide">Top Violators</p>
              {digest.top_violators.length === 0 ? (
                <p className="text-sm text-gray-400">None this period</p>
              ) : (
                digest.top_violators.map((v, i) => (
                  <div key={v.user_id} className="flex items-center justify-between py-1.5 border-b last:border-0">
                    <div>
                      <span className="text-sm font-medium">{v.user_name || v.user_email || v.user_id}</span>
                      {v.department && (
                        <span className="ml-2 text-xs text-gray-400">{v.department}</span>
                      )}
                    </div>
                    <span className={`text-sm font-semibold ${i === 0 ? "text-red-600" : "text-gray-700"}`}>
                      {v.count}
                    </span>
                  </div>
                ))
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
