import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import en, { type MessageKey, type Messages } from "../locales/en";

export const languages: { code: string; label: string }[] = [
  { code: "en", label: "English" },
  { code: "de", label: "Deutsch" },
  { code: "es", label: "Español" },
  { code: "fr", label: "Français" },
  { code: "zh", label: "中文" },
];

const loaders: Record<string, () => Promise<{ default: Messages }>> = {
  de: () => import("../locales/de"),
  es: () => import("../locales/es"),
  fr: () => import("../locales/fr"),
  zh: () => import("../locales/zh"),
};

const STORAGE_KEY = "ipxemultiboot.language";

type Vars = Record<string, string | number>;

interface I18n {
  language: string;
  setLanguage: (code: string) => void;
  t: (key: MessageKey, vars?: Vars) => string;
}

const Ctx = createContext<I18n | null>(null);

function initialLanguage(): string {
  const stored = localStorage.getItem(STORAGE_KEY);
  if (stored && languages.some((l) => l.code === stored)) return stored;
  const nav = navigator.language.slice(0, 2);
  return languages.some((l) => l.code === nav) ? nav : "en";
}

export function I18nProvider({ children }: { children: ReactNode }) {
  const [language, setLang] = useState(initialLanguage);
  const [messages, setMessages] = useState<Messages>(en);

  useEffect(() => {
    let cancelled = false;
    document.documentElement.lang = language;
    if (language === "en" || !loaders[language]) {
      setMessages(en);
      return;
    }
    loaders[language]().then((m) => !cancelled && setMessages(m.default));
    return () => {
      cancelled = true;
    };
  }, [language]);

  const setLanguage = useCallback((code: string) => {
    if (!languages.some((l) => l.code === code)) return;
    localStorage.setItem(STORAGE_KEY, code);
    setLang(code);
  }, []);

  const t = useCallback(
    (key: MessageKey, vars?: Vars) => {
      let s: string = messages[key] ?? en[key] ?? key;
      if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, String(v));
      return s;
    },
    [messages],
  );

  const value = useMemo(() => ({ language, setLanguage, t }), [language, setLanguage, t]);
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useI18n(): I18n {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("useI18n outside I18nProvider");
  return ctx;
}
