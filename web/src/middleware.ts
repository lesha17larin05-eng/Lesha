import { defineMiddleware } from 'astro/middleware';
import { apiJson } from './lib/api';
import { getSiteSettings } from './lib/settings';

export const onRequest = defineMiddleware(async (context, next) => {
  const cookie = context.request.headers.get('cookie') || '';
  const protectedPath = context.url.pathname.startsWith('/cabinet') || context.url.pathname.startsWith('/admin');
  let user: any = null;
  if (cookie) {
    const { status, data } = await apiJson('/api/me', { cookie });
    if (status === 200) user = data;
  }
  (context.locals as any).user = user;
  // Флаги сайта (кешируются на 10 секунд) – их читают шапка и лендинги.
  (context.locals as any).settings = await getSiteSettings();
  if (protectedPath && !user) {
    return context.redirect('/auth/login?next=' + encodeURIComponent(context.url.pathname));
  }
  if (context.url.pathname.startsWith('/admin') && user?.role !== 'admin') {
    return new Response('Not found', { status: 404 });
  }
  return next();
});
