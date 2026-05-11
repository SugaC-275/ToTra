import { useQuery } from "@tanstack/react-query";
import { getMonthlySummary, getAdoptionRate, getBudgetForecast, getInactiveUsers } from "../../api/client";
import type { BudgetForecast, InactiveUser } from "../../api/client";
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

  const { data: forecastData } = useQuery({
    queryKey: ["budget-forecast", currentMonth],
    queryFn: () => getBudgetForecast(currentMonth).then((r) => r.data),
  });

  const { data: inactiveData } = useQuery({
    queryKey: ["inactive-users", currentMonth],
    queryFn: () => getInactiveUsers(currentMonth, 3).then((r) => r.data),
  });

  const forecast: BudgetForecast | undefined = forecastData;
  const inactiveUsers: InactiveUser[] = inactiveData?.users ?? [];

  const totalSCU = summaryData?.summaries?.reduce((s, u) => s + u.total_scu, 0) ?? 0;
  const totalUSD = summaryData?.summaries?.reduce((s, u) => s + u.total_usd, 0) ?? 0;

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Dashboard — {currentMonth}</h1>

      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
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
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm text-zinc-400 font-normal">Month-end Forecast</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-3xl font-bold">
              {forecast
                ? forecast.projected_scu.toLocaleString(undefined, { maximumFractionDigits: 0 })
                : "—"}
              <span className="text-sm font-normal text-zinc-500 ml-1">SCU</span>
            </p>
            {forecast && (
              <p className={`text-xs mt-1 ${forecast.trend_pct >= 0 ? "text-red-400" : "text-emerald-400"}`}>
                {forecast.trend_pct >= 0 ? "↑" : "↓"} {Math.abs(forecast.trend_pct).toFixed(1)}% vs last month
                <span className="text-zinc-500 ml-1">
                  ({forecast.days_elapsed}/{forecast.days_in_month} days)
                </span>
              </p>
            )}
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

      {inactiveUsers.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              Low Activity Employees
              <span className="text-sm font-normal text-zinc-400">
                (&lt;3 active days this month)
              </span>
            </CardTitle>
          </CardHeader>
          <CardContent className="p-0">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-zinc-800 text-zinc-400">
                  <th className="text-left py-2 px-4 font-medium">Employee</th>
                  <th className="text-left py-2 font-medium">Department</th>
                  <th className="text-left py-2 font-medium">Role</th>
                  <th className="text-right py-2 font-medium">Active Days</th>
                  <th className="text-right py-2 px-4 font-medium">Last Active</th>
                </tr>
              </thead>
              <tbody>
                {inactiveUsers.map((u) => (
                  <tr key={u.user_id} className="border-b border-zinc-800/50">
                    <td className="py-2 px-4">
                      <p>{u.name}</p>
                      <p className="text-zinc-500 text-xs">{u.email}</p>
                    </td>
                    <td className="py-2 text-zinc-400">{u.department || "—"}</td>
                    <td className="py-2 text-zinc-400">{u.job_role || "—"}</td>
                    <td className="py-2 text-right">
                      <span className={u.active_days === 0 ? "text-red-400" : "text-yellow-400"}>
                        {u.active_days}
                      </span>
                    </td>
                    <td className="py-2 px-4 text-right text-zinc-500">
                      {u.last_active_at ?? "Never"}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
