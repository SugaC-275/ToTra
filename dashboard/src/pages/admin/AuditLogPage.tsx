import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { getAuditLog, verifyAuditChain, type VerifyResult } from "../../api/client";

export default function AuditLogPage() {
  const [limit, setLimit] = useState(50);
  const [verifyResult, setVerifyResult] = useState<VerifyResult | null>(null);
  const [verifying, setVerifying] = useState(false);
  const queryClient = useQueryClient();

  const { data: entries = [], isLoading } = useQuery({
    queryKey: ["audit-log", limit],
    queryFn: () => getAuditLog(limit),
  });

  const handleVerify = async () => {
    setVerifying(true);
    setVerifyResult(null);
    try {
      const result = await verifyAuditChain();
      setVerifyResult(result);
      queryClient.invalidateQueries({ queryKey: ["audit-log"] });
    } finally {
      setVerifying(false);
    }
  };

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Blockchain Audit Log</h1>
        <button
          onClick={handleVerify}
          disabled={verifying}
          className="px-4 py-2 bg-blue-600 text-white rounded-lg text-sm font-medium hover:bg-blue-700 disabled:opacity-50"
        >
          {verifying ? "Verifying…" : "Verify Chain"}
        </button>
      </div>

      {verifyResult && (
        <div
          className={`rounded-lg px-4 py-3 text-sm font-medium ${
            verifyResult.valid
              ? "bg-green-50 border border-green-300 text-green-800"
              : "bg-red-50 border border-red-300 text-red-800"
          }`}
        >
          {verifyResult.valid
            ? "Chain integrity verified — all hashes match."
            : `Chain tampered! First invalid entry id: ${verifyResult.first_bad_id}`}
        </div>
      )}

      <div className="flex items-center gap-2 text-sm">
        <span className="text-zinc-400">Show last</span>
        {[25, 50, 100, 200].map((n) => (
          <button
            key={n}
            onClick={() => setLimit(n)}
            className={`px-2 py-1 rounded text-sm ${
              limit === n
                ? "bg-blue-600 text-white"
                : "bg-zinc-800 text-zinc-300 hover:bg-zinc-700"
            }`}
          >
            {n}
          </button>
        ))}
        <span className="text-zinc-400">entries</span>
      </div>

      <div className="rounded-lg border border-zinc-800 overflow-x-auto">
        {isLoading ? (
          <p className="p-4 text-zinc-500 text-sm">Loading…</p>
        ) : entries.length === 0 ? (
          <p className="p-4 text-zinc-500 text-sm">No audit entries yet.</p>
        ) : (
          <table className="w-full text-xs">
            <thead>
              <tr className="text-left text-zinc-400 border-b border-zinc-800 bg-zinc-900">
                <th className="px-3 py-2">ID</th>
                <th className="px-3 py-2">Type</th>
                <th className="px-3 py-2">Record ID</th>
                <th className="px-3 py-2">Record Hash</th>
                <th className="px-3 py-2">Chain Hash</th>
                <th className="px-3 py-2">Created At</th>
              </tr>
            </thead>
            <tbody>
              {entries.map((e) => (
                <tr key={e.id} className="border-b border-zinc-800/50 hover:bg-zinc-800/30">
                  <td className="px-3 py-2 font-mono text-zinc-500">{e.id}</td>
                  <td className="px-3 py-2">
                    <span className="bg-blue-900/50 text-blue-300 rounded px-1.5 py-0.5 font-medium">
                      {e.record_type}
                    </span>
                  </td>
                  <td className="px-3 py-2 font-mono text-zinc-400 truncate max-w-[120px]">
                    {e.record_id}
                  </td>
                  <td className="px-3 py-2 font-mono text-zinc-500 truncate max-w-[120px]" title={e.record_hash}>
                    {e.record_hash.slice(0, 12)}…
                  </td>
                  <td className="px-3 py-2 font-mono text-zinc-500 truncate max-w-[120px]" title={e.chain_hash}>
                    {e.chain_hash.slice(0, 12)}…
                  </td>
                  <td className="px-3 py-2 text-zinc-400 whitespace-nowrap">
                    {new Date(e.created_at).toLocaleString()}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
