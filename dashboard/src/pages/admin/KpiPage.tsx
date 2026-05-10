import { useState, Fragment } from "react";
import { useQuery, useMutation } from "@tanstack/react-query";
import { getKPISnapshots, triggerKPISnapshot, getKPIUserHistory } from "../../api/client";
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

function GrowthChart({ userID }: { userID: string }) {
  const { data, isLoading } = useQuery({
    queryKey: ["kpi-user-history", userID],
    queryFn: () => getKPIUserHistory(userID).then((r) => r.data),
  });

  if (isLoading) return <p className="text-xs text-zinc-500 py-2">Loading...</p>;
  const snapshots = data?.snapshots ?? [];
  if (snapshots.length === 0) return <p className="text-xs text-zinc-500 py-2">No historical data yet.</p>;

  const sorted = [...snapshots].reverse();
  const maxScore = Math.max(...sorted.map((s) => s.efficiency_score), 0.01);

  return (
    <div className="px-4 py-3 bg-zinc-900/50 border-t border-zinc-800">
      <p className="text-xs text-zinc-500 mb-2">Growth curve (last {sorted.length} months)</p>
      <div className="flex items-end gap-1 h-14">
        {sorted.map((s) => {
          const h = Math.max(Math.round((s.efficiency_score / maxScore) * 100), 2);
          return (
            <div key={s.year_month} className="flex flex-col items-center gap-1 flex-1 min-w-0">
              <div
                className="w-full bg-indigo-600 rounded-sm"
                style={{ height: `${h}%` }}
                title={`${s.year_month}: ${s.efficiency_score.toFixed(2)}`}
              />
              <span className="text-zinc-600 text-xs truncate w-full text-center">{s.year_month.slice(5)}</span>
            </div>
          );
        })}
      </div>
    </div>
  );
}

export function KpiPage() {
  const [month, setMonth] = useState(currentMonth);
  const [tab, setTab] = useState<"regular" | "cohort">("regular");
  const [expandedUser, setExpandedUser] = useState<string | null>(null);

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

  const toggleUser = (userID: string) =>
    setExpandedUser((prev) => (prev === userID ? null : userID));

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
            <CardContent className="p-0">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-zinc-800 text-zinc-400">
                    <th className="text-left py-2 px-4 font-medium">Rank</th>
                    <th className="text-left py-2 font-medium">Employee</th>
                    <th className="text-right py-2 font-medium">Score</th>
                    <th className="text-right py-2 font-medium text-indigo-400">AIQ</th>
                    <th className="text-right py-2 font-medium text-emerald-400">OSS</th>
                    <th className="text-right py-2 font-medium text-green-400">GTS</th>
                    <th className="text-right py-2 font-medium">SCU</th>
                    <th className="text-right py-2 px-4 font-medium">Peer Rank</th>
                  </tr>
                </thead>
                <tbody>
                  {snapshots.map((s) => {
                    const hasScores = s.aiq_score > 0 || s.oss_score > 0;
                    const gtsSign = s.gts_score >= 0 ? "+" : "";
                    return (
                    <Fragment key={s.user_id}>
                      <tr
                        onClick={() => toggleUser(s.user_id)}
                        className={`border-b border-zinc-800/50 cursor-pointer transition-colors ${s.anomaly_flagged ? "bg-red-950/30 hover:bg-red-900/30" : "hover:bg-zinc-800/30"}`}
                      >
                        <td className="py-2 px-4 font-bold text-indigo-400">#{s.rank}</td>
                        <td className="py-2">
                          <span>{s.user_name}</span>
                          {s.anomaly_flagged && (
                            <Badge variant="destructive" className="ml-2 text-xs">⚠ Review</Badge>
                          )}
                          <span className="ml-2 text-zinc-600 text-xs">
                            {expandedUser === s.user_id ? "▲" : "▼"}
                          </span>
                        </td>
                        <td className="py-2 text-right font-mono">{s.efficiency_score.toFixed(2)}</td>
                        <td className="py-2 text-right font-mono text-indigo-400">
                          {hasScores ? s.aiq_score.toFixed(1) : <span className="text-zinc-600">—</span>}
                        </td>
                        <td className="py-2 text-right font-mono text-emerald-400">
                          {hasScores ? s.oss_score.toFixed(2) : <span className="text-zinc-600">—</span>}
                        </td>
                        <td className="py-2 text-right font-mono">
                          {hasScores
                            ? <span className={s.gts_score >= 0 ? "text-green-400" : "text-red-400"}>
                                {gtsSign}{(s.gts_score * 100).toFixed(1)}%
                              </span>
                            : <span className="text-zinc-600">—</span>}
                        </td>
                        <td className="py-2 text-right">{s.total_scu.toLocaleString()}</td>
                        <td className="py-2 px-4 text-right">
                          <Badge variant="outline">{s.rank} / {s.peer_count}</Badge>
                        </td>
                      </tr>
                      {expandedUser === s.user_id && (
                        <tr>
                          <td colSpan={8} className="p-0">
                            <GrowthChart userID={s.user_id} />
                          </td>
                        </tr>
                      )}
                    </Fragment>
                  );
                  })}
                </tbody>
              </table>
            </CardContent>
          </Card>
        ))
      )}
    </div>
  );
}
