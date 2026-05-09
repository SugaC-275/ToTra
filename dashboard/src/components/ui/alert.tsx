import type { HTMLAttributes } from "react";
import { cn } from "../../lib/utils";

interface AlertProps extends HTMLAttributes<HTMLDivElement> {
  variant?: "default" | "destructive";
}

export function Alert({ className, variant = "default", ...props }: AlertProps) {
  return (
    <div
      className={cn(
        "relative w-full rounded-lg border p-4 text-sm",
        variant === "destructive"
          ? "border-red-800 bg-red-950 text-red-300"
          : "border-zinc-700 bg-zinc-800 text-zinc-200",
        className
      )}
      {...props}
    />
  );
}

export function AlertDescription({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("text-sm", className)} {...props} />;
}
