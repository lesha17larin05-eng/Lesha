import type { APIRoute } from 'astro';
import { getSiteSettings } from '../lib/settings';

// Динамический sitemap: страница Салюта попадает в него только когда
// включён флаг salut_visible (тумблер в админке). Раньше файл лежал
// статикой в public/sitemap.xml.

const SITE = 'https://leshalarin.ru';

type Entry = { path: string; changefreq: string; priority: string };

const BASE: Entry[] = [
  { path: '/', changefreq: 'weekly', priority: '1.0' },
  { path: '/coaching', changefreq: 'weekly', priority: '0.9' },
  { path: '/start', changefreq: 'weekly', priority: '0.9' },
  { path: '/course', changefreq: 'weekly', priority: '0.9' },
  { path: '/courses/myagkiy-start', changefreq: 'weekly', priority: '0.8' },
  { path: '/courses/zdorovaya-spina', changefreq: 'weekly', priority: '0.8' },
  { path: '/results', changefreq: 'monthly', priority: '0.7' },
  { path: '/blog', changefreq: 'weekly', priority: '0.7' },
  { path: '/legal/privacy', changefreq: 'yearly', priority: '0.3' },
  { path: '/legal/offer', changefreq: 'yearly', priority: '0.3' },
  { path: '/legal/terms', changefreq: 'yearly', priority: '0.3' },
  { path: '/legal/consent', changefreq: 'yearly', priority: '0.3' },
];

const SALUT: Entry = { path: '/salut-2026', changefreq: 'weekly', priority: '0.9' };

export const GET: APIRoute = async () => {
  const { salut_visible } = await getSiteSettings();
  const entries = salut_visible
    ? [...BASE.slice(0, 6), SALUT, ...BASE.slice(6)]
    : BASE;
  const body =
    '<?xml version="1.0" encoding="UTF-8"?>\n' +
    '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n' +
    entries
      .map(
        (e) =>
          `  <url><loc>${SITE}${e.path}</loc><changefreq>${e.changefreq}</changefreq><priority>${e.priority}</priority></url>`,
      )
      .join('\n') +
    '\n</urlset>\n';
  return new Response(body, {
    headers: {
      'Content-Type': 'application/xml; charset=utf-8',
      'Cache-Control': 'public, max-age=600',
    },
  });
};
