import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { getDepartmentSummary, exportDepartmentCSV } from "../../api/client";
import type { DeptSummary } from "../../api/client";
import { Card, CardContent, CardHeader, CardTitle } from "../../components/ui/card";
import { Button } from "../../components/ui/button";

const currentMonth = new Date().toISOString().slice(0, 7);

export function DepartmentReportPage() {
  const [month, setMonth] = useState(currentMonth);
  const [exporting, setExporting] = useState(false);

  const { data, isLoading } = useQuery({
    queryKey: ["department-summary", month],
    queryFn: () => getDepartmentSummary(month).then((r) => r.data),
  });

  const departments: DeptSummary[] = data?.departments ?? [];
  const totalSCU = departments.reduce((s, d) => s + d.total_scu, 0);
  const totalUSD = departments.reduce((s, d) => s + d.total_usd, 0);

  async function handleExport() {
    setExporting(true);
    try {
      await exportDepartmentCSV(month);
    } finally {
      setExporting(false);
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Department Cost Report</h1>
        <div className="flex items-center gap-3">
          <input
            type="month"
            value={month}
            onChange={(e) => setMonth(e.target.value)}
            className="h-10 rounded-md border border-zinc-700 bg-zinc-800 px-3 text-sm text-zinc-100"
          />
          <Button
            variant="outline"
            onClick={handleExport}
            disabled={exporting || departments.length === 0}
          >
            {exporting ? "Exporting..." : "Export CSV"}
          </Button>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-4">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm text-zinc-400 font-normal">Total SCU</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-3xl font-bold">
              {totalSCU.toLocaleString(undefined, { maximumFractionDigits: 0 })}
            </p>
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
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Cost by Department — {month}</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {isLoading ? (
            <p className="text-zinc-500 text-sm p-4">Loading...</p>
          ) : departments.length === 0 ? (
            <p className="text-zinc-500 text-sm p-4 text-center">No data for {month}.</p>
          ) : (
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-zinc-800 text-zinc-400">
                  <th className="text-left py-2 px-4 font-medium">Department</th>
                  <th className="text-right py-2 font-medium">Users</th>
                  <th className="text-right py-2 font-medium">Active</th>
                  <th className="text-right py-2 font-medium">SCU</th>
                  <th className="text-right py-2 font-medium">Cost (USD)</th>
                  <th className="text-right py-2 px-4 font-medium">% of Total</th>
                </tr>
              </thead>
              <tbody>
                {departments.map((d) => (
                  <tr key={d.department} className="border-b border-zinc-800/50 hover:bg-zinc-800/30">
                    <td className="py-2 px-4 font-medium">{d.department}</td>
                    <td className="py-2 text-right text-zinc-400">{d.user_count}</td>
                    <td className="py-2 text-right text-emerald-400">{d.active_users}</td>
                    <td className="py-2 text-right font-mono">
                      {d.total_scu.toLocaleString(undefined, { maximumFractionDigits: 0 })}
                    </td>
                    <td className="py-2 text-right font-mono">${d.total_usd.toFixed(2)}</td>
                    <td className="py-2 px-4 text-right text-zinc-400">
                      {totalSCU > 0 ? `${((d.total_scu / totalSCU) * 100).toFixed(1)}%` : "—"}
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
