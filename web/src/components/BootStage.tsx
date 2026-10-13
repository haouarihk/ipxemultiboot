import type { BootStage, Client } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { cn } from "./ui";

const order: Exclude<BootStage, "">[] = ["dhcp", "tftp", "ipxe", "menu", "boot"];
const IN_PROGRESS_MS = 90_000;

export function attemptState(c: Client): "progress" | "ok" | "stuck" | "none" {
  if (!c.stage) return "none";
  if (c.stage === "boot" || c.stage === "menu") return "ok";
  if (c.attemptAt && Date.now() - new Date(c.attemptAt).getTime() < IN_PROGRESS_MS) return "progress";
  return "stuck";
}

/** Five-step progress of the client's latest boot attempt, with a hint where it stopped. */
export function BootStageProgress({ client, compact }: { client: Client; compact?: boolean }) {
  const { t } = useI18n();
  const state = attemptState(client);
  const reached = client.stage ? order.indexOf(client.stage) : -1;
  if (state === "none") return <span className="text-muted">—</span>;
  return (
    <div className="min-w-0 space-y-1">
      <div className="flex items-center gap-1" aria-label={t(`stage.${client.stage || "dhcp"}`)}>
        {order.map((s, i) => (
          <span
            key={s}
            title={t(`stage.${s}`)}
            className={cn(
              "h-1.5 w-5 rounded-full",
              i <= reached
                ? state === "stuck" && i === reached
                  ? "bg-warning"
                  : state === "progress" && i === reached
                    ? "animate-pulse-dot bg-accent"
                    : "bg-success"
                : "bg-surface-2",
            )}
          />
        ))}
        <span className={cn("ml-1.5 text-xs font-medium", state === "stuck" ? "text-warning" : state === "ok" ? "text-success" : "text-accent")}>
          {t(`stage.${client.stage || "dhcp"}`)}
        </span>
      </div>
      {!compact && state === "stuck" && <p className="text-xs text-muted">{t(`stage.hint.${client.stage as Exclude<BootStage, "" | "menu" | "boot">}`)}</p>}
      {!compact && client.stage === "boot" && client.lastIso && <p className="truncate text-xs text-muted">{client.lastIso}</p>}
    </div>
  );
}
