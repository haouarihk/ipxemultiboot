// Typed client for the iPXE Multiboot JSON API.

export type DhcpMode = "proxy" | "full" | "bootstrap";
export type Theme = "system" | "light" | "dark";

export interface SetupStatus {
  needsRegistration: boolean;
  setupComplete: boolean;
  version: string;
  language: string;
  theme: Theme;
}

export interface User {
  id: number;
  username: string;
}

export interface Settings {
  setupComplete: boolean;
  language: string;
  theme: Theme;
  isoDir: string;
  interface: string;
  dhcpMode: DhcpMode;
  poolStart: string;
  poolEnd: string;
  router: string;
  dns: string;
}

export interface SettingsResponse extends Settings {
  defaults: Settings;
  languages: string[];
}

export interface ServiceState {
  state: "running" | "error" | "disabled" | "stopped";
  error?: string;
}

export interface ServerStatus {
  version: string;
  interface: string;
  serverIp: string;
  subnet: string;
  httpPort: number;
  menuUrl: string;
  dhcpMode: DhcpMode;
  services: Record<"dhcp" | "tftp" | "http", ServiceState>;
  bootstrap?: { nextServer: string; biosFile: string; uefiFile: string; ipxeUrl: string };
}

export interface Status {
  server: ServerStatus;
  storage: { dir: string; free: number; total: number; error?: string };
  isoCount: number;
  isoBytes: number;
  clients: number;
}

export interface NetInterface {
  name: string;
  mac: string;
  up: boolean;
  addresses: string[];
  virtual: boolean;
}

export interface Iso {
  id: string;
  name: string;
  path: string;
  size: number;
  modTime: string;
  volumeId: string;
  boot: string;
  kernel?: string;
}

export type BootStage = "" | "dhcp" | "tftp" | "ipxe" | "menu" | "boot";

export interface Client {
  mac: string;
  ip: string;
  firmware: string;
  lastIso: string;
  lastEvent: string;
  firstSeen: string;
  lastSeen: string;
  stage: BootStage;
  attemptAt: string | null;
  lastIpxe: string | null;
}

export type CheckStatus = "ok" | "info" | "warn" | "error";

export interface NetworkCheck {
  id: string;
  status: CheckStatus;
  title: string;
  detail?: string;
  suggestion?: string;
  fix?: { label: string; settings: Partial<Record<keyof Settings, string>> };
}

export interface DhcpOffer {
  serverId: string;
  yourIp?: string;
  nextServer?: string;
  bootFile?: string;
  router?: string;
  pxeVendor: boolean;
}

export interface NetworkReport {
  checkedAt: string;
  status: CheckStatus;
  counts: Partial<Record<CheckStatus, number>>;
  checks: NetworkCheck[];
  offers: DhcpOffer[];
  probed: boolean;
}

export interface BootEvent {
  id: number;
  time: string;
  kind: "dhcp" | "tftp" | "menu" | "boot" | "iso";
  mac?: string;
  ip?: string;
  message: string;
}

export interface DirCheck {
  path: string;
  exists: boolean;
  writable: boolean;
  isos: number;
  free?: number;
  error?: string;
}

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: "same-origin",
    headers: {
      "X-IPXEMultiboot": "1",
      ...(body !== undefined ? { "Content-Type": "application/json" } : {}),
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (res.status === 204) return undefined as T;
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new ApiError(res.status, data.error ?? res.statusText);
  return data as T;
}

export const api = {
  setupStatus: () => request<SetupStatus>("GET", "/api/setup/status"),
  register: (username: string, password: string) =>
    request<User>("POST", "/api/setup/register", { username, password }),
  login: (username: string, password: string) => request<User>("POST", "/api/auth/login", { username, password }),
  logout: () => request<void>("POST", "/api/auth/logout"),
  me: () => request<User>("GET", "/api/auth/me"),
  changePassword: (current: string, next: string) =>
    request<void>("PUT", "/api/auth/password", { current, new: next }),
  settings: () => request<SettingsResponse>("GET", "/api/settings"),
  saveSettings: (s: Partial<Settings>) => request<SettingsResponse>("PUT", "/api/settings", s),
  completeSetup: () => request<SettingsResponse>("POST", "/api/setup/complete"),
  status: () => request<Status>("GET", "/api/status"),
  interfaces: () => request<NetInterface[]>("GET", "/api/interfaces"),
  networkCheck: (fresh = false) => request<NetworkReport>("GET", `/api/network/check${fresh ? "?fresh=1" : ""}`),
  checkDir: (path: string) => request<DirCheck>("POST", "/api/fs/check", { path }),
  isos: () => request<Iso[]>("GET", "/api/isos"),
  deleteIso: (id: string) => request<void>("DELETE", `/api/isos/${encodeURIComponent(id)}`),
  clients: () => request<Client[]>("GET", "/api/clients"),
  deleteClient: (mac: string) => request<void>("DELETE", `/api/clients/${encodeURIComponent(mac)}`),
  events: () => request<BootEvent[]>("GET", "/api/events"),
};

/** Uploads a file with progress reporting. Resolves with the created ISO. */
export function uploadIso(
  file: File,
  opts: { overwrite?: boolean; onProgress?: (loaded: number, total: number) => void; signal?: AbortSignal },
): Promise<Iso> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    const q = new URLSearchParams({ name: file.name });
    if (opts.overwrite) q.set("overwrite", "1");
    xhr.open("PUT", `/api/isos/upload?${q}`);
    xhr.setRequestHeader("X-IPXEMultiboot", "1");
    xhr.setRequestHeader("Content-Type", "application/octet-stream");
    xhr.upload.onprogress = (e) => e.lengthComputable && opts.onProgress?.(e.loaded, e.total);
    xhr.onload = () => {
      let data: { error?: string } = {};
      try {
        data = JSON.parse(xhr.responseText);
      } catch {
        /* empty body */
      }
      if (xhr.status >= 200 && xhr.status < 300) resolve(data as Iso);
      else reject(new ApiError(xhr.status, data.error ?? xhr.statusText));
    };
    xhr.onerror = () => reject(new ApiError(0, "network error"));
    xhr.onabort = () => reject(new ApiError(0, "aborted"));
    opts.signal?.addEventListener("abort", () => xhr.abort());
    xhr.send(file);
  });
}
