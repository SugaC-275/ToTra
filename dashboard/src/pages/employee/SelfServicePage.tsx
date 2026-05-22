import { useState } from "react";
import { useQuery, useMutation } from "@tanstack/react-query";
import { apiClient } from "../../api/client";
import { Card, CardContent, CardHeader, CardTitle } from "../../components/ui/card";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { Badge } from "../../components/ui/badge";

// ---- Types ----

interface PIIViolation {
  occurred_at: string;
  violation_type: string;
  action_taken: string;
}

interface QuotaRequest {
  id: string;
  requested_tokens: number;
  reason: string;
  status: string;
  review_note?: string;
  created_at: string;
}

// ---- API helpers ----

const fetchPIIViolations = (limit: number) =>
  apiClient.get<{ violations: PIIViolation[] }>(`/api/employee/pii-violations?limit=${limit}`).then((r) => r.data);

const fetchQuotaRequests = () =>
  apiClient.get<{ requests: QuotaRequest[] }>("/api/employee/quota-requests").then((r) => r.data);

const submitQuotaRequest = (payload: { requested_tokens: number; reason: string }) =>
  apiClient.post<{ id: string; status: string }>("/api/employee/quota-requests", payload);

const BASE_URL = import.meta.env.VITE_API_URL ?? "http://localhost:8081";

async function downloadUsageCSV(month: string): Promise<void> {
  const token = localStorage.getItem("totra_token");
  const res = await fetch(`${BASE_URL}/api/employee/usage-export?month=${month}`, {
    headers: { Authorization: `Bearer ${token ?? ""}` },
  });
  if (!res.ok) throw new Error("Export failed");
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `usage-${month}.csv`;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}

// ---- Status badge helper ----

function statusVariant(status: string): "default" | "secondary" | "outline" | "destructive" {
  switch (status) {
    case "approved": return "default";
    case "denied":
    case "rejected": return "destructive";
    default: return "secondary";
  }
}

// ---- Page ----

