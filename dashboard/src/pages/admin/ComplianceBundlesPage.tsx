import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiClient } from '../../api/client'

interface BundlePolicy {
  name: string
  pattern: string
  action: string
}

interface BundleActivation {
  id: string
  tenant_id: string
  bundle_id: string
  activated_by: string
  activated_at: string
  is_active: boolean
}

interface BundleWithStatus {
  id: string
  name: string
  description: string
  industry: string
  policies: BundlePolicy[]
  require_baa: boolean
  audit_level: string
  features: string[]
  is_active: boolean
  activation?: BundleActivation
}

const INDUSTRY_ICONS: Record<string, string> = {
  Healthcare: '🏥',
  Legal: '⚖️',
  Government: '🏛️',
  Finance: '🏦',
  Education: '🎓',
}

const AUDIT_LEVEL_COLORS: Record<string, string> = {
  standard: 'bg-gray-100 text-gray-700',
  enhanced: 'bg-blue-100 text-blue-700',
  strict: 'bg-purple-100 text-purple-700',
}

function ConfirmActivateModal({
  bundle,
  onClose,
  onConfirm,
  isPending,
}: {
  bundle: BundleWithStatus
  onClose: () => void
  onConfirm: () => void
  isPending: boolean
}) {
  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div className="bg-white rounded-xl shadow-xl w-full max-w-lg mx-4">
        <div className="px-6 py-4 border-b">
          <h2 className="text-lg font-semibold">Activate {bundle.name}</h2>
          <p className="text-sm text-gray-500 mt-1">
            The following changes will be applied to your tenant.
          </p>
        </div>

        <div className="px-6 py-4 space-y-4">
          <div>
            <p className="text-sm font-medium text-gray-700 mb-2">Features enabled:</p>
            <ul className="space-y-1">
              {bundle.features.map(f => (
                <li key={f} className="flex items-start gap-2 text-sm text-gray-600">
                  <span className="text-green-500 mt-0.5">✓</span>
                  {f}
                </li>
              ))}
            </ul>
          </div>

          {bundle.policies.length > 0 && (
            <div>
              <p className="text-sm font-medium text-gray-700 mb-2">
                Policy rules that will be created:
              </p>
              <div className="space-y-1">
                {bundle.policies.map(p => (
                  <div
                    key={p.name}
                    className="flex items-center justify-between text-xs bg-gray-50 rounded px-3 py-1.5"
                  >
                    <span className="font-mono text-gray-700">{p.name}</span>
                    <span
                      className={`px-2 py-0.5 rounded-full font-medium ${
                        p.action === 'block'
                          ? 'bg-red-100 text-red-700'
                          : 'bg-yellow-100 text-yellow-700'
                      }`}
                    >
                      {p.action}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          )}

          {bundle.require_baa && (
            <div className="bg-yellow-50 border border-yellow-200 rounded-lg px-4 py-3 text-sm text-yellow-800">
              This bundle requires a signed BAA. Ensure your organization has completed
              BAA signing before processing HIPAA workloads.
            </div>
          )}

          <div className="flex gap-3 justify-end pt-2">
            <button
              onClick={onClose}
              className="px-4 py-2 text-sm font-medium text-gray-700 border rounded-lg hover:bg-gray-50"
            >
              Cancel
            </button>
            <button
              disabled={isPending}
              onClick={onConfirm}
              className="px-4 py-2 text-sm font-medium text-white bg-indigo-600 rounded-lg hover:bg-indigo-700 disabled:opacity-50"
            >
              {isPending ? 'Activating…' : 'Activate Bundle'}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

function BundleCard({
  bundle,
  onActivate,
  onDeactivate,
}: {
  bundle: BundleWithStatus
  onActivate: (b: BundleWithStatus) => void
  onDeactivate: (id: string) => void
}) {
  const icon = INDUSTRY_ICONS[bundle.industry] ?? '📋'
  const auditColor = AUDIT_LEVEL_COLORS[bundle.audit_level] ?? AUDIT_LEVEL_COLORS.standard

  return (
    <div
      className={`bg-white border rounded-xl p-5 flex flex-col gap-4 ${
        bundle.is_active ? 'border-indigo-300 ring-1 ring-indigo-200' : ''
      }`}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="flex items-center gap-3">
          <span className="text-2xl">{icon}</span>
          <div>
            <h3 className="font-semibold text-gray-900">{bundle.name}</h3>
            <span className="text-xs text-gray-500">{bundle.industry}</span>
          </div>
        </div>
        <div className="flex flex-col items-end gap-1 shrink-0">
          {bundle.is_active && (
            <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-green-100 text-green-800">
              Active
            </span>
          )}
          {bundle.audit_level && (
            <span className={`inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium ${auditColor}`}>
              {bundle.audit_level} audit
            </span>
          )}
        </div>
      </div>

      <p className="text-sm text-gray-500 leading-relaxed">{bundle.description}</p>

      <ul className="space-y-1">
        {bundle.features.map(f => (
          <li key={f} className="flex items-start gap-2 text-xs text-gray-600">
            <span className="text-indigo-400 mt-0.5 shrink-0">•</span>
            {f}
          </li>
        ))}
      </ul>

      {bundle.require_baa && (
        <div className="text-xs text-yellow-700 bg-yellow-50 rounded px-3 py-1.5 border border-yellow-100">
          Requires signed BAA
        </div>
      )}

      <div className="pt-1 border-t">
        {bundle.is_active ? (
          <div className="flex items-center justify-between">
            <p className="text-xs text-gray-500">
              Active since{' '}
              {bundle.activation?.activated_at
                ? new Date(bundle.activation.activated_at).toLocaleDateString()
                : '—'}
            </p>
            <button
              onClick={() => onDeactivate(bundle.id)}
              className="text-xs text-red-600 hover:underline"
            >
              Deactivate
            </button>
          </div>
        ) : (
          <button
            onClick={() => onActivate(bundle)}
            className="w-full py-2 text-sm font-medium text-indigo-600 border border-indigo-200 rounded-lg hover:bg-indigo-50 transition-colors"
          >
            Activate
          </button>
        )}
      </div>
    </div>
  )
}

export default function ComplianceBundlesPage() {
  const qc = useQueryClient()
  const [pendingBundle, setPendingBundle] = useState<BundleWithStatus | null>(null)

  const { data: bundles = [], isLoading } = useQuery<BundleWithStatus[]>({
    queryKey: ['compliance-bundles'],
    queryFn: () => apiClient.get('/api/compliance/bundles').then(r => r.data),
  })

  const activateMutation = useMutation({
    mutationFn: (bundleID: string) =>
      apiClient.post(`/api/compliance/bundles/${bundleID}/activate`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['compliance-bundles'] })
      setPendingBundle(null)
    },
  })

  const deactivateMutation = useMutation({
    mutationFn: (bundleID: string) =>
      apiClient.delete(`/api/compliance/bundles/${bundleID}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['compliance-bundles'] })
    },
  })

  const activeBundles = bundles.filter(b => b.is_active)
  const inactiveBundles = bundles.filter(b => !b.is_active)

  if (isLoading) {
    return (
      <div className="p-6">
        <p className="text-gray-400 text-sm">Loading compliance bundles…</p>
      </div>
    )
  }

  return (
    <div className="p-6 space-y-6">
      {pendingBundle && (
        <ConfirmActivateModal
          bundle={pendingBundle}
          onClose={() => setPendingBundle(null)}
          onConfirm={() => activateMutation.mutate(pendingBundle.id)}
          isPending={activateMutation.isPending}
        />
      )}

      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">Industry Compliance Bundles</h1>
          <p className="text-sm text-gray-500 mt-1">
            One-click activation of pre-configured compliance controls for your industry.
          </p>
        </div>
        {activeBundles.length > 0 && (
          <span className="px-3 py-1 rounded-full text-sm font-medium bg-indigo-100 text-indigo-700">
            {activeBundles.length} active
          </span>
        )}
      </div>

      {activeBundles.length > 0 && (
        <div>
          <h2 className="text-sm font-semibold text-gray-700 uppercase tracking-wide mb-3">
            Active Bundles
          </h2>
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {activeBundles.map(b => (
              <BundleCard
                key={b.id}
                bundle={b}
                onActivate={setPendingBundle}
                onDeactivate={id => deactivateMutation.mutate(id)}
              />
            ))}
          </div>
        </div>
      )}

      {inactiveBundles.length > 0 && (
        <div>
          <h2 className="text-sm font-semibold text-gray-700 uppercase tracking-wide mb-3">
            Available Bundles
          </h2>
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {inactiveBundles.map(b => (
              <BundleCard
                key={b.id}
                bundle={b}
                onActivate={setPendingBundle}
                onDeactivate={id => deactivateMutation.mutate(id)}
              />
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
