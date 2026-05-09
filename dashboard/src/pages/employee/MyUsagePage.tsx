import { useState } from "react";
import { useQuery, useMutation } from "@tanstack/react-query";
import { getMonthlySummary, apiClient } from "../../api/client";
import { Card, CardContent, CardHeader, CardTitle } from "../../components/ui/card";
import { QuotaMeter } from "../../components/QuotaMeter";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "../../components/ui/dialog";

const currentMonth = new Date().toISOString().slice(0, 7);

function useMyStats() {
  return useQuery({
    queryKey: ["my-summary", currentMonth],
    queryFn: () => getMonthlySummary(currentMonth).then((r) => r.data),
  });
}

function useMyQuota() {
  return useQuery({
    queryKey: ["my-quota"],
    queryFn: () => apiClient.get<{ quota_scu: number; used_scu: number }>("/api/me/quota").then((r) => r.data),
  });
}

export function MyUsagePage() {
  const { data: summaryData } = useMyStats();
  const { data: quotaData } = useMyQuota();
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({ new_quota: "", reason: "" });

  const requestMutation = useMutation({
    mutationFn: (payload: { new_quota: number; reason: string }) =>
      apiClient.post("/api/quota/request", payload),
    onSuccess: () => {
      setOpen(false);
      setForm({ new_quota: "", reason: "" });
    },
  });

  const handleRequest = (e: React.FormEvent) => {
    e.preventDefault();
    requestMutation.mutate({ new_quota: parseInt(form.new_quota), reason: form.reason });
  };

  const mySummary = summaryData?.summaries?.[0];
  const usedSCU = mySummary?.total_scu ?? 0;
  const quotaSCU = quotaData?.quota_scu ?? 0;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">My Usage — {currentMonth}</h1>
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogTrigger asChild>
            <Button variant="outline">Request Quota Increase</Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Request Quota Increase</DialogTitle>
            </DialogHeader>
            <form onSubmit={handleRequest} className="space-y-4">
              <div className="space-y-1">
                <Label>Requested SCU Limit</Label>
                <Input
                  type="number"
                  min="1"
                  placeholder="e.g. 50000"
                  value={form.new_quota}
                  onChange={(e) => setForm({ ...form, new_quota: e.target.value })}
                  required
                />
              </div>
              <div className="space-y-1">
                <Label>Reason</Label>
                <Input
                  placeholder="Briefly explain your use case"
                  value={form.reason}
                  onChange={(e) => setForm({ ...form, reason: e.target.value })}
                />
              </div>
              <Button type="submit" className="w-full" disabled={requestMutation.isPending}>
                {requestMutation.isPending ? "Submitting..." : "Submit Request"}
              </Button>
            </form>
          </DialogContent>
        </Dialog>
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

      {quotaSCU > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Monthly Quota</CardTitle>
          </CardHeader>
          <CardContent>
            <QuotaMeter used={usedSCU} limit={quotaSCU} />
          </CardContent>
        </Card>
      )}
    </div>
  );
}
