import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { listPendingRequests, approveQuota, rejectQuota } from "../../api/client";
import { Card, CardContent } from "../../components/ui/card";
import { Button } from "../../components/ui/button";
import { Badge } from "../../components/ui/badge";

export function QuotaPage() {
  const qc = useQueryClient();

  const { data, isLoading } = useQuery({
    queryKey: ["quota-requests"],
    queryFn: () => listPendingRequests().then((r) => r.data),
  });

  const approveMutation = useMutation({
    mutationFn: approveQuota,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["quota-requests"] }),
  });

  const rejectMutation = useMutation({
    mutationFn: rejectQuota,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["quota-requests"] }),
  });

  const isPending = approveMutation.isPending || rejectMutation.isPending;

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Quota Requests</h1>

      <Card>
        <CardContent className="pt-4">
          {isLoading ? (
            <p className="text-zinc-500 text-sm">Loading...</p>
          ) : !data?.requests?.length ? (
            <p className="text-zinc-500 text-sm py-4 text-center">No pending quota requests.</p>
          ) : (
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-zinc-800 text-zinc-400">
                  <th className="text-left py-2 font-medium">User ID</th>
                  <th className="text-right py-2 font-medium">Requested SCU</th>
                  <th className="text-left py-2 font-medium">Reason</th>
                  <th className="text-center py-2 font-medium">Status</th>
                  <th className="text-right py-2 font-medium">Actions</th>
                </tr>
              </thead>
              <tbody>
                {data.requests.map((r) => (
                  <tr key={r.id} className="border-b border-zinc-800/50">
                    <td className="py-2 font-mono text-xs text-zinc-400">{r.user_id.slice(0, 8)}…</td>
                    <td className="text-right">{r.new_quota.toLocaleString()}</td>
                    <td className="text-zinc-300 max-w-[240px] truncate">{r.reason || "—"}</td>
                    <td className="text-center">
                      <Badge
                        variant={
                          r.status === "approved"
                            ? "default"
                            : r.status === "rejected"
                            ? "destructive"
                            : "secondary"
                        }
                      >
                        {r.status}
                      </Badge>
                    </td>
                    <td className="text-right">
                      {r.status === "pending" && (
                        <div className="flex justify-end gap-2">
                          <Button
                            size="sm"
                            disabled={isPending}
                            onClick={() => approveMutation.mutate(r.id)}
                          >
                            Approve
                          </Button>
                          <Button
                            size="sm"
                            variant="destructive"
                            disabled={isPending}
                            onClick={() => rejectMutation.mutate(r.id)}
                          >
                            Reject
                          </Button>
                        </div>
                      )}
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
