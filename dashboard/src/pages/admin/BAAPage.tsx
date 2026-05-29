import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiClient } from '../../api/client'

interface BAAStatus {
  signed: boolean
  signed_at?: string
  signed_by?: string
  signed_by_email?: string
  agreement_version?: string
  record_id?: string
}

interface BAARecord {
  id: string
  signed_by_name: string
  signed_by_email: string
  signed_at: string
  agreement_version: string
  is_active: boolean
}

const BAA_TEXT = `BUSINESS ASSOCIATE AGREEMENT

This Business Associate Agreement ("BAA") is entered into between the Customer
("Covered Entity") and ToTra ("Business Associate").

1. Permitted Uses and Disclosures. Business Associate may use or disclose
   Protected Health Information ("PHI") only as permitted or required by this
   Agreement or as Required by Law.

2. Safeguards. Business Associate agrees to use appropriate administrative,
   physical, and technical safeguards to prevent unauthorized use or disclosure
   of PHI, in conformance with 45 C.F.R. Part 164 Subpart C.

3. Reporting. Business Associate agrees to report to Covered Entity any use or
   disclosure of PHI not provided for in this Agreement, including security
   incidents, within 30 days of discovery.

4. Subcontractors. Business Associate shall ensure that any subcontractors that
   create, receive, maintain, or transmit PHI on behalf of Business Associate
   agree to the same restrictions and conditions that apply to Business Associate.

5. Term and Termination. This Agreement shall be effective as of the date
   signed and shall terminate when all PHI provided or created by Business
   Associate is destroyed or returned to Covered Entity.

By signing, your organization agrees to use ToTra's services in compliance with
HIPAA requirements and the terms described above.`

