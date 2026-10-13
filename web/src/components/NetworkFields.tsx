import { useQuery } from "@tanstack/react-query";
import { CircleCheck, FolderOpen, Network, RadioTower, Router, Server } from "lucide-react";
import { useEffect, useState } from "react";
import { api, type DhcpMode, type ServerStatus, type Settings } from "../lib/api";
import { formatBytes } from "../lib/format";
import { useI18n } from "../lib/i18n";
import { Alert, Badge, Button, CodeValue, cn, Field, Input, Select, Spinner } from "./ui";

export function InterfaceSelect({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const { t } = useI18n();
  const { data: ifaces = [] } = useQuery({ queryKey: ["interfaces"], queryFn: api.interfaces });
  return (
    <Select value={value} onChange={(e) => onChange(e.target.value)}>
      <option value="">{t("settings.interfaceAuto")}</option>
      {ifaces.map((i) => (
        <option key={i.name} value={i.name}>
          {i.name}
          {i.addresses.length ? ` — ${i.addresses.join(", ")}` : ""}
          {i.virtual ? ` (${t("settings.interfaceVirtual")})` : ""}
          {!i.up ? ` (${t("settings.interfaceDown")})` : ""}
        </option>
      ))}
    </Select>
  );
}

const modeIcons: Record<DhcpMode, typeof Network> = { proxy: RadioTower, full: Router, bootstrap: Server };

export function DhcpModePicker({ value, onChange }: { value: DhcpMode; onChange: (m: DhcpMode) => void }) {
  const { t } = useI18n();
  const modes: DhcpMode[] = ["proxy", "full", "bootstrap"];
  return (
    <div role="radiogroup" className="grid gap-3">
      {modes.map((m) => {
        const Icon = modeIcons[m];
        const active = value === m;
        return (
          <button
            key={m}
            type="button"
            role="radio"
            aria-checked={active}
            onClick={() => onChange(m)}
            className={cn(
              "flex items-start gap-4 rounded-xl border p-4 text-left transition",
              active ? "border-accent bg-accent-soft/60 ring-4 ring-ring" : "border-border bg-surface hover:bg-surface-2",
            )}
          >
            <div
              className={cn(
                "flex size-10 shrink-0 items-center justify-center rounded-lg",
                active ? "bg-accent text-accent-fg" : "bg-surface-2 text-muted",
              )}
            >
              <Icon className="size-5" />
            </div>
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-medium">{t(`settings.mode.${m}`)}</span>
                {m === "proxy" && <Badge tone="accent">{t("common.recommended")}</Badge>}
              </div>
              <p className="mt-1 text-sm text-muted">{t(`settings.mode.${m}.desc`)}</p>
            </div>
            <CircleCheck className={cn("size-5 shrink-0 transition", active ? "text-accent" : "text-transparent")} />
          </button>
        );
      })}
    </div>
  );
}

type PoolValues = Pick<Settings, "poolStart" | "poolEnd" | "router" | "dns">;

export function PoolFields({ value, onChange }: { value: PoolValues; onChange: (v: PoolValues) => void }) {
  const { t } = useI18n();
  const set = (k: keyof PoolValues) => (e: React.ChangeEvent<HTMLInputElement>) =>
    onChange({ ...value, [k]: e.target.value });
  return (
    <div className="grid gap-4 rounded-xl border border-border bg-surface-2/50 p-4 sm:grid-cols-2">
      <Field label={t("settings.poolStart")}>
        <Input value={value.poolStart} onChange={set("poolStart")} placeholder="192.168.50.100" inputMode="decimal" />
      </Field>
      <Field label={t("settings.poolEnd")}>
        <Input value={value.poolEnd} onChange={set("poolEnd")} placeholder="192.168.50.200" inputMode="decimal" />
      </Field>
      <Field label={`${t("settings.router")} (${t("common.optional")})`}>
        <Input value={value.router} onChange={set("router")} placeholder="192.168.50.1" inputMode="decimal" />
      </Field>
      <Field label={`${t("settings.dns")} (${t("common.optional")})`} hint={t("settings.dnsHint")}>
        <Input value={value.dns} onChange={set("dns")} placeholder="1.1.1.1, 9.9.9.9" />
      </Field>
    </div>
  );
}

export function BootstrapHints({ hints }: { hints?: ServerStatus["bootstrap"] }) {
  const { t } = useI18n();
  if (!hints?.nextServer) return null;
  const rows: [string, string][] = [
    [t("bootstrap.nextServer"), hints.nextServer],
    [t("bootstrap.biosFile"), hints.biosFile],
    [t("bootstrap.uefiFile"), hints.uefiFile],
    [t("bootstrap.ipxeUrl"), hints.ipxeUrl],
  ];
  return (
    <div className="space-y-3 rounded-xl border border-border bg-surface-2/50 p-4">
      <div>
        <p className="font-medium">{t("bootstrap.title")}</p>
        <p className="text-sm text-muted">{t("bootstrap.desc")}</p>
      </div>
      <dl className="grid gap-3">
        {rows.map(([label, v]) => (
          <div key={label} className="grid gap-1 sm:grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)] sm:items-center sm:gap-4">
            <dt className="text-sm text-muted">{label}</dt>
            <dd className="min-w-0">
              <CodeValue value={v} />
            </dd>
          </div>
        ))}
      </dl>
      <p className="text-xs text-muted">{t("bootstrap.note")}</p>
    </div>
  );
}

export function IsoDirField({
  value,
  defaultPath,
  onChange,
}: {
  value: string;
  defaultPath: string;
  onChange: (v: string) => void;
}) {
  const { t, language } = useI18n();
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const id = setTimeout(() => setDebounced(value), 400);
    return () => clearTimeout(id);
  }, [value]);
  const check = useQuery({
    queryKey: ["dircheck", debounced],
    queryFn: () => api.checkDir(debounced),
    enabled: debounced.trim().length > 0,
  });
  const c = check.data;
  return (
    <div className="space-y-3">
      <Field label={t("settings.isoDir")} hint={t("settings.isoDirHint", { path: defaultPath })}>
        <div className="flex gap-2">
          <div className="relative flex-1">
            <FolderOpen className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted" />
            <Input
              value={value}
              onChange={(e) => onChange(e.target.value)}
              className="pl-9 font-mono"
              spellCheck={false}
              placeholder="/srv/isos"
            />
          </div>
          {value !== defaultPath && (
            <Button type="button" variant="secondary" onClick={() => onChange(defaultPath)}>
              {t("settings.isoDirReset")}
            </Button>
          )}
        </div>
      </Field>
      {check.isFetching && !c ? (
        <Spinner className="size-4" />
      ) : c ? (
        c.exists && c.writable ? (
          <Alert tone="success">
            {t("settings.dirCheck.ok", { count: c.isos, free: formatBytes(c.free ?? 0, language) })}
          </Alert>
        ) : !c.exists && c.error?.includes("does not exist") ? (
          <Alert tone="neutral">{t("settings.dirCheck.missing")}</Alert>
        ) : c.exists && !c.writable ? (
          <Alert tone="warning">{t("settings.dirCheck.readonly")}</Alert>
        ) : (
          <Alert tone="danger">{c.error}</Alert>
        )
      ) : null}
    </div>
  );
}
