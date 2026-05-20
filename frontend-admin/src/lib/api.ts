import { getPanelPath, getPublicPath } from "./env";

/**
 * The dev-mode host prefix.
 * In production, the Go server serves everything, so no host prefix is needed.
 */
const DEV_HOST =
  import.meta.env.MODE === "development" ? "http://localhost:8080" : "";

/**
 * Resolve the mount-path prefix for a given endpoint.
 *
 * Rules:
 *  - `/api/public/*`  → public mount path  (PUBLIC_PATH)
 *  - everything else  → panel mount path   (PANEL_PATH)
 *
 * Both default to "/" so the behaviour is identical to the old hardcoded
 * version when the app is served from the root.
 */
function resolveBase(endpoint: string): string {
  const mountPath = endpoint.startsWith("/api/public")
    ? getPublicPath()
    : getPanelPath();

  // Normalise: strip trailing slash so we never get double-slash before endpoint
  const base = mountPath === "/" ? "" : mountPath.replace(/\/$/, "");
  return `${DEV_HOST}${base}`;
}

/**
 * Returns a fully qualified WebSocket URL for a given endpoint,
 * respecting the mount path and dev-mode host.
 */
export function getWSURL(endpoint: string): string {
  const mountPath = getPanelPath();
  const base = mountPath === "/" ? "" : mountPath.replace(/\/$/, "");
  
  let host = DEV_HOST;
  if (!host) {
    host = (window.location.protocol === "https:" ? "wss://" : "ws://") + window.location.host;
  } else {
    // Convert http(s) to ws(s) for dev host
    host = host.replace(/^http/, "ws");
  }

  return `${host}${base}${endpoint}`;
}

export async function fetchAPI<T = unknown>(
  endpoint: string,
  options?: RequestInit
): Promise<T> {
  const url = `${resolveBase(endpoint)}${endpoint}`;

  const res = await fetch(url, {
    ...options,
    credentials: "include", // required for session cookies
    headers: {
      "Content-Type": "application/json",
      ...options?.headers,
    },
  });

  if (!res.ok) {
    const errorText = await res.text();
    throw new Error(errorText || `Request failed: ${res.status}`);
  }

  const text = await res.text();
  return text ? (JSON.parse(text) as T) : ({} as T);
}
