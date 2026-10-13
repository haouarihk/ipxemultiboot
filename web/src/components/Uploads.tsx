import { useQueryClient } from "@tanstack/react-query";
import { CircleCheck, CircleX, Disc3, UploadCloud, X } from "lucide-react";
import { createContext, useCallback, useContext, useRef, useState, type ReactNode } from "react";
import { ApiError, uploadIso } from "../lib/api";
import { formatBytes } from "../lib/format";
import { useI18n } from "../lib/i18n";
import { Button, cn } from "./ui";

type UploadState = "uploading" | "done" | "error" | "cancelled" | "conflict";

interface Upload {
  id: number;
  file: File;
  loaded: number;
  state: UploadState;
  error?: string;
  startedAt: number;
  controller: AbortController;
}

interface UploadsCtx {
  uploads: Upload[];
  start: (files: FileList | File[]) => void;
  cancel: (id: number) => void;
  replace: (id: number) => void;
  clearFinished: () => void;
}

const Ctx = createContext<UploadsCtx | null>(null);

export function UploadProvider({ children }: { children: ReactNode }) {
  const qc = useQueryClient();
  const [uploads, setUploads] = useState<Upload[]>([]);
  const nextId = useRef(1);

  const update = useCallback((id: number, patch: Partial<Upload>) => {
    setUploads((list) => list.map((u) => (u.id === id ? { ...u, ...patch } : u)));
  }, []);

  const run = useCallback(
    (u: Upload, overwrite = false) => {
      uploadIso(u.file, {
        overwrite,
        signal: u.controller.signal,
        onProgress: (loaded) => update(u.id, { loaded }),
      })
        .then(() => {
          update(u.id, { state: "done", loaded: u.file.size });
          qc.invalidateQueries({ queryKey: ["isos"] });
          qc.invalidateQueries({ queryKey: ["status"] });
        })
        .catch((e: unknown) => {
          if (u.controller.signal.aborted) return update(u.id, { state: "cancelled" });
          if (e instanceof ApiError && e.status === 409) return update(u.id, { state: "conflict" });
          update(u.id, { state: "error", error: e instanceof Error ? e.message : String(e) });
        });
    },
    [qc, update],
  );

  const start = useCallback(
    (files: FileList | File[]) => {
      const added: Upload[] = Array.from(files).map((file) => {
        const isIso = file.name.toLowerCase().endsWith(".iso");
        return {
          id: nextId.current++,
          file,
          loaded: 0,
          state: isIso ? "uploading" : "error",
          error: isIso ? undefined : "not-iso",
          startedAt: Date.now(),
          controller: new AbortController(),
        };
      });
      setUploads((list) => [...list, ...added]);
      added.filter((u) => u.state === "uploading").forEach((u) => run(u));
    },
    [run],
  );

  const uploadsRef = useRef(uploads);
  uploadsRef.current = uploads;

  const cancel = useCallback((id: number) => {
    const u = uploadsRef.current.find((x) => x.id === id);
    if (u?.state === "uploading") u.controller.abort();
    else setUploads((list) => list.filter((x) => x.id !== id));
  }, []);

  const replace = useCallback(
    (id: number) => {
      const u = uploadsRef.current.find((x) => x.id === id);
      if (!u) return;
      const fresh: Upload = { ...u, state: "uploading", loaded: 0, controller: new AbortController(), startedAt: Date.now() };
      setUploads((list) => list.map((x) => (x.id === id ? fresh : x)));
      run(fresh, true);
    },
    [run],
  );

  const clearFinished = useCallback(() => {
    setUploads((list) => list.filter((u) => u.state === "uploading" || u.state === "conflict"));
  }, []);

  return (
    <Ctx.Provider value={{ uploads, start, cancel, replace, clearFinished }}>
      {children}
      <UploadPanel />
    </Ctx.Provider>
  );
}

export function useUploads(): UploadsCtx {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("useUploads outside UploadProvider");
  return ctx;
}