export function SelfServicePage() {
  // PII violations
  const piiLimit = 50;
  const { data: piiData, isLoading: piiLoading } = useQuery({
    queryKey: ["my-pii-violations", piiLimit],
    queryFn: () => fetchPIIViolations(piiLimit),
  });

  // Quota requests
  const { data: quotaData, refetch: refetchQuota, isLoading: quotaLoading } = useQuery({
    queryKey: ["my-quota-requests"],
    queryFn: fetchQuotaRequests,
  });

  // Submit quota request form state
  const [qForm, setQForm] = useState({ requested_tokens: "", reason: "" });

  const quotaMutation = useMutation({
    mutationFn: (p: { requested_tokens: number; reason: string }) => submitQuotaRequest(p),
    onSuccess: () => {
      setQForm({ requested_tokens: "", reason: "" });
      refetchQuota();
    },
  });

  // Usage export state
  const currentMonth = new Date().toISOString().slice(0, 7);
  const [exportMonth, setExportMonth] = useState(currentMonth);
  const [exportLoading, setExportLoading] = useState(false);
  const [exportError, setExportError] = useState<string | null>(null);

  const handleExport = async () => {
    setExportError(null);
    setExportLoading(true);
    try {
      await downloadUsageCSV(exportMonth);
    } catch {
      setExportError("Download failed. Please try again.");
    } finally {
      setExportLoading(false);
    }
  };

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Self-Service</h1>

      {/* ---- PII Violations ---- */}
      <Card>
        <CardHeader>
          <CardTitle>PII Violations</CardTitle>
        </CardHeader>
        <CardContent>
          {piiLoading ? (
            <p className="text-zinc-400 text-sm">Loading...</p>
          ) : !piiData?.violations?.length ? (
            <p className="text-zinc-500 text-sm">No violations found.</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-zinc-700 text-zinc-400 text-left">
                    <th className="pb-2 pr-6">Date</th>
                    <th className="pb-2 pr-6">Violation Type</th>
                    <th className="pb-2">Action</th>
                  </tr>
                </thead>
                <tbody>
                  {piiData.violations.map((v, i) => (
                    <tr key={i} className="border-b border-zinc-800">
                      <td className="py-2 pr-6 text-zinc-300">
                        {new Date(v.occurred_at).toLocaleString()}
                      </td>
                      <td className="py-2 pr-6 text-zinc-300 font-mono">{v.violation_type}</td>
                      <td className="py-2">
                        <Badge variant={v.action_taken === "blocked" ? "destructive" : "secondary"}>
                          {v.action_taken}
                        </Badge>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>

      {/* ---- Quota Requests ---- */}
      <Card>
        <CardHeader>
          <CardTitle>Quota Requests</CardTitle>
        </CardHeader>
        <CardContent className="space-y-6">
          {/* Submit form */}
          <form
            onSubmit={(e) => {
              e.preventDefault();
              const tokens = parseInt(qForm.requested_tokens, 10);
              if (!tokens || tokens <= 0 || !qForm.reason.trim()) return;
              quotaMutation.mutate({ requested_tokens: tokens, reason: qForm.reason.trim() });
            }}
            className="space-y-4 pb-4 border-b border-zinc-800"
          >
            <p className="text-sm font-medium text-zinc-300">Submit a new request</p>
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-1">
                <Label>Requested Tokens (SCU)</Label>
                <Input
                  type="number"
                  min="1"
                  placeholder="e.g. 500000"
                  value={qForm.requested_tokens}
                  onChange={(e) => setQForm({ ...qForm, requested_tokens: e.target.value })}
                  required
                />
              </div>
              <div className="space-y-1">
                <Label>Reason</Label>
                <textarea
                  className="w-full rounded-md border border-zinc-700 bg-zinc-800 px-3 py-2 text-sm text-zinc-100 placeholder-zinc-500 focus:outline-none focus:ring-2 focus:ring-indigo-500 resize-none"
                  rows={2}
                  placeholder="Explain why you need more quota"
                  value={qForm.reason}
                  onChange={(e) => setQForm({ ...qForm, reason: e.target.value })}
                  required
                />
              </div>
            </div>
            <Button type="submit" disabled={quotaMutation.isPending}>
              {quotaMutation.isPending ? "Submitting..." : "Submit Request"}
            </Button>
            {quotaMutation.isError && (
              <p className="text-red-400 text-xs">Failed to submit. Please try again.</p>
            )}
            {quotaMutation.isSuccess && (
              <p className="text-green-400 text-xs">Request submitted successfully.</p>
            )}
          </form>

          {/* Request history */}
          {quotaLoading ? (
            <p className="text-zinc-400 text-sm">Loading...</p>
          ) : !quotaData?.requests?.length ? (
            <p className="text-zinc-500 text-sm">No quota requests yet.</p>
          ) : (
            <div className="space-y-2">
              {quotaData.requests.map((r) => (
                <div
                  key={r.id}
                  className="flex items-start justify-between rounded-md border border-zinc-800 px-4 py-3"
                >
                  <div className="space-y-0.5">
                    <p className="text-sm text-zinc-200">
                      <span className="font-medium">{r.requested_tokens.toLocaleString()}</span> tokens
                    </p>
                    <p className="text-xs text-zinc-400">{r.reason}</p>
                    {r.review_note && (
                      <p className="text-xs text-zinc-500 italic">Note: {r.review_note}</p>
                    )}
                    <p className="text-xs text-zinc-600">{new Date(r.created_at).toLocaleDateString()}</p>
                  </div>
                  <Badge variant={statusVariant(r.status)} className="capitalize ml-4 shrink-0">
                    {r.status}
                  </Badge>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {/* ---- Usage Export ---- */}
      <Card>
        <CardHeader>
          <CardTitle>Usage Export</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-zinc-400 mb-4">
            Download a CSV of your usage for a given month.
          </p>
          <div className="flex items-end gap-4">
            <div className="space-y-1">
              <Label>Month</Label>
              <Input
                type="month"
                value={exportMonth}
                max={currentMonth}
                onChange={(e) => setExportMonth(e.target.value)}
                className="w-48"
              />
            </div>
            <Button onClick={handleExport} disabled={exportLoading || !exportMonth}>
              {exportLoading ? "Downloading..." : "Download CSV"}
            </Button>
          </div>
          {exportError && <p className="text-red-400 text-xs mt-2">{exportError}</p>}
        </CardContent>
      </Card>
    </div>
  );
}
