// Список курсов для подменю «Курсы» в шапке (SiteHeader).
// Берётся из GET /api/courses (только опубликованные) и кешируется в памяти
// процесса – шапка рендерится на каждой странице, ходить в API каждый раз незачем.
// Новый курс появляется в меню сам после публикации в /admin/courses (в течение TTL).

import { apiJson } from './api';

export type NavCourse = { href: string; title: string };

const TTL_MS = 60_000;
let cache: { at: number; data: NavCourse[] } | null = null;

export async function getNavCourses(): Promise<NavCourse[]> {
  if (cache && Date.now() - cache.at < TTL_MS) return cache.data;
  try {
    const { status, data } = await apiJson<any[]>('/api/courses');
    if (status === 200 && Array.isArray(data)) {
      const list = data.map((c) => ({
        // Все курсы, включая бесплатный, живут на /courses/<slug>.
        href: `/courses/${c.slug}`,
        title: String(c.title || ''),
      }));
      cache = { at: Date.now(), data: list };
      return list;
    }
  } catch {}
  return cache?.data ?? [];
}