function SignBAAModal({ onClose, onSuccess }: { onClose: () => void; onSuccess: () => void }) {
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [authorized, setAuthorized] = useState(false)
  const [error, setError] = useState('')

  const mutation = useMutation({
    mutationFn: () =>
      apiClient.post('/api/compliance/baa/sign', { name, email, version: 'v1.0' }),
    onSuccess: () => {
      onSuccess()
      onClose()
    },
    onError: (err: unknown) => {
      const msg = (err as { response?: { data?: { error?: string } } })?.response?.data?.error
      setError(msg ?? 'Failed to sign BAA')
    },
  })

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div className="bg-white rounded-xl shadow-xl w-full max-w-2xl mx-4 flex flex-col max-h-[90vh]">
        <div className="px-6 py-4 border-b">
          <h2 className="text-lg font-semibold">Sign Business Associate Agreement</h2>
          <p className="text-sm text-gray-500 mt-1">
            Please read the agreement below before signing.
          </p>
        </div>

        <div className="px-6 py-4 overflow-y-auto flex-1">
          <pre className="text-xs text-gray-600 whitespace-pre-wrap bg-gray-50 rounded-lg p-4 border font-mono leading-relaxed">
            {BAA_TEXT}
          </pre>
        </div>

        <div className="px-6 py-4 border-t space-y-4">
          {error && (
            <p className="text-sm text-red-600 bg-red-50 px-3 py-2 rounded">{error}</p>
          )}

          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">
                Full Name <span className="text-red-500">*</span>
              </label>
              <input
                type="text"
                value={name}
                onChange={e => setName(e.target.value)}
                placeholder="Jane Smith"
                className="w-full border rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">
                Email <span className="text-red-500">*</span>
              </label>
              <input
                type="email"
                value={email}
                onChange={e => setEmail(e.target.value)}
                placeholder="jane@example.com"
                className="w-full border rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
              />
            </div>
          </div>

          <label className="flex items-start gap-3 cursor-pointer">
            <input
              type="checkbox"
              checked={authorized}
              onChange={e => setAuthorized(e.target.checked)}
              className="mt-0.5 h-4 w-4 rounded border-gray-300 text-indigo-600 focus:ring-indigo-500"
            />
            <span className="text-sm text-gray-700">
              I am authorized to sign on behalf of my organization and agree to the
              terms of the Business Associate Agreement above.
            </span>
          </label>

          <div className="flex gap-3 justify-end">
            <button
              onClick={onClose}
              className="px-4 py-2 text-sm font-medium text-gray-700 border rounded-lg hover:bg-gray-50"
            >
              Cancel
            </button>
            <button
              disabled={!name || !email || !authorized || mutation.isPending}
              onClick={() => mutation.mutate()}
              className="px-4 py-2 text-sm font-medium text-white bg-indigo-600 rounded-lg hover:bg-indigo-700 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {mutation.isPending ? 'Signing…' : 'Sign Agreement'}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

export default function BAAPage() {
  const qc = useQueryClient()
  const [showModal, setShowModal] = useState(false)

  const { data: status, isLoading } = useQuery<BAAStatus>({
    queryKey: ['baa-status'],
    queryFn: () => apiClient.get('/api/compliance/baa').then(r => r.data),
  })

  const { data: history = [] } = useQuery<BAARecord[]>({
    queryKey: ['baa-history'],
    queryFn: () => apiClient.get('/api/compliance/baa/history').then(r => r.data),
  })

  const revokeMutation = useMutation({
    mutationFn: (id: string) => apiClient.delete(`/api/compliance/baa/${id}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['baa-status'] })
      qc.invalidateQueries({ queryKey: ['baa-history'] })
    },
  })

  function handleDownload() {
    if (!status?.signed) return
    const content = JSON.stringify(
      {
        document: 'Business Associate Agreement',
        version: status.agreement_version,
        signed_by: status.signed_by,
        signed_by_email: status.signed_by_email,
        signed_at: status.signed_at,
        agreement_text: BAA_TEXT,
      },
      null,
      2
    )
    const blob = new Blob([content], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `baa-${status.agreement_version ?? 'v1.0'}.json`
    a.click()
    URL.revokeObjectURL(url)
  }

  if (isLoading) {
    return (
      <div className="p-6">
        <p className="text-gray-400 text-sm">Loading BAA status…</p>
      </div>
    )
  }

  return (
    <div className="p-6 space-y-6">
      {showModal && (
        <SignBAAModal
          onClose={() => setShowModal(false)}
          onSuccess={() => {
            qc.invalidateQueries({ queryKey: ['baa-status'] })
            qc.invalidateQueries({ queryKey: ['baa-history'] })
          }}
        />
      )}

      <h1 className="text-2xl font-bold">Business Associate Agreement</h1>

      {/* Status Card */}
      {status?.signed ? (
        <div className="bg-green-50 border border-green-200 rounded-xl p-5 flex items-start justify-between gap-4">
          <div>
            <div className="flex items-center gap-2 mb-1">
              <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-green-100 text-green-800">
                Signed
              </span>
              <span className="text-sm font-medium text-green-900">BAA is active</span>
            </div>
            <p className="text-sm text-green-700 mt-1">
              Signed by <strong>{status.signed_by}</strong> ({status.signed_by_email})
            </p>
            <p className="text-xs text-green-600 mt-0.5">
              {status.signed_at
                ? new Date(status.signed_at).toLocaleString()
                : ''}
              {' '}— Version {status.agreement_version}
            </p>
          </div>
          <button
            onClick={handleDownload}
            className="shrink-0 px-4 py-2 text-sm font-medium text-green-700 bg-white border border-green-300 rounded-lg hover:bg-green-50"
          >
            Download BAA PDF
          </button>
        </div>
      ) : (
        <div className="bg-yellow-50 border border-yellow-200 rounded-xl p-5 flex items-start justify-between gap-4">
          <div>
            <div className="flex items-center gap-2 mb-1">
              <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-yellow-100 text-yellow-800">
                Not Signed
              </span>
              <span className="text-sm font-medium text-yellow-900">No active BAA</span>
            </div>
            <p className="text-sm text-yellow-700 mt-1">
              Your organization has not signed a BAA. HIPAA workloads require a signed BAA.
            </p>
          </div>
          <button
            onClick={() => setShowModal(true)}
            className="shrink-0 px-4 py-2 text-sm font-medium text-white bg-yellow-600 rounded-lg hover:bg-yellow-700"
          >
            Sign BAA
          </button>
        </div>
      )}

      {/* History Table */}
      {history.length > 0 && (
        <div className="bg-white border rounded-xl overflow-hidden">
          <div className="px-5 py-4 border-b">
            <h2 className="font-semibold">Signing History</h2>
          </div>
          <table className="w-full text-sm">
            <thead className="bg-gray-50">
              <tr>
                {['Signed By', 'Email', 'Version', 'Date', 'Status', ''].map(h => (
                  <th key={h} className="px-4 py-2 text-left font-medium text-gray-600 text-xs">
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {history.map(r => (
                <tr key={r.id} className="border-t">
                  <td className="px-4 py-2 font-medium">{r.signed_by_name}</td>
                  <td className="px-4 py-2 text-gray-500">{r.signed_by_email}</td>
                  <td className="px-4 py-2 font-mono text-xs">{r.agreement_version}</td>
                  <td className="px-4 py-2 text-gray-500">
                    {new Date(r.signed_at).toLocaleDateString()}
                  </td>
                  <td className="px-4 py-2">
                    <span
                      className={`px-2 py-0.5 rounded-full text-xs font-medium ${
                        r.is_active
                          ? 'bg-green-100 text-green-800'
                          : 'bg-gray-100 text-gray-500'
                      }`}
                    >
                      {r.is_active ? 'Active' : 'Revoked'}
                    </span>
                  </td>
                  <td className="px-4 py-2 text-right">
                    {r.is_active && (
                      <button
                        disabled={revokeMutation.isPending}
                        onClick={() => revokeMutation.mutate(r.id)}
                        className="text-xs text-red-600 hover:underline disabled:opacity-50"
                      >
                        Revoke
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
