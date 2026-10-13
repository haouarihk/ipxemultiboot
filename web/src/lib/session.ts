import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { api, ApiError, type Settings, type User } from "./api";
import { useI18n } from "./i18n";
import { useTheme } from "./theme";

export function useSetupStatus() {
  return useQuery({ queryKey: ["setup"], queryFn: api.setupStatus, staleTime: 30_000 });
}

export function useMe() {
  return useQuery<User | null>({
    queryKey: ["me"],
    queryFn: async () => {
      try {
        return await api.me();
      } catch (e) {
        if (e instanceof ApiError && e.status === 401) return null;
        throw e;
      }
    },
    retry: false,
    staleTime: 60_000,
  });
}

export function useSettings(enabled = true) {
  return useQuery({ queryKey: ["settings"], queryFn: api.settings, enabled });
}

/** Applies the server-side language/theme once settings are loaded. */
export function useSyncPreferences(settings?: Pick<Settings, "language" | "theme">) {
  const { language, setLanguage } = useI18n();
  const { theme, setTheme } = useTheme();
  useEffect(() => {
    if (!settings) return;
    if (settings.language && settings.language !== language) setLanguage(settings.language);
    if (settings.theme && settings.theme !== theme) setTheme(settings.theme);
    // Only react to server changes, not local ones.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [settings?.language, settings?.theme]);
}

export function useLogout() {
  const qc = useQueryClient();
  return async () => {
    await api.logout().catch(() => {});
    qc.setQueryData(["me"], null);
    qc.removeQueries({ predicate: (q) => !["me", "setup"].includes(q.queryKey[0] as string) });
  };
}
