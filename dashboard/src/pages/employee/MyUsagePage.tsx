import { useState } from "react";
import { useQuery, useMutation } from "@tanstack/react-query";
import { getMonthlySummary, getMyIntegrations, getMyQuota, bindIntegration, apiClient, getMyUID, getMyProfile, updateMyProfile, downloadMyDataExport, createDeletionRequest } from "../../api/client";
import { Card, CardContent, CardHeader, CardTitle } from "../../components/ui/card";
import { QuotaMeter } from "../../components/QuotaMeter";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { Badge } from "../../components/ui/badge";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "../../components/ui/dialog";

const currentMonth = new Date().toISOString().slice(0, 7);

export function MyUsagePage() {
  const { data: summaryData } = useQuery({
    queryKey: ["my-summary", currentMonth],
    queryFn: () => getMonthlySummary(currentMonth).then((r) => r.data),
  });
  const { data: quotaData } = useQuery({
    queryKey: ["my-quota"],
    queryFn: () => getMyQuota().then((r) => r.data),
  });
  const { data: intData, refetch: refetchInt } = useQuery({
    queryKey: ["my-integrations"],
    queryFn: () => getMyIntegrations().then((r) => r.data),
  });

  const { data: profileData, refetch: refetchProfile } = useQuery({
    queryKey: ["my-profile"],
    queryFn: () => getMyProfile().then((r) => r.data),
  });

  const [jobRoleInput, setJobRoleInput] = useState("");
  const profileMutation = useMutation({
    mutationFn: (jr: string) => updateMyProfile({ job_role: jr, department: "" }),
    onSuccess: () => refetchProfile(),
  });

  const [quotaOpen, setQuotaOpen] = useState(false);
  const [bindOpen, setBindOpen] = useState(false);
  const [quotaForm, setQuotaForm] = useState({ new_quota: "", reason: "" });
  const [bindForm, setBindForm] = useState({ platform: "github", external_id: "" });

  const requestMutation = useMutation({
    mutationFn: (payload: { new_quota: number; reason: string }) =>
      apiClient.post("/api/quota/request", payload),
    onSuccess: () => {
      setQuotaOpen(false);
      setQuotaForm({ new_quota: "", reason: "" });
    },
  });

  const bindMutation = useMutation({
    mutationFn: () => bindIntegration(bindForm.platform, bindForm.external_id),
    onSuccess: () => {
      setBindOpen(false);
      setBindForm({ platform: "github", external_id: "" });
      refetchInt();
    },
  });

  const [exportLoading, setExportLoading] = useState(false);
  const deletionMutation = useMutation({
    mutationFn: createDeletionRequest,
  });

  const uid = getMyUID();
  const mySummary = summaryData?.summaries?.find((s) => s.user_id === uid);
  const usedSCU = quotaData?.used_scu ?? mySummary?.total_scu ?? 0;
  const quotaSCU = quotaData?.quota_scu ?? 0;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">My Usage — {currentMonth}</h1>
        <div className="flex gap-2">
          <Dialog open={bindOpen} onOpenChange={setBindOpen}>
            <DialogTrigger asChild>
              <Button variant="outline">Link Account</Button>
            </DialogTrigger>
            <DialogContent>
              <DialogHeader><DialogTitle>Link Third-Party Account</DialogTitle></DialogHeader>
              <div className="space-y-4">
                <div className="space-y-1">
                  <Label>Platform</Label>
                  <select
                    className="w-full h-10 rounded-md border border-zinc-700 bg-zinc-800 px-3 py-2 text-sm text-zinc-100"
                    value={bindForm.platform}
                    onChange={(e) => setBindForm({ ...bindForm, platform: e.target.value })}
                  >
                    {["github", "jira", "feishu", "dingtalk"].map((p) => (
                      <option key={p} value={p}>{p}</option>
                    ))}
                  </select>
                </div>
                <div className="space-y-1">
                  <Label>Username / Account ID</Label>
                  <Input
                    placeholder="e.g. alice-gh (GitHub login)"
                    value={bindForm.external_id}
                    onChange={(e) => setBindForm({ ...bindForm, external_id: e.target.value })}
                  />
                </div>
                <Button className="w-full" disabled={bindMutation.isPending} onClick={() => bindMutation.mutate()}>
                  {bindMutation.isPending ? "Linking..." : "Link Account"}
                </Button>
              </div>
            </DialogContent>
          </Dialog>
          <Dialog open={quotaOpen} onOpenChange={setQuotaOpen}>
            <DialogTrigger asChild>
              <Button variant="outline">Request Quota</Button>
            </DialogTrigger>
            <DialogContent>
              <DialogHeader><DialogTitle>Request Quota Increase</DialogTitle></DialogHeader>
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  requestMutation.mutate({ new_quota: parseInt(quotaForm.new_quota), reason: quotaForm.reason });
                }}
                className="space-y-4"
              >
                <div className="space-y-1">
                  <Label>Requested SCU Limit</Label>
                  <Input
                    type="number"
                    min="1"
                    value={quotaForm.new_quota}
                    onChange={(e) => setQuotaForm({ ...quotaForm, new_quota: e.target.value })}
                    required
                  />
                </div>
                <div className="space-y-1">
                  <Label>Reason</Label>
                  <Input
                    value={quotaForm.reason}
                    onChange={(e) => setQuotaForm({ ...quotaForm, reason: e.target.value })}
                  />
                </div>
                <Button type="submit" className="w-full" disabled={requestMutation.isPending}>
                  {requestMutation.isPending ? "Submitting..." : "Submit"}
                </Button>
              </form>
            </DialogContent>
          </Dialog>
          <Button
            variant="outline"
            disabled={exportLoading}
            onClick={async () => {
              setExportLoading(true);
              try { await downloadMyDataExport(); } finally { setExportLoading(false); }
            }}
          >
            {exportLoading ? "Exporting..." : "Export My Data"}
          </Button>
          <Button
            variant="outline"
            disabled={deletionMutation.isPending || deletionMutation.isSuccess}
            onClick={() => {
              if (confirm("Request deletion of all your personal data? This cannot be undone once approved.")) {
                deletionMutation.mutate();
              }
            }}
          >
            {deletionMutation.isSuccess ? "Deletion Requested" : deletionMutation.isPending ? "Requesting..." : "Request Data Deletion"}
          </Button>
        </div>
      </div>

      <div className="grid grid-cols-3 gap-4">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm text-zinc-400 font-normal">SCU Used</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-3xl font-bold">{usedSCU.toLocaleString()}</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm text-zinc-400 font-normal">Cost (USD)</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-3xl font-bold">${(mySummary?.total_usd ?? 0).toFixed(4)}</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm text-zinc-400 font-normal">Requests</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-3xl font-bold">{mySummary?.request_count ?? 0}</p>
          </CardContent>
        </Card>
      </div>

      {profileData && !profileData.job_role && (
        <Card className="border-indigo-800 bg-indigo-950/30">
          <CardHeader className="pb-2">
            <CardTitle className="text-sm">Tell us your role</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-xs text-zinc-400 mb-3">
              Your job role helps us compare you fairly with peers in the same function.
            </p>
            <div className="flex gap-2">
              <Input
                placeholder="e.g. engineer, pm, designer, ops"
                value={jobRoleInput}
                onChange={(e) => setJobRoleInput(e.target.value)}
                className="flex-1"
              />
              <Button
                size="sm"
                disabled={!jobRoleInput || profileMutation.isPending}
                onClick={() => profileMutation.mutate(jobRoleInput)}
              >
                Save
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      {quotaSCU > 0 && (
        <Card>
          <CardHeader><CardTitle>Monthly Quota</CardTitle></CardHeader>
          <CardContent><QuotaMeter used={usedSCU} limit={quotaSCU} /></CardContent>
        </Card>
      )}

      {intData && (
        <Card>
          <CardHeader><CardTitle>Linked Accounts</CardTitle></CardHeader>
          <CardContent>
            {!intData.integrations?.length ? (
              <p className="text-zinc-500 text-sm">No accounts linked. Click "Link Account" to connect GitHub/Jira/飞书.</p>
            ) : (
              <div className="flex flex-wrap gap-2">
                {intData.integrations.map((i) => (
                  <Badge key={i.id} variant="secondary" className="capitalize">
                    {i.platform}: {i.external_id}
                  </Badge>
                ))}
              </div>
            )}
          </CardContent>
        </Card>
      )}

    </div>
  );
}
