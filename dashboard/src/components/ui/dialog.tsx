import { useEffect } from "react";
import type { HTMLAttributes, ReactNode } from "react";
import { cn } from "../../lib/utils";

interface DialogProps {
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  children: ReactNode;
}

export function Dialog({ open, onOpenChange, children }: DialogProps) {
  useEffect(() => {
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && open) onOpenChange?.(false);
    };
    document.addEventListener("keydown", handleKey);
    return () => document.removeEventListener("keydown", handleKey);
  }, [open, onOpenChange]);

  if (!open) return null;
  return (
    <div className="fixed inset-0 z-50">
      <div
        className="fixed inset-0 bg-black/70"
        onClick={() => onOpenChange?.(false)}
      />
      {children}
    </div>
  );
}

export function DialogTrigger({ children }: { children: ReactNode; asChild?: boolean }) {
  return <>{children}</>;
}

export function DialogContent({ className, children, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn(
        "fixed left-1/2 top-1/2 z-50 w-full max-w-md -translate-x-1/2 -translate-y-1/2 rounded-lg border border-zinc-700 bg-zinc-900 p-6 shadow-xl",
        className
      )}
      {...props}
    >
      {children}
    </div>
  );
}

export function DialogHeader({ children }: { children: ReactNode }) {
  return <div className="mb-4">{children}</div>;
}

export function DialogTitle({ className, ...props }: HTMLAttributes<HTMLHeadingElement>) {
  return (
    <h2
      className={cn("text-lg font-semibold text-zinc-100", className)}
      {...props}
    />
  );
}
