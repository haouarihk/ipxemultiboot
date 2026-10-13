import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, HardDrive, Info, KeyRound, Network, Palette } from "lucide-react";
import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { BootstrapHints, DhcpModePicker, InterfaceSelect, IsoDirField, PoolFields } from "../components/NetworkFields";
import { PreferenceFields } from "../components/Preferences";
import { Alert, Button, Card, CardHeader, Field, Input, PageHeader, PageLoader } from "../components/ui";
import { api, type Settings, type Theme } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { useSettings } from "../lib/session";
import { useTheme } from "../lib/theme";

function Section({ icon, title, description, children }: { icon: ReactNode; title: string; description?: string; children: ReactNode }) {
  return (
    <Card>
      <CardHeader icon={icon} title={title} description={description} />
      <div className="p-5">{children}</div>
    </Card>
  );
}

function SaveBar({ dirty, pending, saved, onSave, error }: { dirty: boolean; pending: boolean; saved: boolean; onSave: () => void; error?: string }) {
  const { t } = useI18n();
  return (
    <div className="mt-6 flex flex-wrap items-center justify-end gap-3">
      {error && <p className="mr-auto text-sm text-danger">{error}</p>}
      {saved && !dirty && (
        <span className="flex items-center gap-1 text-sm text-success">
          <Check className="size-4" /> {t("common.saved")}
        </span>
      )}
      <Button onClick={onSave} disabled={!dirty} loading={pending}>
        {pending ? t("common.saving") : t("common.save")}
      </Button>
    </div>
  );
}

