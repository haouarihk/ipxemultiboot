import { Languages } from "lucide-react";
import type { ReactNode } from "react";
import { languages, useI18n } from "../lib/i18n";
import { Logo } from "./Logo";
import { ThemeToggle } from "./ThemeToggle";

/** Centered layout for registration, login and onboarding. */
export function AuthShell({ children, wide }: { children: ReactNode; wide?: boolean }) {
  const { t, language, setLanguage } = useI18n();
  return (
    <div className="relative flex min-h-full flex-col overflow-hidden">
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 -z-10 bg-[radial-gradient(60rem_30rem_at_50%_-10%,var(--accent-soft),transparent)]"
      />
      <header className="flex items-center justify-between px-6 py-5">
        <Logo />
        <div className="flex items-center gap-1">
          <label className="flex items-center gap-1.5 rounded-md px-2 py-1 text-sm text-muted hover:bg-surface-2">
            <Languages className="size-4" />
            <select
              value={language}
              onChange={(e) => setLanguage(e.target.value)}
              className="cursor-pointer bg-transparent focus:outline-none"
              aria-label={t("settings.language")}
            >
              {languages.map((l) => (
                <option key={l.code} value={l.code}>
                  {l.label}
                </option>
              ))}
            </select>
          </label>
          <ThemeToggle persist={false} />
        </div>
      </header>
      <main className="flex flex-1 items-start justify-center px-4 pt-6 pb-16 sm:items-center sm:pt-0">
        <div className={wide ? "w-full max-w-2xl" : "w-full max-w-md"}>{children}</div>
      </main>
      <footer className="pb-6 text-center text-xs text-muted">{t("app.tagline")}</footer>
    </div>
  );
}
