import { useQuery } from "@tanstack/react-query";
import {
  Activity,
  ArrowRight,
  Cable,
  Disc3,
  HardDrive,
  MonitorSmartphone,
  Network,
  Power,
  Radio,
  Server,
  Upload,
} from "lucide-react";
import type { ReactNode } from "react";
import { Link } from "react-router";
import { BootStageProgress } from "../components/BootStage";
import { NetworkCheckCard } from "../components/NetworkCheck";
import { BootstrapHints } from "../components/NetworkFields";
import { Badge, Button, Card, CardHeader, CodeValue, EmptyState, PageHeader, PageLoader, StatusDot, cn } from "../components/ui";
import { DropZone } from "../components/Uploads";
import { api, type BootEvent, type ServiceState } from "../lib/api";
import { useLiveEvents } from "../lib/events";
import { formatBytes, formatRelative, formatTime } from "../lib/format";
import { useI18n } from "../lib/i18n";

function serviceTone(s: ServiceState) {
  return s.state === "running" ? "success" : s.state === "error" ? "danger" : "neutral";
}

function StatCard({ icon, label, value, sub, to }: { icon: ReactNode; label: string; value: ReactNode; sub?: ReactNode; to?: string }) {
  const body = (
    <Card className={cn("h-full p-4 transition sm:p-5", to && "hover:border-accent/50 hover:shadow-md")}>
      <div className="flex items-center justify-between">
        <p className="text-sm font-medium text-muted">{label}</p>
        <div className="flex size-8 items-center justify-center rounded-lg bg-accent-soft text-accent">{icon}</div>
      </div>
      <p className="mt-3 truncate text-xl font-semibold tracking-tight sm:text-2xl">{value}</p>
      {sub && <div className="mt-1 text-sm text-muted">{sub}</div>}
    </Card>
  );
  return to ? (
    <Link to={to} className="block rounded-2xl focus-visible:outline-none focus-visible:ring-4 focus-visible:ring-ring">
      {body}
    </Link>
  ) : (
    body
  );
}

const eventIcon: Record<BootEvent["kind"], typeof Activity> = {
  dhcp: Radio,
  tftp: Cable,
  menu: MonitorSmartphone,
  boot: Power,
  iso: Disc3,
};

function ActivityFeed() {
  const { t, language } = useI18n();
  const { events, live, isLoading } = useLiveEvents(40);
  return (
    <Card className="flex h-full flex-col">
      <CardHeader
        icon={<Activity className="size-4" />}
        title={t("dashboard.activity")}
        action={
          <Badge tone={live ? "success" : "neutral"}>
            <StatusDot tone={live ? "success" : "neutral"} pulse={live} />
            {t("dashboard.live")}
          </Badge>
        }
      />
      {isLoading ? (
        <PageLoader />
      ) : events.length === 0 ? (
        <EmptyState icon={<Activity className="size-5" />} title={t("dashboard.activityEmpty")} />
      ) : (
        <ul className="max-h-[28rem] flex-1 divide-y divide-border overflow-y-auto">
          {events.map((e) => {
            const Icon = eventIcon[e.kind] ?? Activity;
            return (
              <li key={e.id} className="animate-fade-in flex items-start gap-3 px-5 py-3">
                <div
                  className={cn(
                    "mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-lg",
                    e.kind === "boot" ? "bg-accent text-accent-fg" : "bg-surface-2 text-muted",
                  )}
                >
                  <Icon className="size-3.5" />
                </div>
                <div className="min-w-0 flex-1">
                  <p className="text-sm break-words">{e.message}</p>
                  <p className="mt-0.5 font-mono text-xs text-muted">
                    {[e.mac, e.ip].filter(Boolean).join(" · ") || e.kind.toUpperCase()}
                  </p>
                </div>
                <time className="shrink-0 text-xs text-muted tabular-nums">{formatTime(e.time, language)}</time>
              </li>
            );
          })}
        </ul>
      )}
    </Card>
  );
}

