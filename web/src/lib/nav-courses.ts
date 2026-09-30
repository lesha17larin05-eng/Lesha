// Список курсов для подменю «Курсы» в шапке (SiteHeader).
// Берётся из GET /api/courses (только опубликованные) и кешируется в памяти
// процесса – шапка рендерится на каждой странице, ходить в API каждый раз незачем.
// Новый курс появляется в меню сам после публикации в /admin/courses (в течение TTL).

import { apiJson } from './api';

export type NavCourse = { href: string; title: string; subtitle: string; badge: string };

const TTL_MS = 60_000;
let cache: { at: number; data: NavCourse[] } | null = null;

const price = (n: number | null | undefined) =>
  n ? `${String(n).replace(/\B(?=(\d{3})+(?!\d))/g, ' ')} ₽` : '';

export async function getNavCourses(): Promise<NavCourse[]> {
  if (cache && Date.now() - cache.at < TTL_MS) return cache.data;
  try {
    const { status, data } = await apiJson<any[]>('/api/courses');
    if (status === 200 && Array.isArray(data)) {
      const list = data.map((c) => ({
        // Бесплатный курс ведёт на свой лендинг /course – как в витрине /courses.
        href: c.kind === 'free' ? '/course' : `/courses/${c.slug}`,
        title: String(c.title || ''),
        subtitle: String(c.subtitle || ''),
        badge: c.kind === 'free' ? 'Бесплатно' : price(c.price_rub),
      }));
      cache = { at: Date.now(), data: list };
      return list;
    }
  } catch {}
  return cache?.data ?? [];
}
