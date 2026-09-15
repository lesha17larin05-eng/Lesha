// Настройки сайта (булевы флаги) для SSR: читаются из /api/settings.
// Кешируются в памяти процесса на TTL – чтобы не ходить в API на каждый
// запрос страницы. После переключения тумблера в админке изменения
// появляются на сайте в течение TTL.

import { apiJson } from './api';

export type SiteSettings = { salut_visible: boolean };

const DEFAULTS: SiteSettings = { salut_visible: false };
const TTL_MS = 10_000;

let cache: { at: number; data: SiteSettings } | null = null;

export async function getSiteSettings(): Promise<SiteSettings> {
  if (cache && Date.now() - cache.at < TTL_MS) return cache.data;
  try {
    const { status, data } = await apiJson<Record<string, boolean>>('/api/settings');
    if (status === 200 && data && typeof data === 'object') {
      const merged = { ...DEFAULTS, ...data } as SiteSettings;
      cache = { at: Date.now(), data: merged };
      return merged;
    }
  } catch {}
  // API недоступен – отдаём дефолты (и не кешируем, чтобы подхватить,
  // как только API поднимется).
  return cache?.data ?? DEFAULTS;
}

// Сброс кеша – используется админской страницей настроек сразу после PATCH,
// чтобы шапка обновилась без ожидания TTL (в рамках этого SSR-процесса).
export function invalidateSiteSettings() {
  cache = null;
}
