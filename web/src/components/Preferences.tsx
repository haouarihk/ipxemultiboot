import { Monitor, Moon, Sun } from "lucide-react";
import type { Theme } from "../lib/api";
import { languages, useI18n } from "../lib/i18n";
import { cn, Field, Segmented } from "./ui";

export function LanguagePicker({ value, onChange }: { value: string; onChange: (code: string) => void }) {
  return (
    <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-5">
      {languages.map((l) => (
        <button
          key={l.code}
          type="button"
          onClick={() => onChange(l.code)}
          aria-pressed={value === l.code}
          className={cn(
            "rounded-xl border px-3 py-2.5 text-left text-sm font-medium transition",
            value === l.code
              ? "border-accent bg-accent-soft text-fg ring-4 ring-ring"
              : "border-border bg-surface hover:bg-surface-2",
          )}
        >
          {l.label}
        </button>
      ))}
    </div>
  );
}

export function ThemePicker({ value, onChange }: { value: Theme; onChange: (t: Theme) => void }) {
  const { t } = useI18n();
  return (
    <Segmented
      value={value}
      onChange={onChange}
      options={[
        { value: "system", label: t("settings.theme.system"), icon: <Monitor className="size-4" /> },
        { value: "light", label: t("settings.theme.light"), icon: <Sun className="size-4" /> },
        { value: "dark", label: t("settings.theme.dark"), icon: <Moon className="size-4" /> },
      ]}
    />
  );
}

export function PreferenceFields({
  language,
  theme,
  onLanguage,
  onTheme,
}: {
  language: string;
  theme: Theme;
  onLanguage: (c: string) => void;
  onTheme: (t: Theme) => void;
}) {
  const { t } = useI18n();
  return (
    <div className="space-y-6">
      <Field label={t("settings.language")}>
        <LanguagePicker value={language} onChange={onLanguage} />
      </Field>
      <Field label={t("settings.theme")}>
        <ThemePicker value={theme} onChange={onTheme} />
      </Field>
    </div>
  );
}
