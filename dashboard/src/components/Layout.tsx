import { Link, useLocation, Outlet } from "react-router-dom";
import { cn } from "../lib/utils";

const navItems = [
  { label: "Dashboard", href: "/admin/dashboard" },
  { label: "Employees", href: "/admin/users" },
  { label: "Models", href: "/admin/models" },
  { label: "Quota Requests", href: "/admin/quota" },
  { label: "KPI", href: "/admin/kpi" },
  { label: "Integrations", href: "/admin/integrations" },
  { label: "My Usage", href: "/me" },
];

export function Layout() {
  const location = useLocation();
  return (
    <div className="flex min-h-screen bg-zinc-950 text-zinc-100">
      <aside className="w-56 border-r border-zinc-800 flex flex-col py-6 px-4 gap-1 shrink-0">
        <div className="mb-6 px-2">
          <span className="font-bold text-lg text-indigo-400">ToTra</span>
        </div>
        {navItems.map((item) => (
          <Link
            key={item.href}
            to={item.href}
            className={cn(
              "px-3 py-2 rounded-md text-sm font-medium transition-colors",
              location.pathname === item.href
                ? "bg-indigo-600 text-white"
                : "text-zinc-400 hover:text-zinc-100 hover:bg-zinc-800"
            )}
          >
            {item.label}
          </Link>
        ))}
      </aside>
      <main className="flex-1 p-8 overflow-auto">
        <Outlet />
      </main>
    </div>
  );
}
