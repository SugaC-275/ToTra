import type { HTMLAttributes } from "react";
import { cn } from "../../lib/utils";

interface ProgressProps extends HTMLAttributes<HTMLDivElement> {
  value?: number;
}

export function Progress({ className, value = 0, ...props }: ProgressProps) {
  const color =
    value > 90 ? "bg-red-500" : value > 70 ? "bg-yellow-500" : "bg-green-500";
  return (
    <div
      className={cn("relative h-3 w-full overflow-hidden rounded-full bg-zinc-800", className)}
      {...props}
    >
      <div
        className={cn("h-full transition-all", color)}
        style={{ width: `${Math.min(value, 100)}%` }}
      />
    </div>
  );
}
