import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Monitor, Moon, Sun } from "lucide-react";
import { api, type Theme } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { useTheme } from "../lib/theme";

const order: Theme[] = ["system", "light", "dark"];

/** Cycles system → light → dark and persists the choice on the server. */
export function ThemeToggle({ persist = true }: { persist?: boolean }) {
  const { t } = useI18n();
  const { theme, setTheme } = useTheme();
  const qc = useQueryClient();
  const save = useMutation({
    mutationFn: (next: Theme) => api.saveSettings({ theme: next }),
    onSuccess: (data) => qc.setQueryData(["settings"], data),
  });
  const Icon = theme === "dark" ? Moon : theme === "light" ? Sun : Monitor;
  const next = order[(order.indexOf(theme) + 1) % order.length];
  return (
    <button
      onClick={() => {
        setTheme(next);
        if (persist) save.mutate(next);
      }}
      title={`${t("settings.theme")}: ${t(`settings.theme.${theme}`)}`}
      aria-label={t("settings.theme")}
      className="rounded-md p-1.5 text-muted transition hover:bg-surface-2 hover:text-fg"
    >
      <Icon className="size-4" />
    </button>
  );
}
