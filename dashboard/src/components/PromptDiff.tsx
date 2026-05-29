import { useState, useEffect } from "react";
import { diffChars } from "diff";
import { apiClient } from "../api/client";

interface PromptVersion {
  version: number;
  content: string;
  created_at: string;
}

interface PiiCheckResult {
  flagged: boolean;
  hits: { entity_type: string; text: string }[];
}

interface PromptDiffProps {
  promptName: string;
  versionA: PromptVersion;
  versionB: PromptVersion;
}

// Count PII pattern matches by calling the check endpoint (fallback to client-side regex)
async function checkPii(text: string): Promise<PiiCheckResult> {
  try {
    const { data } = await apiClient.post<PiiCheckResult>("/gateway/v1/pii/check", { text });
    return data;
  } catch {
    const emailRe = /[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}/g;
    const phoneRe = /\b\d{3}[-.\s]?\d{3}[-.\s]?\d{4}\b/g;
    const ssnRe = /\b\d{3}-\d{2}-\d{4}\b/g;
    const hits: { entity_type: string; text: string }[] = [];
    (text.match(emailRe) ?? []).forEach((t) => hits.push({ entity_type: "EMAIL", text: t }));
    (text.match(phoneRe) ?? []).forEach((t) => hits.push({ entity_type: "PHONE", text: t }));
    (text.match(ssnRe) ?? []).forEach((t) => hits.push({ entity_type: "SSN", text: t }));
    return { flagged: hits.length > 0, hits };
  }
}

function estimateTokens(text: string): number {
  return Math.ceil(text.length / 4);
}