function UploadPanel() {
  const { t, language } = useI18n();
  const { uploads, cancel, replace, clearFinished } = useUploads();
  if (uploads.length === 0) return null;
  const finished = uploads.some((u) => u.state !== "uploading" && u.state !== "conflict");
  return (
    <div className="animate-fade-in fixed right-4 bottom-4 z-40 w-[min(24rem,calc(100vw-2rem))] overflow-hidden rounded-2xl border border-border bg-surface shadow-2xl">
      <div className="flex items-center justify-between border-b border-border px-4 py-3">
        <p className="flex items-center gap-2 font-medium">
          <UploadCloud className="size-4 text-accent" /> {t("upload.title")}
        </p>
        {finished && (
          <button onClick={clearFinished} className="text-xs text-muted hover:text-fg">
            {t("upload.clear")}
          </button>
        )}
      </div>
      <ul className="max-h-80 divide-y divide-border overflow-y-auto">
        {uploads.map((u) => {
          const pct = u.file.size ? Math.round((u.loaded / u.file.size) * 100) : 0;
          const elapsed = (Date.now() - u.startedAt) / 1000;
          const speed = elapsed > 1 ? u.loaded / elapsed : 0;
          return (
            <li key={u.id} className="px-4 py-3">
              <div className="flex items-center gap-3">
                <Disc3 className={cn("size-4 shrink-0 text-muted", u.state === "uploading" && "animate-spin [animation-duration:3s]")} />
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium" title={u.file.name}>
                    {u.file.name}
                  </p>
                  <p className="text-xs text-muted">
                    {u.state === "uploading" &&
                      `${pct}% · ${formatBytes(u.loaded, language)} / ${formatBytes(u.file.size, language)}${speed ? ` · ${formatBytes(speed, language)}/s` : ""}`}
                    {u.state === "done" && (
                      <span className="text-success">
                        {t("upload.done")} · {formatBytes(u.file.size, language)}
                      </span>
                    )}
                    {u.state === "cancelled" && t("upload.cancelled")}
                    {u.state === "conflict" && <span className="text-warning">{t("upload.exists", { name: u.file.name })}</span>}
                    {u.state === "error" && (
                      <span className="text-danger">
                        {u.error === "not-iso" ? t("upload.notIso", { name: u.file.name }) : `${t("upload.failed")}: ${u.error}`}
                      </span>
                    )}
                  </p>
                </div>
                {u.state === "done" && <CircleCheck className="size-4 text-success" />}
                {u.state === "error" && <CircleX className="size-4 text-danger" />}
                {u.state === "conflict" && (
                  <Button size="sm" variant="secondary" onClick={() => replace(u.id)}>
                    {t("upload.replace")}
                  </Button>
                )}
                <button
                  onClick={() => cancel(u.id)}
                  className="rounded-md p-1 text-muted hover:bg-surface-2 hover:text-fg"
                  aria-label={t("common.cancel")}
                >
                  <X className="size-4" />
                </button>
              </div>
              {u.state === "uploading" && (
                <div className="mt-2 h-1.5 overflow-hidden rounded-full bg-surface-2">
                  <div className="h-full rounded-full bg-accent transition-[width] duration-300" style={{ width: `${pct}%` }} />
                </div>
              )}
            </li>
          );
        })}
      </ul>
    </div>
  );
}

export function DropZone({ path, compact }: { path: string; compact?: boolean }) {
  const { t } = useI18n();
  const { start } = useUploads();
  const input = useRef<HTMLInputElement>(null);
  const [over, setOver] = useState(false);
  return (
    <div
      role="button"
      tabIndex={0}
      onClick={() => input.current?.click()}
      onKeyDown={(e) => (e.key === "Enter" || e.key === " ") && input.current?.click()}
      onDragOver={(e) => {
        e.preventDefault();
        setOver(true);
      }}
      onDragLeave={() => setOver(false)}
      onDrop={(e) => {
        e.preventDefault();
        setOver(false);
        if (e.dataTransfer.files.length) start(e.dataTransfer.files);
      }}
      className={cn(
        "group flex cursor-pointer flex-col items-center justify-center rounded-2xl border-2 border-dashed text-center transition",
        "focus-visible:outline-none focus-visible:ring-4 focus-visible:ring-ring",
        compact ? "px-4 py-6" : "px-6 py-10",
        over ? "border-accent bg-accent-soft/60" : "border-border hover:border-accent/60 hover:bg-surface-2/60",
      )}
    >
      <div
        className={cn(
          "mb-3 flex size-11 items-center justify-center rounded-xl transition",
          over ? "bg-accent text-accent-fg" : "bg-accent-soft text-accent group-hover:scale-105",
        )}
      >
        <UploadCloud className="size-5" />
      </div>
      <p className="font-medium">{over ? t("isos.dropActive") : t("isos.dropTitle")}</p>
      <p className="mt-1 text-sm text-muted">
        {t("isos.dropDesc", { path: "" })}
        <code className="rounded bg-surface-2 px-1.5 py-0.5 font-mono text-xs">{path}</code>
      </p>
      <input
        ref={input}
        type="file"
        accept=".iso,application/x-iso9660-image"
        multiple
        hidden
        onChange={(e) => {
          if (e.target.files?.length) start(e.target.files);
          e.target.value = "";
        }}
      />
    </div>
  );
}
