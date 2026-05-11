import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  getQuarterlyROI,
  getROIBenchmark,
  getDeptChallenge,
} from "../../api/client";

const currentYear = new Date().getFullYear().toString();
const currentMonth = new Date().toISOString().slice(0, 7);

export default function ROIReportPage() {
  const [year, setYear] = useState(currentYear);
  const [month, setMonth] = useState(currentMonth);

  const { data: quarterly = [] } = useQuery({
    queryKey: ["roi-quarterly", year],
    queryFn: () => getQuarterlyROI(year),
  });

  const { data: benchmark } = useQuery({
    queryKey: ["roi-benchmark", month],
    queryFn: () => getROIBenchmark(month),
  });

  const { data: challenge = [] } = useQuery({
    queryKey: ["roi-challenge", month],
    queryFn: () => getDeptChallenge(month),
  });

  return (
    <div className="p-6 space-y-8">
      <h1 className="text-2xl font-bold">Executive ROI Reports</h1>

      {/* Quarterly ROI */}
      <section className="bg-white rounded-lg border p-4 space-y-4">
        <div className="flex items-center justify-between">
          <h2 className="font-semibold text-lg">Quarterly ROI</h2>
          <input
            type="number"
            value={year}
            min="2020"
            max="2030"
            onChange={(e) => setYear(e.target.value)}
            className="border rounded px-2 py-1 w-24 text-sm"
          />
        </div>
        {quarterly.length === 0 ? (
          <p className="text-gray-400 text-sm">No data for {year}.</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-gray-500 border-b">
                  <th className="pb-2 pr-4">Quarter</th>
                  <th className="pb-2 pr-4">AI Cost (USD)</th>
                  <th className="pb-2 pr-4">Output Weight</th>
                  <th className="pb-2 pr-4">ROI Score</th>
                  <th className="pb-2">Active Users</th>
                </tr>
              </thead>
              <tbody>
                {quarterly.map((q) => (
                  <tr key={q.quarter} className="border-b last:border-0">
                    <td className="py-2 pr-4 font-medium">{q.quarter}</td>
                    <td className="py-2 pr-4">${q.total_usd.toFixed(2)}</td>
                    <td className="py-2 pr-4">{q.total_output.toFixed(2)}</td>
                    <td className="py-2 pr-4 font-semibold text-blue-700">
                      {q.roi_score.toFixed(3)}
                    </td>
                    <td className="py-2">{q.active_users}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {/* Month selector shared by benchmark + challenge */}
      <div className="flex items-center gap-2">
        <span className="text-sm text-gray-500">Month:</span>
        <input
          type="month"
          value={month}
          onChange={(e) => setMonth(e.target.value)}
          className="border rounded px-2 py-1 text-sm"
        />
      </div>

      {/* Industry Benchmark */}
      <section className="bg-white rounded-lg border p-4 space-y-3">
        <h2 className="font-semibold text-lg">Industry Benchmark</h2>
        {benchmark ? (
          <div className="space-y-3">
            <div className="flex items-center gap-4">
              <div className="text-center">
                <p className="text-3xl font-bold text-blue-700">{benchmark.label}</p>
                <p className="text-gray-500 text-sm">Your standing</p>
              </div>
              <div className="text-center">
                <p className="text-3xl font-bold text-gray-800">
                  {benchmark.tenant_avg_efficiency.toFixed(3)}
                </p>
                <p className="text-gray-500 text-sm">Your avg efficiency</p>
              </div>
            </div>
            <div className="grid grid-cols-4 gap-2 text-center text-xs text-gray-500">
              {[
                { label: "P25", value: benchmark.industry_p25 },
                { label: "P50", value: benchmark.industry_p50 },
                { label: "P75", value: benchmark.industry_p75 },
                { label: "P90", value: benchmark.industry_p90 },
              ].map(({ label, value }) => (
                <div key={label} className="bg-gray-50 rounded p-2">
                  <p className="font-medium text-gray-700">{value.toFixed(3)}</p>
                  <p>{label}</p>
                </div>
              ))}
            </div>
          </div>
        ) : (
          <p className="text-gray-400 text-sm">Loading...</p>
        )}
      </section>

      {/* Department Efficiency Challenge */}
      <section className="bg-white rounded-lg border p-4 space-y-3">
        <h2 className="font-semibold text-lg">Department Efficiency Challenge</h2>
        {challenge.length === 0 ? (
          <p className="text-gray-400 text-sm">No department data for {month}.</p>
        ) : (
          <div className="space-y-2">
            {challenge.map((d) => (
              <div
                key={d.department}
                className={`flex items-center justify-between rounded-lg p-3 ${
                  d.is_winner ? "bg-yellow-50 border border-yellow-300" : "bg-gray-50"
                }`}
              >
                <div className="flex items-center gap-3">
                  <span className="text-lg font-bold text-gray-400">#{d.rank}</span>
                  <div>
                    <p className="font-medium">
                      {d.department}
                      {d.is_winner && (
                        <span className="ml-2 text-xs bg-yellow-200 text-yellow-800 rounded px-1 py-0.5">
                          Winner
                        </span>
                      )}
                    </p>
                    <p className="text-xs text-gray-500">{d.user_count} users</p>
                  </div>
                </div>
                <p className="text-xl font-bold text-blue-700">{d.avg_aiq_score.toFixed(1)}</p>
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}
