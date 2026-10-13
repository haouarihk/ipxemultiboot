import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronDown, CircleAlert, CircleCheck, Info, Radar, ShieldCheck, TriangleAlert, Wand2 } from "lucide-react";
import { useState } from "react";
import { api, type CheckStatus, type NetworkCheck as Check, type Settings } from "../lib/api";
import { formatTime } from "../lib/format";
import { useI18n } from "../lib/i18n";
import { Badge, Button, Card, CodeValue, cn } from "./ui";

const statusIcon: Record<CheckStatus, typeof Info> = {
  ok: CircleCheck,
  info: Info,
  warn: TriangleAlert,
  error: CircleAlert,
};

const statusColor: Record<CheckStatus, string> = {
  ok: "text-success",
  info: "text-muted",
  warn: "text-warning",
  error: "text-danger",
};

/** Runs automatically: on load, every minute, and after settings change. */
export function useNetworkCheck() {
  return useQuery({
    queryKey: ["netcheck"],
    queryFn: () => api.networkCheck(),
    staleTime: 30_000,
    refetchInterval: 60_000,
  });
}

function looksLikeCommand(s: string) {
  return /^(sudo |firewall-cmd|ufw )/.test(s);
}

function CheckRow({ check }: { check: Check }) {
  const { t } = useI18n();
  const qc = useQueryClient();
  const Icon = statusIcon[check.status];
  const fix = useMutation({
    mutationFn: () => api.saveSettings(check.fix!.settings as Partial<Settings>),
    onSuccess: async (data) => {
      qc.setQueryData(["settings"], data);
      qc.invalidateQueries({ queryKey: ["status"] });
      // Give the restarted DHCP service a moment, then re-scan without cache.
      await new Promise((r) => setTimeout(r, 800));
      qc.setQueryData(["netcheck"], await api.networkCheck(true));
    },
  });
  return (
    <li className="flex gap-3 px-5 py-3.5">
      <Icon className={cn("mt-0.5 size-4 shrink-0", statusColor[check.status])} />
      <div className="min-w-0 flex-1 space-y-1.5">
        <p className="text-sm font-medium">{check.title}</p>
        {check.detail && <p className="text-sm break-words text-muted">{check.detail}</p>}
        {check.suggestion &&
          (looksLikeCommand(check.suggestion) ? (
            <CodeValue value={check.suggestion} />
          ) : (
            <p className="text-sm break-words">
              <span className="font-medium text-accent">{t("netcheck.suggestion")}: </span>
              {check.suggestion}
            </p>
          ))}
        {check.fix && (
          <div className="pt-1">
            <Button size="sm" variant="secondary" icon={<Wand2 className="size-3.5" />} loading={fix.isPending} onClick={() => fix.mutate()}>
              {check.fix.label}
            </Button>
            {fix.error && <p className="mt-1 text-sm text-danger">{fix.error.message}</p>}
          </div>
        )}
      </div>
    </li>
  );
}

export function NetworkCheckCard({ className }: { className?: string }) {
  const { t, language } = useI18n();
  const { data, isLoading, isFetching, error } = useNetworkCheck();
  const [showPassed, setShowPassed] = useState(false);

  if (isLoading) {
    return (
      <Card className={cn("flex items-center gap-4 p-5", className)}>
        <div className="relative flex size-10 items-center justify-center rounded-xl bg-accent-soft text-accent">
          <Radar className="size-5 animate-spin [animation-duration:2.5s]" />
        </div>
        <div>
          <p className="font-medium">{t("netcheck.scanning")}</p>
          <p className="text-sm text-muted">{t("netcheck.scanningDesc")}</p>
        </div>
      </Card>
    );
  }
  if (error || !data) {
    return (
      <Card className={cn("p-5 text-sm text-danger", className)}>
        {t("netcheck.failed")}: {error?.message}
      </Card>
    );
  }

  const problems = data.checks.filter((c) => c.status === "error" || c.status === "warn");
  const passed = data.checks.filter((c) => c.status === "ok" || c.status === "info");
  const errors = data.counts.error ?? 0;
  const warnings = data.counts.warn ?? 0;
  const healthy = problems.length === 0;
  const servers = data.offers.filter((o) => o.yourIp);

  return (
    <Card className={cn("overflow-hidden", errors > 0 && "border-danger/40", className)}>
      <div
        className={cn(
          "flex flex-wrap items-center gap-4 px-5 py-4",
          errors > 0 ? "bg-danger-soft/60" : warnings > 0 ? "bg-warning-soft/60" : "bg-success-soft/50",
        )}
      >
        <div
          className={cn(
            "flex size-10 shrink-0 items-center justify-center rounded-xl",
            errors > 0 ? "bg-danger text-white" : warnings > 0 ? "bg-warning text-white" : "bg-success text-white",
          )}
        >
          {healthy ? <ShieldCheck className="size-5" /> : errors > 0 ? <CircleAlert className="size-5" /> : <TriangleAlert className="size-5" />}
        </div>
        <div className="min-w-0 flex-1">
          <p className="flex flex-wrap items-center gap-2 font-semibold">
            {healthy ? t("netcheck.healthy") : errors > 0 ? t("netcheck.attention") : t("netcheck.mostlyReady")}
            {errors > 0 && <Badge tone="danger">{t("netcheck.errorCount", { count: errors })}</Badge>}
            {warnings > 0 && <Badge tone="warning">{t("netcheck.warnCount", { count: warnings })}</Badge>}
          </p>
          <p className="text-sm text-muted">
            {healthy ? t("netcheck.healthyDesc") : t("netcheck.problemsDesc")}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2 text-xs text-muted">
          {data.probed && (
            <Badge>
              {t("netcheck.dhcpServers", { count: servers.length })}
              {servers.length > 0 && ` (${servers.map((o) => o.serverId).join(", ")})`}
            </Badge>
          )}
          <span className="flex items-center gap-1.5">
            {isFetching && <Radar className="size-3.5 animate-spin [animation-duration:2.5s]" />}
            {t("netcheck.checkedAt", { time: formatTime(data.checkedAt, language) })}
          </span>
        </div>
      </div>

      {problems.length > 0 && (
        <ul className="divide-y divide-border border-t border-border">
          {problems.map((c) => (
            <CheckRow key={c.id + c.title} check={c} />
          ))}
        </ul>
      )}

      {passed.length > 0 && (
        <div className="border-t border-border">
          <button
            onClick={() => setShowPassed(!showPassed)}
            aria-expanded={showPassed}
            className="flex w-full items-center gap-2 px-5 py-3 text-left text-sm text-muted transition hover:bg-surface-2/60 hover:text-fg"
          >
            <CircleCheck className="size-4 text-success" />
            {t("netcheck.passed", { count: passed.length })}
            <ChevronDown className={cn("ml-auto size-4 transition", showPassed && "rotate-180")} />
          </button>
          {showPassed && (
            <ul className="animate-fade-in divide-y divide-border border-t border-border">
              {passed.map((c) => (
                <CheckRow key={c.id + c.title} check={c} />
              ))}
            </ul>
          )}
        </div>
      )}
    </Card>
  );
}
