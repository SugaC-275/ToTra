import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  listIPAllowlist,
  addIPAllowlistEntry,
  deleteIPAllowlistEntry,
} from "../../api/client";
import type { IPAllowlistEntry } from "../../api/client";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "../../components/ui/card";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";

export function IPAllowlistPage() {
  const queryClient = useQueryClient();
  const [cidr, setCidr] = useState("");
  const [label, setLabel] = useState("");
  const [addError, setAddError] = useState<string | null>(null);

  const { data, isLoading } = useQuery({
    queryKey: ["ip-allowlist"],
    queryFn: () => listIPAllowlist().then((r) => r.data),
  });

  const entries: IPAllowlistEntry[] = data?.entries ?? [];

  const addMutation = useMutation({
    mutationFn: () => addIPAllowlistEntry(cidr.trim(), label.trim()),
    onSuccess: () => {
      setCidr("");
      setLabel("");
      setAddError(null);
      queryClient.invalidateQueries({ queryKey: ["ip-allowlist"] });
    },
    onError: (err: unknown) => {
      const message =
        (err as { response?: { data?: { error?: string } } })?.response?.data
          ?.error ?? "Failed to add entry";
      setAddError(message);
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteIPAllowlistEntry(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["ip-allowlist"] });
    },
  });

  function handleAdd(e: React.FormEvent) {
    e.preventDefault();
    if (!cidr.trim()) return;
    setAddError(null);
    addMutation.mutate();
  }

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">IP Allowlist</h1>
      <p className="text-sm text-zinc-400">
        When at least one entry exists, only requests from matching IP ranges
        are permitted. If no entries exist, all IPs are allowed (opt-in).
      </p>

      <Card>
        <CardHeader>
          <CardTitle>Add CIDR Entry</CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleAdd} className="flex items-end gap-3 flex-wrap">
            <div className="flex flex-col gap-1">
              <label className="text-xs text-zinc-400 font-medium">
                CIDR <span className="text-zinc-500">(e.g. 10.0.0.0/8)</span>
              </label>
              <Input
                type="text"
                placeholder="203.0.113.0/24"
                value={cidr}
                onChange={(e) => setCidr(e.target.value)}
                className="w-52"
                required
              />
            </div>
            <div className="flex flex-col gap-1">
              <label className="text-xs text-zinc-400 font-medium">
                Label <span className="text-zinc-500">(optional)</span>
              </label>
              <Input
                type="text"
                placeholder="office-vpn"
                value={label}
                onChange={(e) => setLabel(e.target.value)}
                className="w-40"
              />
            </div>
            <Button
              type="submit"
              disabled={addMutation.isPending || !cidr.trim()}
            >
              {addMutation.isPending ? "Adding..." : "Add Entry"}
            </Button>
          </form>
          {addError && (
            <p className="mt-2 text-sm text-red-400">{addError}</p>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>
            Current Allowlist
            {entries.length > 0 && (
              <span className="ml-2 text-sm font-normal text-zinc-400">
                ({entries.length} {entries.length === 1 ? "entry" : "entries"})
              </span>
            )}
          </CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {isLoading ? (
            <p className="text-zinc-500 text-sm p-4">Loading...</p>
          ) : entries.length === 0 ? (
            <p className="text-zinc-500 text-sm p-4 text-center">
              No entries — all IPs are currently allowed.
            </p>
          ) : (
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-zinc-800 text-zinc-400">
                  <th className="text-left py-2 px-4 font-medium">CIDR</th>
                  <th className="text-left py-2 font-medium">Label</th>
                  <th className="text-left py-2 font-medium">Added</th>
                  <th className="py-2 px-4" />
                </tr>
              </thead>
              <tbody>
                {entries.map((entry) => (
                  <tr
                    key={entry.id}
                    className="border-b border-zinc-800/50 hover:bg-zinc-800/30"
                  >
                    <td className="py-2 px-4 font-mono text-indigo-300">
                      {entry.cidr}
                    </td>
                    <td className="py-2 text-zinc-400">
                      {entry.label || <span className="text-zinc-600">—</span>}
                    </td>
                    <td className="py-2 text-zinc-500">
                      {new Date(entry.created_at).toLocaleDateString()}
                    </td>
                    <td className="py-2 px-4 text-right">
                      <Button
                        variant="destructive"
                        size="sm"
                        onClick={() => deleteMutation.mutate(entry.id)}
                        disabled={deleteMutation.isPending}
                      >
                        Delete
                      </Button>
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