export function PromptDiff({ promptName, versionA, versionB }: PromptDiffProps) {
  const [piiA, setPiiA] = useState<PiiCheckResult | null>(null);
  const [piiB, setPiiB] = useState<PiiCheckResult | null>(null);
  const [piiLoading, setPiiLoading] = useState(false);

  const tokensA = estimateTokens(versionA.content);
  const tokensB = estimateTokens(versionB.content);
  // Rough cost: $0.50 / 1M input tokens (conservative proxy)
  const costPerToken = 0.50 / 1_000_000;
  const costA = tokensA * costPerToken;
  const costB = tokensB * costPerToken;

  useEffect(() => {
    let cancelled = false;
    setPiiLoading(true);
    setPiiA(null);
    setPiiB(null);
    Promise.all([checkPii(versionA.content), checkPii(versionB.content)])
      .then(([a, b]) => {
        if (!cancelled) {
          setPiiA(a);
          setPiiB(b);
        }
      })
      .finally(() => {
        if (!cancelled) setPiiLoading(false);
      });
    return () => { cancelled = true; };
  }, [versionA.content, versionB.content]);

  const tokenDelta = tokensB - tokensA;
  const costDelta = costB - costA;
  const piiDelta = piiA && piiB ? piiB.hits.length - piiA.hits.length : null;

  const changes = diffChars(versionA.content, versionB.content);

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3">
        <span className="text-sm font-semibold text-zinc-200">
          Diff: {promptName}
        </span>
        <span className="text-xs text-zinc-500">
          v{versionA.version} vs v{versionB.version}
        </span>
      </div>

      {/* Side-by-side diff */}
      <div className="grid grid-cols-2 gap-3">
        {/* Old version */}
        <div>
          <p className="text-xs text-zinc-500 mb-1 font-medium">
            v{versionA.version} &mdash; {new Date(versionA.created_at).toLocaleDateString()}
          </p>
          <div className="rounded bg-zinc-950 border border-zinc-800 p-3 text-xs font-mono text-zinc-300 whitespace-pre-wrap break-words leading-relaxed min-h-[80px]">
            {changes.map((part, i) => {
              if (part.added) return null;
              if (part.removed)
                return (
                  <mark key={i} className="bg-red-900/60 text-red-300 rounded-sm">
                    {part.value}
                  </mark>
                );
              return <span key={i}>{part.value}</span>;
            })}
          </div>
        </div>
        {/* New version */}
        <div>
          <p className="text-xs text-zinc-500 mb-1 font-medium">
            v{versionB.version} &mdash; {new Date(versionB.created_at).toLocaleDateString()}
          </p>
          <div className="rounded bg-zinc-950 border border-zinc-800 p-3 text-xs font-mono text-zinc-300 whitespace-pre-wrap break-words leading-relaxed min-h-[80px]">
            {changes.map((part, i) => {
              if (part.removed) return null;
              if (part.added)
                return (
                  <mark key={i} className="bg-green-900/60 text-green-300 rounded-sm">
                    {part.value}
                  </mark>
                );
              return <span key={i}>{part.value}</span>;
            })}
          </div>
        </div>
      </div>

      {/* Comparison table */}
      <div className="rounded border border-zinc-800 overflow-hidden">
        <table className="w-full text-xs">
          <thead>
            <tr className="bg-zinc-900 border-b border-zinc-800 text-left text-zinc-400">
              <th className="px-3 py-2 font-medium">Metric</th>
              <th className="px-3 py-2 font-medium">v{versionA.version}</th>
              <th className="px-3 py-2 font-medium">v{versionB.version}</th>
              <th className="px-3 py-2 font-medium">Delta</th>
            </tr>
          </thead>
          <tbody>
            <tr className="border-b border-zinc-800/50">
              <td className="px-3 py-2 text-zinc-400">Tokens (est.)</td>
              <td className="px-3 py-2 text-zinc-300">{tokensA.toLocaleString()}</td>
              <td className="px-3 py-2 text-zinc-300">{tokensB.toLocaleString()}</td>
              <td className={`px-3 py-2 font-mono ${tokenDelta > 0 ? "text-yellow-400" : tokenDelta < 0 ? "text-green-400" : "text-zinc-500"}`}>
                {tokenDelta > 0 ? "+" : ""}{tokenDelta}
              </td>
            </tr>
            <tr className="border-b border-zinc-800/50">
              <td className="px-3 py-2 text-zinc-400">Est. cost / call</td>
              <td className="px-3 py-2 text-zinc-300">${costA.toFixed(6)}</td>
              <td className="px-3 py-2 text-zinc-300">${costB.toFixed(6)}</td>
              <td className={`px-3 py-2 font-mono ${costDelta > 0 ? "text-yellow-400" : costDelta < 0 ? "text-green-400" : "text-zinc-500"}`}>
                {costDelta > 0 ? "+" : ""}${costDelta.toFixed(6)}
              </td>
            </tr>
            <tr>
              <td className="px-3 py-2 text-zinc-400">PII risk score</td>
              <td className="px-3 py-2 text-zinc-300">
                {piiLoading ? "…" : piiA ? (
                  <span className={piiA.hits.length > 0 ? "text-red-400" : "text-green-400"}>
                    {piiA.hits.length} pattern{piiA.hits.length !== 1 ? "s" : ""}
                  </span>
                ) : "—"}
              </td>
              <td className="px-3 py-2 text-zinc-300">
                {piiLoading ? "…" : piiB ? (
                  <span className={piiB.hits.length > 0 ? "text-red-400" : "text-green-400"}>
                    {piiB.hits.length} pattern{piiB.hits.length !== 1 ? "s" : ""}
                  </span>
                ) : "—"}
              </td>
              <td className={`px-3 py-2 font-mono ${
                piiDelta === null ? "text-zinc-500" :
                piiDelta > 0 ? "text-red-400" :
                piiDelta < 0 ? "text-green-400" : "text-zinc-500"
              }`}>
                {piiDelta === null ? "—" : piiDelta > 0
                  ? `+${piiDelta} (PII risk increased)`
                  : piiDelta < 0
                  ? `${piiDelta} (PII risk decreased)`
                  : "No change"}
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      {/* PII entity detail if any hits in newer version */}
      {(piiB?.hits.length ?? 0) > 0 && (
        <div className="rounded border border-red-900/50 bg-red-950/20 p-3 space-y-1">
          <p className="text-xs font-medium text-red-400">PII patterns in v{versionB.version}:</p>
          <div className="flex flex-wrap gap-1">
            {piiB!.hits.map((h, i) => (
              <span key={i} className="px-1.5 py-0.5 rounded bg-red-900/40 text-red-300 text-xs font-mono">
                {h.entity_type}: &ldquo;{h.text.slice(0, 20)}{h.text.length > 20 ? "…" : ""}&rdquo;
              </span>
            ))}
          </div>
        </div>
      )}

      <p className="text-xs text-zinc-600">
        Legend:{" "}
        <mark className="bg-red-900/60 text-red-300 rounded-sm px-0.5">removed</mark>{" "}
        <mark className="bg-green-900/60 text-green-300 rounded-sm px-0.5">added</mark>
      </p>
    </div>
  );
}
