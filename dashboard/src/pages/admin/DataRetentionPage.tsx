import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiClient } from '../../api/client'
import { Card, CardContent, CardHeader, CardTitle } from '../../components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '../../components/ui/dialog'

// ---- types ----

interface RetentionConfig {
  retention_months: number
  last_cleanup_at: string | null
  last_cleanup_rows_deleted: number | null
}

interface CleanupResult {
  rows_deleted: number
  last_cleanup_at: string
}

// ---- api helpers ----

const fetchConfig = (): Promise<RetentionConfig> =>
  apiClient.get<RetentionConfig>('/api/admin/data-retention/config').then(r => r.data)

const saveMonths = (months: number): Promise<{ retention_months: number }> =>
  apiClient.put('/api/admin/data-retention/config', { months }).then(r => r.data)

const triggerCleanup = (): Promise<CleanupResult> =>
  apiClient.post<CleanupResult>('/api/admin/data-retention/cleanup').then(r => r.data)

// ---- page ----

export default function DataRetentionPage() {
  const qc = useQueryClient()

  const { data: config, isLoading, isError } = useQuery<RetentionConfig>({
    queryKey: ['data-retention-config'],
    queryFn: fetchConfig,
  })

  // Current-setting edit state
  const [editMonths, setEditMonths] = useState<string>('')
  const [editOpen, setEditOpen] = useState(false)

  const saveMutation = useMutation({
    mutationFn: (months: number) => saveMonths(months),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['data-retention-config'] })
      setEditOpen(false)
      setEditMonths('')
    },
  })

  // Cleanup state
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [cleanupResult, setCleanupResult] = useState<CleanupResult | null>(null)

  const cleanupMutation = useMutation({
    mutationFn: triggerCleanup,
    onSuccess: (result) => {
      setCleanupResult(result)
      setConfirmOpen(false)
      qc.invalidateQueries({ queryKey: ['data-retention-config'] })
    },
  })

  const handleSave = () => {
    const months = parseInt(editMonths, 10)
    if (isNaN(months) || months < 1 || months > 84) return
    saveMutation.mutate(months)
  }

  const handleOpenEdit = () => {
    setEditMonths(String(config?.retention_months ?? ''))
    setEditOpen(true)
  }

  const editMonthsNum = parseInt(editMonths, 10)
  const editValid = !isNaN(editMonthsNum) && editMonthsNum >= 1 && editMonthsNum <= 84

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Data Retention</h1>

      {/* Current Setting card */}
      <Card>
        <CardHeader>
          <CardTitle>Current Setting</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {isLoading && <p className="text-zinc-500 text-sm">Loading...</p>}
          {isError && <p className="text-red-500 text-sm">Failed to load retention config.</p>}
          {config && !isLoading && (
            <>
              <p className="text-sm text-zinc-400">
                Data is retained for{' '}
                <span className="font-semibold text-zinc-100">
                  {config.retention_months} month{config.retention_months !== 1 ? 's' : ''}
                </span>
                .
              </p>
              <button
                onClick={handleOpenEdit}
                className="h-9 px-4 rounded-md bg-blue-600 text-white text-sm font-medium hover:bg-blue-700"
              >
                Edit
              </button>
            </>
          )}
        </CardContent>
      </Card>

      {/* Cleanup History card */}
      <Card>
        <CardHeader>
          <CardTitle>Cleanup History</CardTitle>
        </CardHeader>
        <CardContent>
          {isLoading && <p className="text-zinc-500 text-sm">Loading...</p>}
          {config && !isLoading && (
            config.last_cleanup_at == null ? (
              <p className="text-zinc-500 text-sm">No cleanup has been run yet.</p>
            ) : (
              <div className="space-y-1 text-sm">
                <p className="text-zinc-400">
                  Last run:{' '}
                  <span className="font-semibold text-zinc-100">
                    {new Date(config.last_cleanup_at).toLocaleString()}
                  </span>
                </p>
                <p className="text-zinc-400">
                  Rows deleted:{' '}
                  <span className="font-semibold text-zinc-100">
                    {config.last_cleanup_rows_deleted?.toLocaleString() ?? '—'}
                  </span>
                </p>
              </div>
            )
          )}
        </CardContent>
      </Card>

      {/* Run Cleanup card */}
      <Card>
        <CardHeader>
          <CardTitle>Run Cleanup</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-sm text-zinc-400">
            Permanently delete all usage records older than the current retention window.
            This action cannot be undone.
          </p>
          <button
            onClick={() => {
              setCleanupResult(null)
              setConfirmOpen(true)
            }}
            disabled={isLoading || isError}
            className="h-9 px-4 rounded-md bg-red-700 text-white text-sm font-medium hover:bg-red-800 disabled:opacity-50"
          >
            Run Cleanup Now
          </button>
          {cleanupResult && (
            <p className="text-sm text-green-400">
              Cleanup complete — {cleanupResult.rows_deleted.toLocaleString()} row
              {cleanupResult.rows_deleted !== 1 ? 's' : ''} deleted.
            </p>
          )}
          {cleanupMutation.isError && (
            <p className="text-sm text-red-400">Cleanup failed. Please try again.</p>
          )}
        </CardContent>
      </Card>

      {/* Edit retention months dialog */}
      <Dialog open={editOpen} onOpenChange={setEditOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Set Retention Period</DialogTitle>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-1">
              <label className="text-sm text-zinc-400" htmlFor="retention-months">
                Retention months (1 – 84)
              </label>
              <input
                id="retention-months"
                type="number"
                min={1}
                max={84}
                value={editMonths}
                onChange={e => setEditMonths(e.target.value)}
                className="h-9 w-32 rounded-md border border-zinc-700 bg-zinc-800 px-3 text-sm text-zinc-100"
              />
              {editMonths !== '' && !editValid && (
                <p className="text-xs text-red-400">Must be between 1 and 84.</p>
              )}
            </div>
            {saveMutation.isError && (
              <p className="text-sm text-red-400">Save failed. Please try again.</p>
            )}
            <div className="flex gap-3">
              <button
                onClick={handleSave}
                disabled={!editValid || saveMutation.isPending}
                className="h-9 px-4 rounded-md bg-blue-600 text-white text-sm font-medium hover:bg-blue-700 disabled:opacity-50"
              >
                {saveMutation.isPending ? 'Saving...' : 'Save'}
              </button>
              <button
                onClick={() => setEditOpen(false)}
                className="h-9 px-4 rounded-md border border-zinc-600 text-zinc-300 text-sm font-medium hover:bg-zinc-800"
              >
                Cancel
              </button>
            </div>
          </div>
        </DialogContent>
      </Dialog>

      {/* Confirm cleanup dialog */}
      <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Confirm Cleanup</DialogTitle>
          </DialogHeader>
          <div className="space-y-4">
            <p className="text-sm text-zinc-300">
              This will permanently delete all usage records older than{' '}
              <span className="font-semibold text-zinc-100">
                {config?.retention_months ?? '?'} month
                {(config?.retention_months ?? 2) !== 1 ? 's' : ''}
              </span>
              . This cannot be undone.
            </p>
            <div className="flex gap-3">
              <button
                onClick={() => cleanupMutation.mutate()}
                disabled={cleanupMutation.isPending}
                className="h-9 px-4 rounded-md bg-red-700 text-white text-sm font-medium hover:bg-red-800 disabled:opacity-50"
              >
                {cleanupMutation.isPending ? 'Running...' : 'Yes, run cleanup'}
              </button>
              <button
                onClick={() => setConfirmOpen(false)}
                disabled={cleanupMutation.isPending}
                className="h-9 px-4 rounded-md border border-zinc-600 text-zinc-300 text-sm font-medium hover:bg-zinc-800 disabled:opacity-50"
              >
                Cancel
              </button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}
