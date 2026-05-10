import { useState } from "react";
import { useQuery, useMutation } from "@tanstack/react-query";
import { getKPISnapshots, triggerKPISnapshot } from "../../api/client";
import type { EfficiencySnapshot } from "../../api/client";
import { Card, CardContent, CardHeader, CardTitle } from "../../components/ui/card";
import { Button } from "../../components/ui/button";
import { Badge } from "../../components/ui/badge";

const currentMonth = new Date().toISOString().slice(0, 7);

function groupByPeerGroup(snapshots: EfficiencySnapshot[]) {
  const groups: Record<string, EfficiencySnapshot[]> = {};
  for (const s of snapshots) {
    if (!groups[s.peer_group]) groups[s.peer_group] = [];
    groups[s.peer_group].push(s);
  }
  return groups;
}

export function KpiPage() {
  const [month, setMonth] = useState(currentMonth);
  const [tab, setTab] = useState<"regular" | "cohort">("regular");

  const { data, isLoading, refetch } = useQuery({
    queryKey: ["kpi-snapshots", month],
    queryFn: () => getKPISnapshots(month).then((r) => r.data),
  });

  const triggerMutation = useMutation({
    mutationFn: () => triggerKPISnapshot(month),
    onSuccess: () => refetch(),
  });

  const allSnapshots = data?.snapshots ?? [];
  const regularSnapshots = allSnapshots.filter((s) => !s.peer_group.startsWith("cohort_"));
  const cohortSnapshots = allSnapshots.filter((s) => s.peer_group.startsWith("cohort_"));
  const displayed = tab === "regular" ? groupByPeerGroup(regularSnapshots) : groupByPeerGroup(cohortSnapshots);

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">KPI Leaderboard</h1>
        <div className="flex items-center gap-3">
          <input
            type="month"
            value={month}
            onChange={(e) => setMonth(e.target.value)}
            className="h-10 rounded-md border border-zinc-700 bg-zinc-800 px-3 text-sm text-zinc-100"
          />
          <Button
            variant="outline"
            onClick={() => triggerMutation.mutate()}
            disabled={triggerMutation.isPending}
          >
            {triggerMutation.isPending ? "Computing..." : "Run Snapshot"}
          </Button>
        </div>
      </div>

      <div className="flex gap-2">
        {(["regular", "cohort"] as const).map((t) => (
          <button
            key={t}
            onClick={() => setTab(t)}
            className={`px-4 py-2 rounded-md text-sm font-medium transition-colors ${
              tab === t ? "bg-indigo-600 text-white" : "bg-zinc-800 text-zinc-400 hover:text-zinc-100"
            }`}
          >
            {t === "regular" ? "Established Employees" : "New Employees (Cohort)"}
          </button>
        ))}
      </div>

      {isLoading ? (
        <p className="text-zinc-500 text-sm">Loading...</p>
      ) : Object.keys(displayed).length === 0 ? (
        <p className="text-zinc-500 text-sm py-8 text-center">
          No data for {month}. Click "Run Snapshot" to compute.
        </p>
      ) : (
        Object.entries(displayed).map(([group, snapshots]) => (
          <Card key={group}>
            <CardHeader>
              <CardTitle className="capitalize">{group.replace("cohort_", "Cohort: ")}</CardTitle>
            </CardHeader>
            <CardContent>
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-zinc-800 text-zinc-400">
                    <th className="text-left py-2 font-medium">Rank</th>
                    <th className="text-left py-2 font-medium">Employee</th>
                    <th className="text-right py-2 font-medium">Efficiency Score</th>
                    <th className="text-right py-2 font-medium">Output Weight</th>
                    <th className="text-right py-2 font-medium">SCU Used</th>
                    <th className="text-right py-2 font-medium">Peer Rank</th>
                  </tr>
                </thead>
                <tbody>
                  {snapshots.map((s) => (
                    <tr key={s.id} className="border-b border-zinc-800/50">
                      <td className="py-2 font-bold text-indigo-400">#{s.rank}</td>
                      <td>{s.user_name}</td>
                      <td className="text-right font-mono">{s.efficiency_score.toFixed(2)}</td>
                      <td className="text-right">{s.total_output_weight.toFixed(1)}</td>
                      <td className="text-right">{s.total_scu.toLocaleString()}</td>
                      <td className="text-right">
                        <Badge variant="outline">{s.rank} / {s.peer_count}</Badge>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </CardContent>
          </Card>
        ))
      )}
    </div>
  );
}
