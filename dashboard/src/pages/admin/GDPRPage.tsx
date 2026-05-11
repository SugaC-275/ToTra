import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  getDataRetention,
  setDataRetention,
  runRetentionCleanup,
  listDeletionRequests,
  approveDeletionRequest,
  rejectDeletionRequest,
} from "../../api/client";
import { Card, CardContent, CardHeader, CardTitle } from "../../components/ui/card";

export default function GDPRPage() {
  const qc = useQueryClient();

  const { data: retentionData, isLoading: retLoading } = useQuery({
    queryKey: ["data-retention"],
    queryFn: () => getDataRetention().then((r) => r.data),
  });

  const [monthsInput, setMonthsInput] = useState("");
  const retentionMutation = useMutation({
    mutationFn: (months: number) => setDataRetention(months),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["data-retention"] });
      setMonthsInput("");
    },
  });

  const cleanupMutation = useMutation({
    mutationFn: runRetentionCleanup,
  });

  const { data: deletionData, isLoading: delLoading } = useQuery({
    queryKey: ["deletion-requests"],
    queryFn: () => listDeletionRequests().then((r) => r.data),
  });

  const approveMutation = useMutation({
    mutationFn: (id: string) => approveDeletionRequest(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["deletion-requests"] }),
  });

  const rejectMutation = useMutation({
    mutationFn: (id: string) => rejectDeletionRequest(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["deletion-requests"] }),
  });

  const requests = deletionData?.requests ?? [];

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">GDPR &amp; Compliance</h1>

      <Card>
        <CardHeader>
          <CardTitle>Data Retention Policy</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {retLoading ? (
            <p className="text-zinc-500 text-sm">Loading...</p>
          ) : (
            <p className="text-sm text-zinc-400">
              Current retention window:{" "}
              <span className="font-semibold text-zinc-100">
                {retentionData?.data_retention_months ?? "—"} months
              </span>
            </p>
          )}
          <div className="flex gap-3 items-end">
            <div className="space-y-1">
              <label className="text-sm text-zinc-400">New retention (months)</label>
              <input
                type="number"
                min="1"
                placeholder="e.g. 24"
                value={monthsInput}
                onChange={(e) => setMonthsInput(e.target.value)}
                className="h-9 w-32 rounded-md border border-zinc-700 bg-zinc-800 px-3 text-sm text-zinc-100"
              />
            </div>
            <button
              disabled={!monthsInput || parseInt(monthsInput) < 1 || retentionMutation.isPending}
              onClick={() => retentionMutation.mutate(parseInt(monthsInput))}
              className="h-9 px-4 rounded-md bg-blue-600 text-white text-sm font-medium hover:bg-blue-700 disabled:opacity-50"
            >
              {retentionMutation.isPending ? "Saving..." : "Save"}
            </button>
          </div>

          <div className="pt-2 border-t border-zinc-800">
            <p className="text-xs text-zinc-500 mb-3">
              Manually trigger deletion of all usage records older than the current retention window. This cannot be undone.
            </p>
            <button
              disabled={cleanupMutation.isPending}
              onClick={() => cleanupMutation.mutate()}
              className="h-9 px-4 rounded-md bg-red-700 text-white text-sm font-medium hover:bg-red-800 disabled:opacity-50"
            >
              {cleanupMutation.isPending ? "Running..." : "Run Retention Cleanup"}
            </button>
            {cleanupMutation.isSuccess && (
              <p className="mt-2 text-sm text-green-400">
                Cleanup complete — {(cleanupMutation.data?.data as { deleted_count?: number })?.deleted_count ?? 0} records deleted.
              </p>
            )}
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>
            Data Deletion Requests
            {requests.length > 0 && (
              <span className="ml-2 inline-flex items-center rounded-full bg-red-900/50 px-2 py-0.5 text-xs font-medium text-red-300">
                {requests.length} pending
              </span>
            )}
          </CardTitle>
        </CardHeader>
        <CardContent>
          {delLoading ? (
            <p className="text-zinc-500 text-sm">Loading...</p>
          ) : requests.length === 0 ? (
            <p className="text-zinc-500 text-sm">No pending data deletion requests.</p>
          ) : (
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-zinc-800 text-zinc-400">
                  <th className="text-left py-2 font-medium">Employee</th>
                  <th className="text-left py-2 font-medium">Email</th>
                  <th className="text-left py-2 font-medium">Requested</th>
                  <th className="text-right py-2 font-medium">Actions</th>
                </tr>
              </thead>
              <tbody>
                {requests.map((r) => (
                  <tr key={r.id} className="border-b border-zinc-800/50">
                    <td className="py-2 text-zinc-200 font-medium">{r.user_name ?? r.user_id}</td>
                    <td className="py-2 text-zinc-400">{r.user_email ?? "—"}</td>
                    <td className="py-2 text-zinc-400">{r.requested_at.slice(0, 10)}</td>
                    <td className="py-2 flex gap-2 justify-end">
                      <button
                        disabled={approveMutation.isPending}
                        onClick={() => approveMutation.mutate(r.id)}
                        className="px-3 py-1 rounded-md bg-red-700 text-white text-xs font-medium hover:bg-red-800 disabled:opacity-50"
                      >
                        Approve &amp; Erase
                      </button>
                      <button
                        disabled={rejectMutation.isPending}
                        onClick={() => rejectMutation.mutate(r.id)}
                        className="px-3 py-1 rounded-md border border-zinc-600 text-zinc-300 text-xs font-medium hover:bg-zinc-800 disabled:opacity-50"
                      >
                        Reject
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
