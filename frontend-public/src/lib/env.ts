/**
 * env.ts — Runtime environment helpers
 *
 * The Go SPA handler injects `window.__APP_ENV__` into the served index.html
 * before the closing </head> tag. This module provides typed accessors for
 * those values so the rest of the frontend can use them safely.
 *
 * Key contract:
 *   - `panelSpa`  injects both PANEL_PATH and PUBLIC_PATH → panel context
 *   - `publicSpa` injects only PUBLIC_PATH               → public context
 *
 * Therefore: `isPanelContext()` ⟺ PANEL_PATH is present in __APP_ENV__.
 */

declare global {
  interface Window {
    __APP_ENV__?: {
      PANEL_PATH?: string;
      PUBLIC_PATH?: string;
    };
  }
}

function getRawEnv(): NonNullable<Window["__APP_ENV__"]> {
  if (typeof window === "undefined") return {};
  return window.__APP_ENV__ ?? {};
}

/**
 * Returns true only when this page was served by the panel SPA handler,
 * i.e. when PANEL_PATH was injected into window.__APP_ENV__.
 *
 * Three states:
 *  - __APP_ENV__ is undefined  → Go did not inject anything (dev mode /
 *    direct Next.js server). Treat as panel context so development works.
 *  - __APP_ENV__ has PANEL_PATH → served by panelSpa. Panel context.
 *  - __APP_ENV__ has no PANEL_PATH → served by publicSpa. NOT panel context.
 */
export function isPanelContext(): boolean {
  if (typeof window === "undefined") return true; 
  // In dev mode (no injection), we treat it as panel context.
  if (window.__APP_ENV__ === undefined) return true; 
  return typeof window.__APP_ENV__.PANEL_PATH !== "undefined";
}

/**
 * Returns true only when served by the public dashboard handler.
 */
export function isPublicContext(): boolean {
  if (typeof window === "undefined") return false;
  if (window.__APP_ENV__ === undefined) return false;
  // Public context has PUBLIC_PATH but NO PANEL_PATH.
  return typeof window.__APP_ENV__.PUBLIC_PATH !== "undefined" && 
         typeof window.__APP_ENV__.PANEL_PATH === "undefined";
}

/**
 * The base path the admin panel is mounted on, e.g. "/" or "/admin".
 * Falls back to "/" if the env is absent (dev mode / same-path config).
 */
export function getPanelPath(): string {
  return getRawEnv().PANEL_PATH ?? "/";
}

/**
 * The base path the public dashboard is mounted on, e.g. "/" or "/status".
 * Falls back to "/" when not set.
 */
export function getPublicPath(): string {
  const env = getRawEnv();
  const pub = env.PUBLIC_PATH ?? "/";
  
  // Default Config: if PUBLIC_PATH is unset or "/", we shift 
  // dashboards to "/dashboard" to keep the root clean.
  if (pub === "/") {
    return "/dashboard";
  }
  return pub;
}

/**
 * Resolves a path relative to the panel's mount point.
 * Ensures the result starts with a single slash and has no double slashes.
 */
export function resolvePanelPath(path: string): string {
  const base = getPanelPath().replace(/\/$/, "");
  const cleanPath = path.startsWith("/") ? path : `/${path}`;
  return `${base}${cleanPath}` || "/";
}

/**
 * Resolves a path relative to the public dashboard's mount point.
 */
export function resolvePublicPath(path: string): string {
  const base = getPublicPath().replace(/\/$/, "");
  const cleanPath = path.startsWith("/") ? path : `/${path}`;
  return `${base}${cleanPath}` || "/";
}
