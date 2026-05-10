import { useState } from "react";
import { useQuery, useMutation } from "@tanstack/react-query";
import { getMonthlySummary, getMyKPI, getMyFuel, getMyIntegrations, getMyQuota, bindIntegration, apiClient, getMyUID, getMyProfile, updateMyProfile, getMyKPISubmetrics } from "../../api/client";
import type { AIQSubmetrics } from "../../api/client";
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
  const { data: kpiData } = useQuery({
    queryKey: ["my-kpi"],
    queryFn: () => getMyKPI().then((r) => r.data),
  });
  const { data: submetricsData } = useQuery({
    queryKey: ["my-kpi-submetrics", currentMonth],
    queryFn: () => getMyKPISubmetrics(currentMonth).then((r) => r.data),
  });
  const submetrics: AIQSubmetrics | null = submetricsData?.metrics ?? null;
  const { data: fuelData } = useQuery({
    queryKey: ["my-fuel"],
    queryFn: () => getMyFuel().then((r) => r.data),
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

  const uid = getMyUID();
  const mySummary = summaryData?.summaries?.find((s) => s.user_id === uid);
  const usedSCU = quotaData?.used_scu ?? mySummary?.total_scu ?? 0;
  const quotaSCU = quotaData?.quota_scu ?? 0;
  const latestSnapshot = kpiData?.snapshots?.[0];
  const isNewEmployee = latestSnapshot?.peer_group?.startsWith("cohort_");

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

      {latestSnapshot && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              Efficiency Score
              {isNewEmployee && <Badge variant="secondary">New Employee Period</Badge>}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex items-baseline gap-3 mb-4">
              <p className="text-4xl font-bold text-indigo-400">{latestSnapshot.efficiency_score.toFixed(2)}</p>
              <p className="text-zinc-400 text-sm">
                {isNewEmployee ? "Cohort" : "Peer"} rank:{" "}
                <span className="text-zinc-100 font-medium">{latestSnapshot.rank} / {latestSnapshot.peer_count}</span>
              </p>
            </div>
            {latestSnapshot.aiq_score > 0 && (
              <div className="grid grid-cols-3 gap-3 mt-4 pt-4 border-t border-zinc-800">
                <div className="text-center">
                  <p className="text-xs text-zinc-500 mb-1">AI Quality</p>
                  <p className="text-lg font-semibold text-indigo-400">{latestSnapshot.aiq_score.toFixed(1)}</p>
                </div>
                <div className="text-center">
                  <p className="text-xs text-zinc-500 mb-1">Output</p>
                  <p className="text-lg font-semibold text-emerald-400">{latestSnapshot.oss_score.toFixed(2)}</p>
                </div>
                <div className="text-center">
                  <p className="text-xs text-zinc-500 mb-1">Growth</p>
                  <p className={`text-lg font-semibold ${latestSnapshot.gts_score >= 0 ? "text-green-400" : "text-red-400"}`}>
                    {latestSnapshot.gts_score >= 0 ? "+" : ""}{(latestSnapshot.gts_score * 100).toFixed(1)}%
                  </p>
                </div>
              </div>
            )}
            {submetrics && (
              <div className="mt-4 pt-4 border-t border-zinc-800">
                <p className="text-xs text-zinc-500 mb-3">AIQ Sub-metrics — {currentMonth}</p>
                <div className="grid grid-cols-2 gap-2 text-xs">
                  <div className="bg-zinc-800/50 rounded p-2">
                    <p className="text-zinc-500 mb-1">Output Density</p>
                    <p className="font-mono font-semibold">{submetrics.output_density.toFixed(2)}</p>
                    <p className="text-zinc-600 mt-0.5">completion / prompt ratio</p>
                  </div>
                  <div className="bg-zinc-800/50 rounded p-2">
                    <p className="text-zinc-500 mb-1">Usage Consistency</p>
                    <p className="font-mono font-semibold">{(submetrics.usage_consistency * 100).toFixed(0)}%</p>
                    <p className="text-zinc-600 mt-0.5">{submetrics.active_days} / {submetrics.working_days} working days</p>
                  </div>
                  <div className="bg-zinc-800/50 rounded p-2">
                    <p className="text-zinc-500 mb-1">Task Depth</p>
                    <p className="font-mono font-semibold">{submetrics.task_depth.toFixed(1)}</p>
                    <p className="text-zinc-600 mt-0.5">turns × log(tokens)</p>
                  </div>
                  <div className="bg-zinc-800/50 rounded p-2">
                    <p className="text-zinc-500 mb-1">Cost Efficiency</p>
                    <p className="font-mono font-semibold">{submetrics.cost_efficiency.toFixed(0)}</p>
                    <p className="text-zinc-600 mt-0.5">completion tokens / SCU</p>
                  </div>
                </div>
              </div>
            )}
            {kpiData && kpiData.snapshots.length > 1 && (() => {
              const pts = [...kpiData.snapshots].reverse();
              const maxScore = Math.max(...pts.map((x) => x.efficiency_score), 0.01);
              const W = 300, chartH = 56, labelH = 18, totalH = chartH + labelH;
              const colW = W / pts.length;
              const coords = pts.map((s, i) => {
                const bh = Math.max(2, (s.efficiency_score / maxScore) * chartH);
                return { s, cx: i * colW + colW / 2, cy: chartH - bh, bh };
              });
              const linePts = coords.map((c) => `${c.cx},${c.cy}`).join(" ");
              return (
                <div className="mt-2">
                  <p className="text-xs text-zinc-500 mb-2">Growth curve (last 12 months)</p>
                  <svg viewBox={`0 0 ${W} ${totalH}`} width="100%" className="overflow-visible">
                    {coords.map(({ s, cy, bh }, i) => (
                      <rect key={s.year_month} x={i * colW + 3} y={cy} width={colW - 6}
                        height={bh} rx="2" fill="rgb(79,70,229)" fillOpacity="0.55" />
                    ))}
                    <polyline points={linePts} fill="none" stroke="rgb(129,140,248)"
                      strokeWidth="1.8" strokeLinejoin="round" strokeLinecap="round" />
                    {coords.map(({ s, cx, cy }) => (
                      <circle key={s.year_month + "-d"} cx={cx} cy={cy} r="2.5"
                        fill="rgb(165,180,252)" />
                    ))}
                    {coords.map(({ s, cx }) => (
                      <text key={s.year_month + "-l"} x={cx} y={totalH} textAnchor="middle"
                        fontSize="9" fill="rgb(82,82,91)">{s.year_month.slice(5)}</text>
                    ))}
                  </svg>
                </div>
              );
            })()}
          </CardContent>
        </Card>
      )}

      {fuelData && fuelData.transactions?.length > 0 && (
        <Card>
          <CardHeader><CardTitle>AI-Fuel Rewards</CardTitle></CardHeader>
          <CardContent>
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-zinc-800 text-zinc-400">
                  <th className="text-left py-2 font-medium">Date</th>
                  <th className="text-left py-2 font-medium">Tier</th>
                  <th className="text-right py-2 font-medium">SCU Awarded</th>
                </tr>
              </thead>
              <tbody>
                {fuelData.transactions.map((t) => (
                  <tr key={t.id} className="border-b border-zinc-800/50">
                    <td className="py-2 text-zinc-400">{t.created_at.slice(0, 10)}</td>
                    <td><Badge variant="outline">{t.tier || t.reason}</Badge></td>
                    <td className="text-right text-green-400">+{t.amount_scu.toLocaleString()}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </CardContent>
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
