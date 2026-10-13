export function formatBytes(bytes: number, locale?: string): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024)));
  const value = bytes / 1024 ** i;
  return `${value.toLocaleString(locale, { maximumFractionDigits: value < 10 && i > 0 ? 1 : 0 })} ${units[i]}`;
}

export function formatRelative(date: string | Date, locale?: string): string {
  const d = typeof date === "string" ? new Date(date) : date;
  const diff = (d.getTime() - Date.now()) / 1000;
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });
  const abs = Math.abs(diff);
  if (abs < 45) return rtf.format(Math.round(diff), "second");
  if (abs < 2700) return rtf.format(Math.round(diff / 60), "minute");
  if (abs < 64800) return rtf.format(Math.round(diff / 3600), "hour");
  if (abs < 2592000) return rtf.format(Math.round(diff / 86400), "day");
  return d.toLocaleDateString(locale);
}

export function formatTime(date: string, locale?: string): string {
  return new Date(date).toLocaleTimeString(locale, { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}
