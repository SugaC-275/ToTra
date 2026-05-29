import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { apiClient, getComplianceReport, type ComplianceReportData } from '../../api/client'

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

function buildPrintHTML(report: ComplianceReportData): string {
  const statusColor = (s: string) => {
    if (s === 'enabled') return '#16a34a'
    if (s === 'disabled') return '#dc2626'
    return '#d97706'
  }
  const statusBg = (s: string) => {
    if (s === 'enabled') return '#dcfce7'
    if (s === 'disabled') return '#fee2e2'
    return '#fef9c3'
  }
  const fmt = (d: string) => new Date(d).toLocaleDateString()
  const rows = (items: { category: string; item: string; status: string; detail: string }[]) =>
    items.map(i => `
      <tr>
        <td>${i.category}</td>
        <td>${i.item}</td>
        <td style="text-align:center">
          <span style="background:${statusBg(i.status)};color:${statusColor(i.status)};padding:2px 8px;border-radius:4px;font-weight:600;font-size:12px">
            ${i.status.toUpperCase()}
          </span>
        </td>
        <td style="color:#6b7280;font-size:13px">${i.detail}</td>
      </tr>`).join('')

  return `<!DOCTYPE html><html><head><title>ToTra Compliance Report</title>
  <style>
    body{font-family:system-ui,sans-serif;color:#111;margin:40px;font-size:14px}
    h1{font-size:22px;margin-bottom:4px}
    .meta{color:#6b7280;font-size:13px;margin-bottom:24px}
    .stats{display:grid;grid-template-columns:repeat(5,1fr);gap:12px;margin-bottom:28px}
    .stat{border:1px solid #e5e7eb;border-radius:8px;padding:12px;text-align:center}
    .stat .val{font-size:24px;font-weight:700;color:#4f46e5}
    .stat .lbl{font-size:12px;color:#6b7280;margin-top:2px}
    h2{font-size:16px;margin:24px 0 8px;border-bottom:1px solid #e5e7eb;padding-bottom:4px}
    table{width:100%;border-collapse:collapse;font-size:13px;margin-bottom:16px}
    th{background:#f9fafb;text-align:left;padding:8px 10px;border-bottom:2px solid #e5e7eb;font-weight:600}
    td{padding:7px 10px;border-bottom:1px solid #f3f4f6}
    @media print{body{margin:20px}.no-print{display:none}}
    .footer{margin-top:32px;font-size:11px;color:#9ca3af;text-align:center}
  </style></head><body>
  <h1>ToTra Compliance Report</h1>
  <div class="meta">
    Tenant: <strong>${report.tenant_id}</strong> &nbsp;|&nbsp;
    Period: <strong>${fmt(report.period_start)}</strong> to <strong>${fmt(report.period_end)}</strong> &nbsp;|&nbsp;
    Generated: <strong>${fmt(report.generated_at)}</strong>
  </div>
  <div class="stats">
    <div class="stat"><div class="val">${report.total_requests.toLocaleString()}</div><div class="lbl">Total Requests</div></div>
    <div class="stat"><div class="val">${report.blocked_requests.toLocaleString()}</div><div class="lbl">Blocked</div></div>
    <div class="stat"><div class="val">${report.pii_detections.toLocaleString()}</div><div class="lbl">PII Detections</div></div>
    <div class="stat"><div class="val">${report.baa_violations.toLocaleString()}</div><div class="lbl">BAA Violations</div></div>
    <div class="stat"><div class="val">${report.policy_violations.toLocaleString()}</div><div class="lbl">Policy Violations</div></div>
  </div>

  <h2>PII Detection Breakdown</h2>
  <table><thead><tr><th>PII Type</th><th>Count</th></tr></thead><tbody>
  ${report.pii_breakdown.length === 0
    ? '<tr><td colspan="2" style="color:#9ca3af;text-align:center">No PII detections in period</td></tr>'
    : report.pii_breakdown.map(p => `<tr><td style="font-family:monospace">${p.type}</td><td>${p.count}</td></tr>`).join('')}
  </tbody></table>

  <h2>SIEM Events Breakdown</h2>
  <table><thead><tr><th>Event Type</th><th>Count</th></tr></thead><tbody>
  ${report.siem_breakdown.length === 0
    ? '<tr><td colspan="2" style="color:#9ca3af;text-align:center">No SIEM events in period</td></tr>'
    : report.siem_breakdown.map(s => `<tr><td style="font-family:monospace">${s.event_type}</td><td>${s.count}</td></tr>`).join('')}
  </tbody></table>

  <h2>SOC 2 Compliance Checklist</h2>
  <table><thead><tr><th>Category</th><th>Control</th><th>Status</th><th>Detail</th></tr></thead><tbody>
  ${rows(report.checklist)}
  </tbody></table>

  <div class="footer">Generated by ToTra Compliance Platform &mdash; ${new Date().toISOString()}</div>
  </body></html>`
}

async function downloadReportJSON(period: '30d' | '90d' | '1y') {
  const report = await getComplianceReport(period, 'json') as ComplianceReportData
  const blob = new Blob([JSON.stringify(report, null, 2)], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `totra-compliance-report-${new Date().toISOString().slice(0, 10)}.json`
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}

async function printReport(period: '30d' | '90d' | '1y') {
  const report = await getComplianceReport(period, 'json') as ComplianceReportData
  const html = buildPrintHTML(report)
  const iframe = document.createElement('iframe')
  iframe.style.position = 'fixed'
  iframe.style.right = '0'
  iframe.style.bottom = '0'
  iframe.style.width = '0'
  iframe.style.height = '0'
  iframe.style.border = '0'
  document.body.appendChild(iframe)
  const doc = iframe.contentWindow?.document
  if (!doc) return
  doc.open()
  doc.write(html)
  doc.close()
  setTimeout(() => {
    iframe.contentWindow?.print()
    setTimeout(() => document.body.removeChild(iframe), 1000)
  }, 300)
}

export default function CompliancePage() {
  const [month, setMonth] = useState(() => new Date().toISOString().slice(0, 7))
  const [exportPeriod] = useState<'30d' | '90d' | '1y'>('30d')

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
          <button
            onClick={() => downloadReportJSON(exportPeriod)}
            className="px-3 py-1.5 text-sm bg-indigo-100 hover:bg-indigo-200 text-indigo-700 rounded-lg font-medium"
          >
            Export Report (JSON)
          </button>
          <button
            onClick={() => printReport(exportPeriod)}
            className="px-3 py-1.5 text-sm bg-emerald-100 hover:bg-emerald-200 text-emerald-700 rounded-lg font-medium"
          >
            Print / PDF
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