function RecentClients() {
  const { t, language } = useI18n();
  const { data: clients = [] } = useQuery({ queryKey: ["clients"], queryFn: api.clients, refetchInterval: 15000 });
  return (
    <Card>
      <CardHeader
        icon={<MonitorSmartphone className="size-4" />}
        title={t("dashboard.recentClients")}
        action={
          <Link to="/clients" className="inline-flex items-center gap-1 text-sm font-medium text-accent hover:underline">
            {t("dashboard.viewAll")} <ArrowRight className="size-4" />
          </Link>
        }
      />
      {clients.length === 0 ? (
        <EmptyState icon={<MonitorSmartphone className="size-5" />} title={t("clients.empty")} />
      ) : (
        <ul className="divide-y divide-border">
          {clients.slice(0, 5).map((c) => (
            <li key={c.mac} className="grid grid-cols-1 gap-2 px-5 py-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1.3fr)_auto] sm:items-center sm:gap-4">
              <div className="min-w-0">
                <p className="truncate font-mono text-sm">{c.mac}</p>
                <p className="truncate text-xs text-muted">{[c.ip, c.firmware].filter(Boolean).join(" · ") || "—"}</p>
              </div>
              <BootStageProgress client={c} compact />
              <p className="text-xs whitespace-nowrap text-muted sm:text-right" title={c.lastIpxe ? new Date(c.lastIpxe).toLocaleString(language) : undefined}>
                {t("clients.col.lastIpxe")}: {c.lastIpxe ? formatRelative(c.lastIpxe, language) : t("common.never")}
              </p>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}

function LibraryPreview() {
  const { t, language } = useI18n();
  const { data: isos = [] } = useQuery({ queryKey: ["isos"], queryFn: api.isos });
  const recent = [...isos].sort((a, b) => b.modTime.localeCompare(a.modTime)).slice(0, 6);
  return (
    <Card>
      <CardHeader
        icon={<Disc3 className="size-4" />}
        title={t("nav.isos")}
        action={
          <Link to="/isos" className="inline-flex items-center gap-1 text-sm font-medium text-accent hover:underline">
            {t("dashboard.viewAll")} <ArrowRight className="size-4" />
          </Link>
        }
      />
      <ul className="grid sm:grid-cols-2 lg:grid-cols-3">
        {recent.map((iso) => (
          <li key={iso.id} className="-mt-px flex items-center gap-3 border-t border-border px-5 py-3.5 first:mt-0 first:border-t-0 sm:[&:nth-child(-n+2)]:border-t-0 lg:[&:nth-child(-n+3)]:border-t-0">
            <div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-surface-2 text-muted">
              <Disc3 className="size-4" />
            </div>
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm font-medium" title={iso.name}>
                {iso.name}
              </p>
              <p className="text-xs text-muted">
                {formatBytes(iso.size, language)} · {iso.boot === "sanboot" ? t("isos.boot.sanboot") : iso.boot}
              </p>
            </div>
          </li>
        ))}
      </ul>
    </Card>
  );
}

export function DashboardPage() {
  const { t, language } = useI18n();
  const { data, isLoading } = useQuery({ queryKey: ["status"], queryFn: api.status, refetchInterval: 5000 });
  if (isLoading || !data) return <PageLoader />;
  const { server, storage } = data;
  const usedPct = storage.total ? Math.round(((storage.total - storage.free) / storage.total) * 100) : 0;

  return (
    <div className="animate-fade-in space-y-6">
      <PageHeader
        title={t("dashboard.title")}
        description={t("dashboard.subtitle")}
        action={
          <Link to="/isos">
            <Button icon={<Upload className="size-4" />}>{t("dashboard.addIsos")}</Button>
          </Link>
        }
      />

      <NetworkCheckCard />

      <div className="grid grid-cols-2 gap-3 sm:gap-4 xl:grid-cols-4">
        <StatCard
          icon={<Server className="size-4" />}
          label={t("dashboard.server")}
          value={<span className="font-mono text-base sm:text-xl">{server.serverIp || "—"}</span>}
          sub={
            <span className="flex items-center gap-1.5">
              <Network className="size-3.5" />
              {server.interface || t("dashboard.noInterface")} · {t(`settings.mode.${server.dhcpMode}`)}
            </span>
          }
        />
        <StatCard
          icon={<Disc3 className="size-4" />}
          label={t("dashboard.isos")}
          value={data.isoCount}
          sub={formatBytes(data.isoBytes, language)}
          to="/isos"
        />
        <StatCard
          icon={<HardDrive className="size-4" />}
          label={t("dashboard.storage")}
          value={`${usedPct}%`}
          sub={
            <div className="space-y-2">
              <div className="h-1.5 overflow-hidden rounded-full bg-surface-2">
                <div
                  className={cn("h-full rounded-full", usedPct > 90 ? "bg-danger" : usedPct > 75 ? "bg-warning" : "bg-accent")}
                  style={{ width: `${usedPct}%` }}
                />
              </div>
              <p>{t("dashboard.storageFree", { free: formatBytes(storage.free, language), total: formatBytes(storage.total, language) })}</p>
            </div>
          }
        />
        <StatCard icon={<MonitorSmartphone className="size-4" />} label={t("dashboard.clients")} value={data.clients} to="/clients" />
      </div>

      <div className="grid gap-6 lg:grid-cols-5">
        <div className="space-y-6 lg:col-span-2">
          <Card>
            <CardHeader icon={<Radio className="size-4" />} title={t("dashboard.services")} />
            <ul className="divide-y divide-border">
              {(["dhcp", "tftp", "http"] as const).map((name) => {
                const s = server.services[name];
                return (
                  <li key={name} className="px-5 py-3.5">
                    <div className="flex items-center justify-between gap-3">
                      <span className="font-medium">{t(`service.${name}`)}</span>
                      <Badge tone={serviceTone(s)}>
                        <StatusDot tone={serviceTone(s)} />
                        {t(`service.${s.state}`)}
                      </Badge>
                    </div>
                    {s.error && <p className="mt-2 text-sm break-words text-danger">{s.error}</p>}
                  </li>
                );
              })}
            </ul>
            {server.menuUrl && (
              <div className="space-y-1.5 border-t border-border px-5 py-4">
                <p className="text-sm text-muted">{t("dashboard.menuUrl")}</p>
                <CodeValue value={server.menuUrl} />
              </div>
            )}
          </Card>
          {server.dhcpMode === "bootstrap" && (
            <Card className="p-5">
              <BootstrapHints hints={server.bootstrap} />
            </Card>
          )}
          {data.isoCount === 0 && (
            <Card className="p-5">
              <p className="font-medium">{t("dashboard.noIsosTitle")}</p>
              <p className="mt-1 mb-4 text-sm text-muted">{t("dashboard.noIsosDesc", { path: storage.dir })}</p>
              <DropZone path={storage.dir} compact />
            </Card>
          )}
        </div>
        <div className="lg:col-span-3">
          <ActivityFeed />
        </div>
      </div>

      <RecentClients />

      {data.isoCount > 0 && <LibraryPreview />}
    </div>
  );
}
