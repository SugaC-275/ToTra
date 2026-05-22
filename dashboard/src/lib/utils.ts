import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

/** Extract a human-readable message from an API/Axios error, falling back to `fallback`. */
export function apiErrorMessage(err: unknown, fallback: string): string {
  const apiErr = err as { response?: { data?: { error?: string } } } | null;
  return apiErr?.response?.data?.error ?? fallback;
}
