import { useQuery } from "@tanstack/react-query";
import { getMonthlySummary, getAdoptionRate } from "../../api/client";
import { Card, CardContent, CardHeader, CardTitle } from "../../components/ui/card";
import { UsageChart } from "../../components/UsageChart";

const currentMonth = new Date().toISOString().slice(0, 7);

export function DashboardPage() {
  const { data: summaryData, isLoading } = useQuery({
    queryKey: ["usage-summary", currentMonth],
    queryFn: () => getMonthlySummary(currentMonth).then((r) => r.data),
  });

  const { data: adoptionData } = useQuery({
    queryKey: ["adoption", currentMonth],
    queryFn: () => getAdoptionRate(currentMonth).then((r) => r.data),
  });

  const totalSCU = summaryData?.summaries?.reduce((s, u) => s + u.total_scu, 0) ?? 0;
  const totalUSD = summaryData?.summaries?.reduce((s, u) => s + u.total_usd, 0) ?? 0;

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Dashboard — {currentMonth}</h1>

      <div className="grid grid-cols-3 gap-4">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm text-zinc-400 font-normal">Total SCU Used</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-3xl font-bold">{totalSCU.toLocaleString()}</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm text-zinc-400 font-normal">Total Cost</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-3xl font-bold">${totalUSD.toFixed(2)}</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm text-zinc-400 font-normal">AI Adoption Rate</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-3xl font-bold">
              {adoptionData ? `${(adoptionData.adoption_rate * 100).toFixed(0)}%` : "—"}
            </p>
            <p className="text-xs text-zinc-500 mt-1">
              {adoptionData?.active_users ?? 0} / {adoptionData?.total_users ?? 0} employees
            </p>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Top 10 Users by SCU</CardTitle>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <p className="text-zinc-500 text-sm">Loading...</p>
          ) : (
            <UsageChart data={summaryData?.summaries ?? []} />
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Usage Details</CardTitle>
        </CardHeader>
        <CardContent>
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-zinc-800 text-zinc-400">
                <th className="text-left py-2 font-medium">Employee</th>
                <th className="text-right py-2 font-medium">SCU</th>
                <th className="text-right py-2 font-medium">Cost (USD)</th>
                <th className="text-right py-2 font-medium">Requests</th>
              </tr>
            </thead>
            <tbody>
              {summaryData?.summaries?.map((u) => (
                <tr key={u.user_id} className="border-b border-zinc-800/50">
                  <td className="py-2">{u.user_name}</td>
                  <td className="text-right">{u.total_scu.toLocaleString()}</td>
                  <td className="text-right">${u.total_usd.toFixed(4)}</td>
                  <td className="text-right">{u.request_count}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </CardContent>
      </Card>
    </div>
  );
}