function useSection<K extends keyof Settings>(settings: Settings, keys: K[]) {
  const qc = useQueryClient();
  const pick = (s: Settings) => Object.fromEntries(keys.map((k) => [k, s[k]])) as Pick<Settings, K>;
  const [draft, setDraft] = useState(() => pick(settings));
  const [saved, setSaved] = useState(false);
  useEffect(() => {
    setDraft(pick(settings));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [settings]);
  const save = useMutation({
    mutationFn: () => api.saveSettings(draft),
    onSuccess: (data) => {
      qc.setQueryData(["settings"], data);
      qc.invalidateQueries({ queryKey: ["status"] });
      qc.invalidateQueries({ queryKey: ["netcheck"] });
      setSaved(true);
    },
  });
  const dirty = keys.some((k) => draft[k] !== settings[k]);
  return { draft, setDraft: (p: Partial<Pick<Settings, K>>) => (setSaved(false), setDraft({ ...draft, ...p })), save, dirty, saved };
}

function GeneralSection({ settings }: { settings: Settings }) {
  const { t, setLanguage } = useI18n();
  const { setTheme } = useTheme();
  const qc = useQueryClient();
  const save = useMutation({
    mutationFn: (p: Partial<Settings>) => api.saveSettings(p),
    onSuccess: (data) => qc.setQueryData(["settings"], data),
  });
  // Preferences apply instantly, no save button needed.
  return (
    <Section icon={<Palette className="size-4" />} title={t("settings.section.general")} description={t("settings.section.general.desc")}>
      <PreferenceFields
        language={settings.language}
        theme={settings.theme}
        onLanguage={(c) => {
          setLanguage(c);
          save.mutate({ language: c });
        }}
        onTheme={(th: Theme) => {
          setTheme(th);
          save.mutate({ theme: th });
        }}
      />
    </Section>
  );
}

function NetworkSection({ settings }: { settings: Settings }) {
  const { t } = useI18n();
  const { draft, setDraft, save, dirty, saved } = useSection(settings, ["interface", "dhcpMode", "poolStart", "poolEnd", "router", "dns"]);
  const status = useQuery({ queryKey: ["status"], queryFn: api.status });
  const dhcp = status.data?.server.services.dhcp;
  return (
    <Section icon={<Network className="size-4" />} title={t("settings.section.network")} description={t("settings.section.network.desc")}>
      <div className="space-y-6">
        <Field label={t("settings.interface")}>
          <InterfaceSelect value={draft.interface} onChange={(v) => setDraft({ interface: v })} />
        </Field>
        <Field label={t("settings.dhcpMode")}>
          <DhcpModePicker value={draft.dhcpMode} onChange={(m) => setDraft({ dhcpMode: m })} />
        </Field>
        {draft.dhcpMode === "full" && <PoolFields value={draft} onChange={(v) => setDraft(v)} />}
        {draft.dhcpMode === "bootstrap" && settings.dhcpMode === "bootstrap" && <BootstrapHints hints={status.data?.server.bootstrap} />}
        {dhcp?.state === "error" && !dirty && (
          <Alert tone="danger" title={t("service.dhcp")}>
            {dhcp.error}
          </Alert>
        )}
        <p className="flex items-center gap-2 text-xs text-muted">
          <Info className="size-3.5" /> {t("settings.networkRestart")}
        </p>
      </div>
      <SaveBar dirty={dirty} pending={save.isPending} saved={saved} onSave={() => save.mutate()} error={save.error?.message} />
    </Section>
  );
}

function StorageSection({ settings, defaults }: { settings: Settings; defaults: Settings }) {
  const { t } = useI18n();
  const { draft, setDraft, save, dirty, saved } = useSection(settings, ["isoDir"]);
  return (
    <Section icon={<HardDrive className="size-4" />} title={t("settings.section.storage")} description={t("settings.section.storage.desc")}>
      <IsoDirField value={draft.isoDir} defaultPath={defaults.isoDir} onChange={(v) => setDraft({ isoDir: v })} />
      <SaveBar dirty={dirty} pending={save.isPending} saved={saved} onSave={() => save.mutate()} error={save.error?.message} />
    </Section>
  );
}

function AccountSection() {
  const { t } = useI18n();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const change = useMutation({
    mutationFn: () => api.changePassword(current, next),
    onSuccess: () => {
      setCurrent("");
      setNext("");
      setConfirm("");
    },
  });
  const mismatch = confirm.length > 0 && next !== confirm;
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!mismatch) change.mutate();
  };
  return (
    <Section icon={<KeyRound className="size-4" />} title={t("settings.section.account")} description={t("settings.section.account.desc")}>
      <form onSubmit={submit} className="grid gap-4 sm:grid-cols-3">
        <Field label={t("settings.currentPassword")}>
          <Input type="password" autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} required />
        </Field>
        <Field label={t("settings.newPassword")} hint={t("auth.passwordHint")}>
          <Input type="password" autoComplete="new-password" value={next} onChange={(e) => setNext(e.target.value)} minLength={8} required />
        </Field>
        <Field label={t("auth.confirmPassword")} error={mismatch ? t("auth.passwordMismatch") : undefined}>
          <Input type="password" autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} invalid={mismatch} required />
        </Field>
        <div className="flex flex-wrap items-center justify-end gap-3 sm:col-span-3">
          {change.error && <p className="mr-auto text-sm text-danger">{change.error.message}</p>}
          {change.isSuccess && (
            <span className="flex items-center gap-1 text-sm text-success">
              <Check className="size-4" /> {t("settings.passwordChanged")}
            </span>
          )}
          <Button type="submit" variant="secondary" loading={change.isPending}>
            {t("settings.changePassword")}
          </Button>
        </div>
      </form>
    </Section>
  );
}

export function SettingsPage() {
  const { t } = useI18n();
  const { data } = useSettings();
  const setup = useQuery({ queryKey: ["setup"], queryFn: api.setupStatus });
  if (!data) return <PageLoader />;
  return (
    <div className="animate-fade-in mx-auto max-w-3xl space-y-6">
      <PageHeader title={t("nav.settings")} />
      <GeneralSection settings={data} />
      <NetworkSection settings={data} />
      <StorageSection settings={data} defaults={data.defaults} />
      <AccountSection />
      <Section icon={<Info className="size-4" />} title={t("settings.section.about")}>
        <dl className="grid gap-2 text-sm sm:grid-cols-[10rem_1fr]">
          <dt className="text-muted">{t("settings.version")}</dt>
          <dd className="font-mono">{setup.data?.version}</dd>
        </dl>
        <p className="mt-3 text-sm text-muted">{t("settings.license")}</p>
      </Section>
    </div>
  );
}
