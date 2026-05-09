import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from "recharts";
import type { UserSummary } from "../api/client";

export function UsageChart({ data }: { data: UserSummary[] }) {
  const chartData = data.slice(0, 10).map((u) => ({
    name: u.user_name.split(" ")[0],
    scu: Math.round(u.total_scu),
    requests: u.request_count,
  }));

  return (
    <ResponsiveContainer width="100%" height={280}>
      <BarChart data={chartData}>
        <CartesianGrid strokeDasharray="3 3" stroke="#3f3f46" />
        <XAxis dataKey="name" tick={{ fontSize: 12, fill: "#71717a" }} />
        <YAxis tick={{ fontSize: 12, fill: "#71717a" }} />
        <Tooltip
          contentStyle={{ background: "#18181b", border: "1px solid #3f3f46", borderRadius: 6 }}
          labelStyle={{ color: "#e4e4e7" }}
        />
        <Bar dataKey="scu" fill="#6366f1" radius={[4, 4, 0, 0]} name="SCU Used" />
      </BarChart>
    </ResponsiveContainer>
  );
}
