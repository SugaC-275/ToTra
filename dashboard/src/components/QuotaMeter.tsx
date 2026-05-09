import { Progress } from "./ui/progress";

interface QuotaMeterProps {
  used: number;
  limit: number;
  label?: string;
}

export function QuotaMeter({ used, limit, label = "SCU" }: QuotaMeterProps) {
  const pct = limit > 0 ? Math.min((used / limit) * 100, 100) : 0;

  return (
    <div className="space-y-2">
      <div className="flex justify-between text-sm">
        <span className="text-zinc-400">{label} Used</span>
        <span className="font-medium text-zinc-100">
          {used.toLocaleString()} / {limit.toLocaleString()}
        </span>
      </div>
      <Progress value={pct} />
      <p className="text-xs text-zinc-500 text-right">
        {(100 - pct).toFixed(1)}% remaining
      </p>
    </div>
  );
}
