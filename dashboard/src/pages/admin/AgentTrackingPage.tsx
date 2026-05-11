import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { getAdminAgentSessions } from "../../api/client";
import type { AgentSession } from "../../api/client";
import { Card, CardContent, CardHeader, CardTitle } from "../../components/ui/card";

const currentMonth = new Date().toISOString().slice(0, 7);

export function AgentTrackingPage() {
  const [month, setMonth] = useState(currentMonth);

  const { data, isLoading } = useQuery({
    queryKey: ["agent-sessions-admin", month],
    queryFn: () => getAdminAgentSessions(month).then((r) => r.data),
  });

  const sessions: AgentSession[] = data?.sessions ?? [];
  const deadLoopCount = sessions.filter((s) => s.is_dead_loop).length;
  const totalLoops = sessions.reduce((sum, s) => sum + s.loop_count, 0);
  const totalToolCalls = sessions.reduce((sum, s) => sum + s.tool_call_count, 0);

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">AI Agent Tracking</h1>
        <input
          type="month"
          value={month}
          onChange={(e) => setMonth(e.target.value)}
          className="h-10 rounded-md border border-zinc-700 bg-zinc-800 px-3 text-sm text-zinc-100"
        />
      </div>

      <div className="grid grid-cols-3 gap-4">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm text-zinc-400 font-normal">Total Sessions</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-3xl font-bold">{sessions.length}</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm text-zinc-400 font-normal">Total Loops</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-3xl font-bold">{totalLoops.toLocaleString()}</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm text-zinc-400 font-normal">Dead-Loop Sessions</CardTitle>
          </CardHeader>
          <CardContent>
            <p className={`text-3xl font-bold ${deadLoopCount > 0 ? "text-red-400" : ""}`}>
              {deadLoopCount}
            </p>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Agent Sessions — {month}</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {isLoading ? (
            <p className="text-zinc-500 text-sm p-4">Loading...</p>
          ) : sessions.length === 0 ? (
            <p className="text-zinc-500 text-sm p-4">No agent sessions found for {month}.</p>
          ) : (
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-zinc-800 text-zinc-400 text-left">
                  <th className="px-4 py-3 font-medium">User</th>
                  <th className="px-4 py-3 font-medium">Conversation ID</th>
                  <th className="px-4 py-3 font-medium text-right">Loops</th>
                  <th className="px-4 py-3 font-medium text-right">Tool Calls</th>
                  <th className="px-4 py-3 font-medium">Dead Loop</th>
                  <th className="px-4 py-3 font-medium">Last Seen</th>
                </tr>
              </thead>
              <tbody>
                {sessions.map((s) => (
                  <tr
                    key={s.id}
                    className={`border-b border-zinc-800/50 hover:bg-zinc-800/30 transition-colors ${
                      s.is_dead_loop ? "bg-red-950/20" : ""
                    }`}
                  >
                    <td className="px-4 py-3 font-medium">{s.user_name}</td>
                    <td className="px-4 py-3 font-mono text-xs text-zinc-400 truncate max-w-[180px]">
                      {s.conversation_id}
                    </td>
                    <td className="px-4 py-3 text-right">{s.loop_count}</td>
                    <td className="px-4 py-3 text-right">{s.tool_call_count}</td>
                    <td className="px-4 py-3">
                      {s.is_dead_loop ? (
                        <span className="inline-flex items-center rounded-full bg-red-900/50 px-2 py-0.5 text-xs font-medium text-red-300">
                          Dead Loop
                        </span>
                      ) : (
                        <span className="inline-flex items-center rounded-full bg-zinc-800 px-2 py-0.5 text-xs font-medium text-zinc-400">
                          OK
                        </span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-zinc-400">
                      {new Date(s.last_seen_at).toLocaleString()}
                    </td>
                  </tr>
                ))}
              </tbody>
              <tfoot>
                <tr className="border-t border-zinc-700 bg-zinc-800/30">
                  <td className="px-4 py-3 font-medium text-zinc-300" colSpan={2}>
                    Total
                  </td>
                  <td className="px-4 py-3 text-right font-medium">{totalLoops.toLocaleString()}</td>
                  <td className="px-4 py-3 text-right font-medium">{totalToolCalls.toLocaleString()}</td>
                  <td className="px-4 py-3 text-red-400 font-medium">{deadLoopCount} dead</td>
                  <td />
                </tr>
              </tfoot>
            </table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
