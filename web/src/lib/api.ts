// Server-side API client (used inside Astro pages/middleware).
// In production behind nginx, hitting api:8080 directly bypasses nginx but is
// fine for SSR fetches. From the browser we always use relative /api paths.

const BASE = process.env.API_URL_INTERNAL || 'http://api:8080';

export type FetchInit = RequestInit & { cookie?: string };

export async function apiFetch(path: string, init: FetchInit = {}) {
  const headers = new Headers(init.headers || {});
  if (init.cookie) headers.set('cookie', init.cookie);
  if (init.body && !headers.has('content-type')) {
    headers.set('content-type', 'application/json');
  }
  const res = await fetch(BASE + path, { ...init, headers });
  return res;
}

export async function apiJson<T = any>(path: string, init: FetchInit = {}): Promise<{ status: number; data: T | null; setCookies: string[] }> {
  const res = await apiFetch(path, init);
  let data: any = null;
  try { data = await res.json(); } catch {}
  const setCookies: string[] = [];
  // collect Set-Cookie headers (for SSR pass-through on login)
  // @ts-ignore
  const raw = (res.headers as any).getSetCookie?.() || res.headers.get('set-cookie');
  if (Array.isArray(raw)) setCookies.push(...raw);
  else if (raw) setCookies.push(raw);
  return { status: res.status, data, setCookies };
}

export function userFromCookies(cookieHeader: string | null | undefined): { authed: boolean } {
  if (!cookieHeader) return { authed: false };
  return { authed: /(?:^|;\s*)access_token=/.test(cookieHeader) };
}
