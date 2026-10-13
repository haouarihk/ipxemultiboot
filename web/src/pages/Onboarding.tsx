import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, ArrowRight, Check, Disc3, HardDrive, Network, Palette, PartyPopper, Power, Rocket } from "lucide-react";
import { useEffect, useState } from "react";
import { useNavigate } from "react-router";
import { AuthShell } from "../components/AuthShell";
import { NetworkCheckCard } from "../components/NetworkCheck";
import { BootstrapHints, DhcpModePicker, InterfaceSelect, IsoDirField, PoolFields } from "../components/NetworkFields";
import { PreferenceFields } from "../components/Preferences";
import { Alert, Button, Card, cn, Field, PageLoader } from "../components/ui";
import { api, type Settings } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { useSettings } from "../lib/session";
import { useTheme } from "../lib/theme";

const steps = [
  { key: "prefs", icon: Palette },
  { key: "storage", icon: HardDrive },
  { key: "network", icon: Network },
  { key: "done", icon: PartyPopper },
] as const;

export function OnboardingPage() {
  const { t, language, setLanguage } = useI18n();
  const { theme, setTheme } = useTheme();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const { data: settings } = useSettings();
  const [step, setStep] = useState(0);
  const [draft, setDraft] = useState<Settings | null>(null);

  useEffect(() => {
    if (settings && !draft) setDraft({ ...settings, language, theme });
  }, [settings, draft, language, theme]);

  const status = useQuery({ queryKey: ["status"], queryFn: api.status, enabled: step === 3 });

  const save = useMutation({
    mutationFn: (s: Partial<Settings>) => api.saveSettings(s),
    onSuccess: (data) => qc.setQueryData(["settings"], data),
  });
  const finish = useMutation({
    mutationFn: api.completeSetup,
    onSuccess: async (data) => {
      qc.setQueryData(["settings"], data);
      await qc.invalidateQueries({ queryKey: ["setup"] });
      navigate("/", { replace: true });
    },
  });

  if (!settings || !draft) return <PageLoader />;
  const set = (patch: Partial<Settings>) => setDraft({ ...draft, ...patch });

  const next = async () => {
    try {
      if (step === 0) await save.mutateAsync({ language: draft.language, theme: draft.theme });
      if (step === 1) await save.mutateAsync({ isoDir: draft.isoDir });
      if (step === 2) {
        await save.mutateAsync({
          interface: draft.interface,
          dhcpMode: draft.dhcpMode,
          poolStart: draft.poolStart,
          poolEnd: draft.poolEnd,
          router: draft.router,
          dns: draft.dns,
        });
        qc.invalidateQueries({ queryKey: ["status"] });
        qc.removeQueries({ queryKey: ["netcheck"] });
      }
      setStep(step + 1);
    } catch {
      /* error shown from mutation state */
    }
  };

  const current = steps[step];

  return (
    <AuthShell wide>
      <div className="animate-fade-in">
        <ol className="mb-6 flex items-center justify-center gap-2" aria-label={t("onboarding.step", { current: step + 1, total: steps.length })}>
          {steps.map((s, i) => {
            const Icon = s.icon;
            return (
              <li key={s.key} className="flex items-center gap-2">
                <div
                  className={cn(
                    "flex size-9 items-center justify-center rounded-full border transition",
                    i < step && "border-accent bg-accent text-accent-fg",
                    i === step && "border-accent bg-accent-soft text-accent ring-4 ring-ring",
                    i > step && "border-border bg-surface text-muted",
                  )}
                >
                  {i < step ? <Check className="size-4" /> : <Icon className="size-4" />}
                </div>
                {i < steps.length - 1 && <div className={cn("h-px w-8 sm:w-14", i < step ? "bg-accent" : "bg-border")} />}
              </li>
            );
          })}
        </ol>

        <Card className="p-6 sm:p-8">
          <p className="text-xs font-medium tracking-wide text-accent uppercase">
            {t("onboarding.step", { current: step + 1, total: steps.length })}
          </p>
          <h1 className="mt-1 text-2xl font-semibold tracking-tight">{t(`onboarding.${current.key}.title`)}</h1>
          <p className="mt-1.5 text-muted">{t(`onboarding.${current.key}.subtitle`)}</p>

          <div className="mt-6" key={current.key}>
            {current.key === "prefs" && (
              <PreferenceFields
                language={draft.language}
                theme={draft.theme}
                onLanguage={(c) => {
                  set({ language: c });
                  setLanguage(c);
                }}
                onTheme={(th) => {
                  set({ theme: th });
                  setTheme(th);
                }}
              />
            )}

            {current.key === "storage" && (
              <IsoDirField value={draft.isoDir} defaultPath={settings.defaults.isoDir} onChange={(v) => set({ isoDir: v })} />
            )}

            {current.key === "network" && (
              <div className="space-y-6">
                <Field label={t("settings.interface")}>
                  <InterfaceSelect value={draft.interface} onChange={(v) => set({ interface: v })} />
                </Field>
                <Field label={t("settings.dhcpMode")}>
                  <DhcpModePicker value={draft.dhcpMode} onChange={(m) => set({ dhcpMode: m })} />
                </Field>
                {draft.dhcpMode === "full" && <PoolFields value={draft} onChange={(v) => set(v)} />}
              </div>
            )}

            {current.key === "done" && (
              <div className="space-y-5">
                <NetworkCheckCard />
                {status.data?.server.dhcpMode === "bootstrap" && <BootstrapHints hints={status.data.server.bootstrap} />}
                <ol className="space-y-3">
                  {[
                    { icon: Disc3, text: t("onboarding.done.step1") },
                    { icon: Power, text: t("onboarding.done.step2") },
                    { icon: Rocket, text: t("onboarding.done.step3") },
                  ].map(({ icon: Icon, text }, i) => (
                    <li key={i} className="flex items-start gap-3 rounded-xl border border-border bg-surface-2/50 p-4">
                      <div className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-accent-soft text-accent">
                        <Icon className="size-4" />
                      </div>
                      <p className="pt-1 text-sm">{text}</p>
                    </li>
                  ))}
                </ol>
              </div>
            )}
          </div>

          {save.error && (
            <Alert tone="danger" className="mt-6">
              {save.error.message}
            </Alert>
          )}
          {finish.error && (
            <Alert tone="danger" className="mt-6">
              {finish.error.message}
            </Alert>
          )}

          <div className="mt-8 flex items-center justify-between gap-3">
            <Button variant="ghost" onClick={() => setStep(step - 1)} disabled={step === 0} icon={<ArrowLeft className="size-4" />}>
              {t("common.back")}
            </Button>
            {current.key === "done" ? (
              <Button onClick={() => finish.mutate()} loading={finish.isPending} icon={<Rocket className="size-4" />}>
                {t("onboarding.finish")}
              </Button>
            ) : (
              <Button onClick={next} loading={save.isPending}>
                {t("common.continue")}
                <ArrowRight className="size-4" />
              </Button>
            )}
          </div>
        </Card>
      </div>
    </AuthShell>
  );
}
