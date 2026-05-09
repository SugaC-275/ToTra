import type { HTMLAttributes } from "react";
import { cn } from "../../lib/utils";

interface BadgeProps extends HTMLAttributes<HTMLSpanElement> {
  variant?: "default" | "secondary" | "destructive" | "outline";
}

export function Badge({ className, variant = "default", ...props }: BadgeProps) {
  const variants: Record<string, string> = {
    default: "bg-indigo-600 text-white",
    secondary: "bg-zinc-700 text-zinc-300",
    destructive: "bg-red-900 text-red-300",
    outline: "border border-zinc-600 text-zinc-300",
  };
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium",
        variants[variant],
        className
      )}
      {...props}
    />
  );
}
