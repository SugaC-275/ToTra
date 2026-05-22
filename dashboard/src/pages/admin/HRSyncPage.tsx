import { useState, useRef } from "react";
import { syncHRCSV } from "../../api/client";
import type { SyncResult } from "../../api/client";

export default function HRSyncPage() {
  const fileRef = useRef<HTMLInputElement>(null);
  const [loading, setLoading] = useState(false);
  const [result, setResult] = useState<SyncResult | null>(null);
  const [error, setError] = useState("");

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const file = fileRef.current?.files?.[0];
    if (!file) return;
    setLoading(true);
    setError("");
    setResult(null);
    try {
      const res = await syncHRCSV(file);
      setResult(res);
    } catch (err: unknown) {
      const apiErr = err as { response?: { data?: { error?: string } } } | null;
      setError(apiErr?.response?.data?.error ?? "Sync failed");
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="p-6 space-y-6">
      <h1 className="text-2xl font-bold">HR Sync</h1>
      <p className="text-gray-500 text-sm">
        Upload a CSV file to sync org structure and personnel. Required columns: email, name, role (admin/employee), department.
      </p>

      <div className="bg-white rounded-lg border p-4 space-y-4">
        <h2 className="font-semibold text-lg">Upload CSV</h2>
        <form onSubmit={handleSubmit} className="space-y-3">
          <input
            type="file"
            accept=".csv"
            ref={fileRef}
            className="block border rounded px-3 py-2 w-full"
            required
          />
          {error && <p className="text-red-500 text-sm">{error}</p>}
          <button
            type="submit"
            disabled={loading}
            className="bg-blue-600 text-white px-4 py-2 rounded hover:bg-blue-700 disabled:opacity-50"
          >
            {loading ? "Syncing..." : "Sync"}
          </button>
        </form>
      </div>

      {result && (
        <div className="bg-white rounded-lg border p-4">
          <h2 className="font-semibold text-lg mb-3">Sync Result</h2>
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-4">
            {[
              { label: "Created", value: result.created, color: "text-green-700" },
              { label: "Updated", value: result.updated, color: "text-blue-700" },
              { label: "Skipped", value: result.skipped, color: "text-yellow-700" },
              { label: "Errors", value: result.errors, color: "text-red-700" },
            ].map(({ label, value, color }) => (
              <div key={label} className="text-center">
                <p className={`text-2xl font-bold ${color}`}>{value}</p>
                <p className="text-gray-500 text-sm">{label}</p>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
