import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Cpu, MonitorSmartphone, X } from "lucide-react";
import { BootStageProgress } from "../components/BootStage";
import { Badge, Card, EmptyState, PageHeader, PageLoader } from "../components/ui";
import { api } from "../lib/api";
import { formatRelative } from "../lib/format";
import { useI18n } from "../lib/i18n";
import { useLiveEvents } from "../lib/events";

export function ClientsPage() {
  const { t, language } = useI18n();
  const qc = useQueryClient();
  useLiveEvents(1); // keeps the list fresh while a client is booting
  const { data: clients, isLoading } = useQuery({ queryKey: ["clients"], queryFn: api.clients, refetchInterval: 30000 });
  const forget = useMutation({
    mutationFn: api.deleteClient,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["clients"] }),
  });

  return (
    <div className="animate-fade-in space-y-6">
      <PageHeader title={t("clients.title")} description={t("clients.subtitle")} />
      <Card>
        {isLoading ? (
          <PageLoader />
        ) : !clients?.length ? (
          <EmptyState icon={<MonitorSmartphone className="size-5" />} title={t("clients.empty")} />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-left text-xs font-medium tracking-wide text-muted uppercase">
                  <th className="px-5 py-3">{t("clients.col.mac")}</th>
                  <th className="px-5 py-3">{t("clients.col.ip")}</th>
                  <th className="px-5 py-3">{t("clients.col.firmware")}</th>
                  <th className="px-5 py-3">{t("clients.col.attempt")}</th>
                  <th className="px-5 py-3">{t("clients.col.lastIpxe")}</th>
                  <th className="px-5 py-3">{t("clients.col.lastSeen")}</th>
                  <th className="w-12 px-5 py-3" />
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {clients.map((c) => (
                  <tr key={c.mac} className="group transition hover:bg-surface-2/50">
                    <td className="px-5 py-3.5 font-mono whitespace-nowrap">{c.mac}</td>
                    <td className="px-5 py-3.5 font-mono whitespace-nowrap text-muted">{c.ip || "—"}</td>
                    <td className="px-5 py-3.5">
                      {c.firmware ? (
                        <Badge>
                          <Cpu className="size-3" />
                          {c.firmware}
                        </Badge>
                      ) : (
                        "—"
                      )}
                    </td>
                    <td className="max-w-xs px-5 py-3.5" title={c.lastEvent}>
                      <BootStageProgress client={c} />
                    </td>
                    <td className="px-5 py-3.5 whitespace-nowrap" title={c.lastIpxe ? new Date(c.lastIpxe).toLocaleString(language) : undefined}>
                      {c.lastIpxe ? formatRelative(c.lastIpxe, language) : <span className="text-muted">{t("common.never")}</span>}
                    </td>
                    <td className="px-5 py-3.5 whitespace-nowrap text-muted" title={new Date(c.lastSeen).toLocaleString(language)}>
                      {formatRelative(c.lastSeen, language)}
                    </td>
                    <td className="px-5 py-3.5">
                      <button
                        onClick={() => forget.mutate(c.mac)}
                        title={t("clients.forget")}
                        aria-label={t("clients.forget")}
                        className="rounded-md p-1.5 text-muted opacity-60 transition group-hover:opacity-100 hover:bg-surface-2 hover:text-fg"
                      >
                        <X className="size-4" />
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
    </div>
  );
}
