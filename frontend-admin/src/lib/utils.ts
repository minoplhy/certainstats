export function fmtBytes(b: number) {
  if (!b) return "-";
  const u = ["B", "KB", "MB", "GB", "TB"];
  let v = b, i = 0;
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
  return `${v.toFixed(1)} ${u[i]}`;
}

export function fmtBps(v: number) {
  const u = ["B/s", "KB/s", "MB/s", "GB/s", "TB/s"];
  let i = 0;
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
  return `${v.toFixed(1)} ${u[i]}`;
}

export function fmtUptime(s: number) {
  if (!s) return "-";
  const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60);
  return d ? `${d}d ${h}h` : h ? `${h}h ${m}m` : `${m}m`;
}

export function getUsageColor(pct: number) {
  if (pct < 50) return "var(--status-online)"; // Green
  if (pct < 85) return "#eab308"; // Yellow
  return "var(--status-offline)"; // Red
}

export function isUnauthorized(err: unknown): boolean {
  if (typeof err === "object" && err !== null && "status" in err && (err as any).status === 401) {
    return true;
  }
  if (err instanceof Error) {
    const msg = err.message.toLowerCase();
    if (msg.includes("401") || msg.includes("unauthorized") || msg.includes("unauthenticated")) {
      return true;
    }
  }
  return false;
}
