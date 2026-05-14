import { useQuery } from '@tanstack/react-query'
import { apiClient } from '../../api/client'

interface AnomalyUser {
  user_id: string
  user_name: string
  user_email: string
  department: string
  recent_count: number
  weekly_baseline: number
  severity: 'high' | 'medium'
  detected_at: string
}

const severityStyles: Record<string, string> = {
  high: 'border-red-300 bg-red-50',
  medium: 'border-yellow-300 bg-yellow-50',
}

const severityBadge: Record<string, string> = {
  high: 'bg-red-100 text-red-800',
  medium: 'bg-yellow-100 text-yellow-800',
}

export default function ComplianceAnomalyPage() {
  const { data, isLoading } = useQuery<{ anomalies: AnomalyUser[] }>({
    queryKey: ['compliance-anomalies'],
    queryFn: () =>
      apiClient.get('/api/admin/compliance/anomalies').then(r => r.data),
  })

  if (isLoading) return <div className="p-8 text-gray-400">Loading anomalies…</div>

  const anomalies = data?.anomalies ?? []

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Anomaly Alerts</h1>
        <span className="text-sm text-gray-500">
          {anomalies.length} anomal{anomalies.length === 1 ? 'y' : 'ies'} detected (last 7 days)
        </span>
      </div>

      {anomalies.length === 0 ? (
        <div className="bg-white border rounded-xl p-12 text-center text-gray-400">
          <p className="text-lg font-medium">No anomalies detected</p>
          <p className="text-sm mt-1">All users are within normal violation thresholds.</p>
        </div>
      ) : (
        <div className="space-y-3">
          {anomalies.map(a => (
            <div
              key={a.user_id}
              className={`border rounded-xl p-4 ${severityStyles[a.severity] ?? 'border-gray-200 bg-white'}`}
            >
              <div className="flex items-start justify-between gap-4">
                <div className="flex-1">
                  <div className="flex items-center gap-2 mb-1">
                    <span className="font-semibold text-gray-900">{a.user_name}</span>
                    <span
                      className={`px-2 py-0.5 rounded-full text-xs font-medium ${severityBadge[a.severity] ?? ''}`}
                    >
                      {a.severity.toUpperCase()}
                    </span>
                  </div>
                  <p className="text-sm text-gray-500">{a.user_email}</p>
                  {a.department && (
                    <p className="text-sm text-gray-500">Department: {a.department}</p>
                  )}
                </div>
                <div className="text-right shrink-0">
                  <p className="text-sm font-medium text-gray-700">
                    {a.recent_count} violations (7d)
                  </p>
                  <p className="text-xs text-gray-400">
                    Baseline: {a.weekly_baseline.toFixed(1)}/wk
                  </p>
                  <p className="text-xs text-gray-400 mt-1">
                    {new Date(a.detected_at).toLocaleString()}
                  </p>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
