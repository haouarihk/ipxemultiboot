import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Disc3, Search, Trash2, Zap } from "lucide-react";
import { useMemo, useState } from "react";
import { Badge, Button, Card, Dialog, EmptyState, Input, PageHeader, PageLoader } from "../components/ui";
import { DropZone } from "../components/Uploads";
import { api, type Iso } from "../lib/api";
import { formatBytes, formatRelative } from "../lib/format";
import { useI18n } from "../lib/i18n";
import { useSettings } from "../lib/session";

const familyLabels: Record<string, string> = {
  archiso: "Arch Linux",
  casper: "Ubuntu",
  "debian-live": "Debian Live",
  "debian-installer": "Debian Installer",
  "fedora-live": "Fedora Live",
  anaconda: "Fedora/RHEL Installer",
  alpine: "Alpine",
  tinycore: "Tiny Core",
};

function BootBadge({ iso }: { iso: Iso }) {
  const { t } = useI18n();
  if (iso.boot === "sanboot") {
    return (
      <span title={t("isos.boot.sanbootHint")}>
        <Badge>{t("isos.boot.sanboot")}</Badge>
      </span>
    );
  }
  return (
    <span title={t("isos.boot.recipeHint")}>
      <Badge tone="accent">
        <Zap className="size-3" />
        {familyLabels[iso.boot] ?? iso.boot}
      </Badge>
    </span>
  );
}

export function IsosPage() {
  const { t, language } = useI18n();
  const qc = useQueryClient();
  const { data: settings } = useSettings();
  const { data: isos, isLoading } = useQuery({ queryKey: ["isos"], queryFn: api.isos, refetchInterval: 15000 });
  const [query, setQuery] = useState("");
  const [toDelete, setToDelete] = useState<Iso | null>(null);

  const del = useMutation({
    mutationFn: (id: string) => api.deleteIso(id),
    onSuccess: () => {
      setToDelete(null);
      qc.invalidateQueries({ queryKey: ["isos"] });
      qc.invalidateQueries({ queryKey: ["status"] });
    },
  });

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return (isos ?? []).filter((i) => !q || i.name.toLowerCase().includes(q) || i.volumeId.toLowerCase().includes(q));
  }, [isos, query]);

  const totalSize = (isos ?? []).reduce((n, i) => n + i.size, 0);

  return (
    <div className="animate-fade-in space-y-6">
      <PageHeader title={t("isos.title")} description={t("isos.subtitle")} />

      <DropZone path={settings?.isoDir ?? ""} />

      <Card>
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-5 py-3">
          <p className="text-sm text-muted">
            {t("isos.total", { count: isos?.length ?? 0, size: formatBytes(totalSize, language) })}
          </p>
          <div className="relative w-full sm:w-64">
            <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted" />
            <Input value={query} onChange={(e) => setQuery(e.target.value)} placeholder={t("isos.search")} className="h-9 pl-9" />
          </div>
        </div>

        {isLoading ? (
          <PageLoader />
        ) : filtered.length === 0 ? (
          <EmptyState icon={<Disc3 className="size-5" />} title={t("isos.empty")} />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-left text-xs font-medium tracking-wide text-muted uppercase">
                  <th className="px-5 py-3">{t("isos.col.name")}</th>
                  <th className="px-5 py-3">{t("isos.col.boot")}</th>
                  <th className="px-5 py-3 text-right">{t("isos.col.size")}</th>
                  <th className="hidden px-5 py-3 md:table-cell">{t("isos.col.added")}</th>
                  <th className="w-12 px-5 py-3" />
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {filtered.map((iso) => (
                  <tr key={iso.id} className="group transition hover:bg-surface-2/50">
                    <td className="max-w-0 px-5 py-3.5 sm:max-w-md">
                      <div className="flex items-center gap-3">
                        <div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-surface-2 text-muted">
                          <Disc3 className="size-4" />
                        </div>
                        <div className="min-w-0">
                          <p className="truncate font-medium" title={iso.path}>
                            {iso.name}
                          </p>
                          {iso.volumeId && <p className="truncate font-mono text-xs text-muted">{iso.volumeId}</p>}
                        </div>
                      </div>
                    </td>
                    <td className="px-5 py-3.5">
                      <BootBadge iso={iso} />
                    </td>
                    <td className="px-5 py-3.5 text-right whitespace-nowrap tabular-nums">{formatBytes(iso.size, language)}</td>
                    <td className="hidden px-5 py-3.5 whitespace-nowrap text-muted md:table-cell">{formatRelative(iso.modTime, language)}</td>
                    <td className="px-5 py-3.5">
                      <button
                        onClick={() => setToDelete(iso)}
                        className="rounded-md p-1.5 text-muted opacity-60 transition group-hover:opacity-100 hover:bg-danger-soft hover:text-danger"
                        aria-label={`${t("common.delete")} ${iso.name}`}
                      >
                        <Trash2 className="size-4" />
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      <Dialog
        open={!!toDelete}
        onClose={() => setToDelete(null)}
        title={t("isos.deleteTitle", { name: toDelete?.name ?? "" })}
        description={t("isos.deleteDesc")}
        footer={
          <>
            <Button variant="secondary" onClick={() => setToDelete(null)}>
              {t("common.cancel")}
            </Button>
            <Button
              variant="danger"
              loading={del.isPending}
              icon={<Trash2 className="size-4" />}
              onClick={() => toDelete && del.mutate(toDelete.id)}
            >
              {t("common.delete")}
            </Button>
          </>
        }
      >
        {del.error && <p className="text-sm text-danger">{del.error.message}</p>}
      </Dialog>
    </div>
  );
}
