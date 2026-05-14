import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "../../api/client";

interface ChecklistEntry {
  item_key: string;
  label: string;
  description: string;
  status: "not_assessed" | "compliant" | "partial" | "non_compliant";
  notes: string;
  assessed_at: string | null;
}

const STATUS_COLORS: Record<string, string> = {
  compliant: "text-green-700 bg-green-50 border-green-200",
  partial: "text-yellow-700 bg-yellow-50 border-yellow-200",
  non_compliant: "text-red-700 bg-red-50 border-red-200",
  not_assessed: "text-gray-500 bg-gray-50 border-gray-200",
};

const STATUS_LABELS: Record<string, string> = {
  compliant: "Compliant",
  partial: "Partial",
  non_compliant: "Non-Compliant",
  not_assessed: "Not Assessed",
};

export default function ComplianceChecklistPage() {
  const qc = useQueryClient();
  const { data, isLoading } = useQuery({
    queryKey: ["compliance-checklist"],
    queryFn: () =>
      apiClient
        .get<{ items: ChecklistEntry[] }>("/api/admin/compliance/checklist")
        .then((r) => r.data.items),
  });

  const updateMutation = useMutation({
    mutationFn: ({ key, status, notes }: { key: string; status: string; notes: string }) =>
      apiClient.put(`/api/admin/compliance/checklist/${key}`, { status, notes }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["compliance-checklist"] }),
  });

  if (isLoading) return <div className="p-8 text-gray-400">Loading checklist…</div>;

  const items = data ?? [];
  const compliantCount = items.filter((i) => i.status === "compliant").length;

  return (
    <div className="p-8 max-w-4xl mx-auto">
      <h1 className="text-2xl font-bold mb-1">EU AI Act Compliance Checklist</h1>
      <p className="text-gray-500 text-sm mb-6">
        Self-assessment for high-risk AI system requirements.
      </p>
      <div className="mb-6 px-4 py-3 bg-blue-50 border border-blue-100 rounded-lg text-sm text-blue-700">
        <span className="font-semibold">{compliantCount}</span> / {items.length} items marked compliant
      </div>
      <div className="space-y-3">
        {items.map((item) => (
          <div key={item.item_key} className={`border rounded-lg p-4 ${STATUS_COLORS[item.status]}`}>
            <div className="flex items-start justify-between gap-4">
              <div className="flex-1">
                <div className="flex items-center gap-2 mb-1">
                  <h3 className="font-semibold text-gray-800">{item.label}</h3>
                  <span className="text-xs px-2 py-0.5 rounded-full border font-medium">
                    {STATUS_LABELS[item.status]}
                  </span>
                </div>
                <p className="text-sm text-gray-600">{item.description}</p>
                {item.assessed_at && (
                  <p className="text-xs text-gray-400 mt-1">
                    Last assessed: {new Date(item.assessed_at).toLocaleDateString()}
                  </p>
                )}
              </div>
              <select
                value={item.status}
                onChange={(e) =>
                  updateMutation.mutate({ key: item.item_key, status: e.target.value, notes: item.notes })
                }
                className="text-sm border border-gray-200 rounded px-2 py-1 bg-white text-gray-700 min-w-[140px]"
              >
                <option value="not_assessed">Not Assessed</option>
                <option value="compliant">Compliant</option>
                <option value="partial">Partial</option>
                <option value="non_compliant">Non-Compliant</option>
              </select>
            </div>
            {item.notes && (
              <p className="mt-2 text-sm text-gray-600 border-t border-gray-200 pt-2">{item.notes}</p>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
