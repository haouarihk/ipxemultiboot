import { BookOpen, Disc3, LayoutDashboard, LogOut, Menu, MonitorSmartphone, Settings, X } from "lucide-react";
import { useEffect, useState } from "react";
import { NavLink, Outlet, useLocation } from "react-router";
import { useI18n } from "../lib/i18n";
import { useLogout, useMe } from "../lib/session";
import { Logo } from "./Logo";
import { ThemeToggle } from "./ThemeToggle";
import { cn } from "./ui";
import { UploadProvider } from "./Uploads";

export function Layout() {
  const { t } = useI18n();
  const { data: me } = useMe();
  const logout = useLogout();
  const [open, setOpen] = useState(false);
  const location = useLocation();
  useEffect(() => setOpen(false), [location.pathname]);

  const nav = [
    { to: "/", label: t("nav.dashboard"), icon: LayoutDashboard, end: true },
    { to: "/isos", label: t("nav.isos"), icon: Disc3 },
    { to: "/clients", label: t("nav.clients"), icon: MonitorSmartphone },
    { to: "/settings", label: t("nav.settings"), icon: Settings },
  ];

  const sidebar = (
    <div className="flex h-full flex-col">
      <div className="flex h-16 items-center px-5">
        <Logo />
      </div>
      <nav className="flex-1 space-y-1 px-3 py-2">
        {nav.map(({ to, label, icon: Icon, end }) => (
          <NavLink
            key={to}
            to={to}
            end={end}
            className={({ isActive }) =>
              cn(
                "flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition",
                isActive ? "bg-accent-soft text-accent" : "text-muted hover:bg-surface-2 hover:text-fg",
              )
            }
          >
            <Icon className="size-4" />
            {label}
          </NavLink>
        ))}
      </nav>
      <div className="space-y-1 border-t border-border p-3">
        <a
          href="https://github.com/haouarihk/ipxemultiboot#readme"
          target="_blank"
          rel="noreferrer"
          className="flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium text-muted transition hover:bg-surface-2 hover:text-fg"
        >
          <BookOpen className="size-4" />
          {t("nav.docs")}
        </a>
        <div className="flex items-center gap-2 rounded-lg px-3 py-2">
          <div className="flex size-7 items-center justify-center rounded-full bg-accent-soft text-xs font-semibold text-accent uppercase">
            {me?.username.slice(0, 1)}
          </div>
          <span className="min-w-0 flex-1 truncate text-sm font-medium">{me?.username}</span>
          <ThemeToggle />
          <button
            onClick={logout}
            title={t("nav.logout")}
            aria-label={t("nav.logout")}
            className="rounded-md p-1.5 text-muted transition hover:bg-surface-2 hover:text-fg"
          >
            <LogOut className="size-4" />
          </button>
        </div>
      </div>
    </div>
  );

  return (
    <UploadProvider>
      <div className="flex h-full">
        <aside className="hidden w-64 shrink-0 border-r border-border bg-surface lg:block">{sidebar}</aside>

        {open && (
          <div className="fixed inset-0 z-50 lg:hidden">
            <div className="absolute inset-0 bg-black/50 backdrop-blur-sm" onClick={() => setOpen(false)} />
            <aside className="animate-fade-in absolute inset-y-0 left-0 w-72 border-r border-border bg-surface shadow-2xl">
              <button
                onClick={() => setOpen(false)}
                className="absolute top-4 right-3 rounded-md p-1.5 text-muted hover:bg-surface-2"
                aria-label="Close menu"
              >
                <X className="size-5" />
              </button>
              {sidebar}
            </aside>
          </div>
        )}

        <div className="flex min-w-0 flex-1 flex-col">
          <header className="flex h-14 items-center gap-3 border-b border-border bg-surface px-4 lg:hidden">
            <button onClick={() => setOpen(true)} className="rounded-md p-1.5 text-muted hover:bg-surface-2" aria-label="Open menu">
              <Menu className="size-5" />
            </button>
            <Logo />
          </header>
          <main className="flex-1 overflow-y-auto">
            <div className="mx-auto max-w-6xl px-4 py-8 sm:px-6 lg:px-8">
              <Outlet />
            </div>
          </main>
        </div>
      </div>
    </UploadProvider>
  );
}
