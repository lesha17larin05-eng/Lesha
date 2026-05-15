#!/usr/bin/env node
// Одноразовый батч: пройти по списку папок внутри web/public/img/, найти "сырые" исходники
// (без существующего {base}@2x.*) и пересобрать их в 4-варианта (jpg/png + webp, 1x + 2x)
// строго на месте, сохраняя исходное имя и расширение.
//
// Использование (внутри docker, см. ниже):
//   node scripts/batch-reprocess.mjs <subdir1> [<subdir2> ...]
// Примеры:
//   node scripts/batch-reprocess.mjs ""        # обработать только web/public/img/ (корень)
//   node scripts/batch-reprocess.mjs "" blog   # корень + blog
//
// Особенности:
//   * Исходник читается в Buffer ДО записи (избегаем самоперезаписи).
//   * Имя файла сохраняется как есть (без slugify), регистр сохраняется.
//   * Расширение исходника (.jpg/.jpeg/.png) сохраняется в native-варианте.
//   * Пропускаются: файлы < SKIP_SMALLER (по умолчанию 100KB), и любые, у которых уже есть
//     сосед "{base}@2x.{любое расширение}" (значит, уже обработано).

import { promises as fs } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import sharp from 'sharp';

const __filename = fileURLToPath(import.meta.url);
const REPO_ROOT = path.resolve(path.dirname(__filename), '..', '..');
const PUBLIC_IMG = path.resolve(REPO_ROOT, 'web', 'public', 'img');

const SKIP_SMALLER = 100 * 1024;
const MAX_RETINA_WIDTH = 2400;
const QUALITY = 82;

async function processFile(absPath) {
  const dir = path.dirname(absPath);
  const ext = path.extname(absPath).toLowerCase();
  const base = path.basename(absPath, path.extname(absPath));

  if (!['.jpg', '.jpeg', '.png'].includes(ext)) return { skipped: 'unsupported' };

  // Не пересобираем уже-готовые ретина-варианты — это привело бы к "foo@2x@2x.jpg".
  if (base.endsWith('@2x')) return { skipped: 'is-retina-variant' };

  const stat = await fs.stat(absPath);
  if (stat.size < SKIP_SMALLER) return { skipped: 'too-small', bytesIn: stat.size };

  // Уже обработан?
  const siblings = await fs.readdir(dir);
  const already = siblings.some((f) => {
    const fb = path.basename(f, path.extname(f));
    return fb === `${base}@2x` || fb === `${base}@2x`.toLowerCase() && f !== path.basename(absPath);
  });
  if (already) return { skipped: 'already-processed', bytesIn: stat.size };

  const buf = await fs.readFile(absPath);

  const meta = await sharp(buf, { failOn: 'none' }).rotate().metadata();
  const srcWidth = meta.width ?? 0;
  if (!srcWidth) return { skipped: 'no-width', bytesIn: stat.size };

  const width2x = Math.min(MAX_RETINA_WIDTH, srcWidth);
  const width1x = Math.round(width2x / 2);

  const nativeFormat = ext === '.png' ? 'png' : 'jpeg';
  // Сохраняем именно исходное расширение для native (jpg/jpeg/png), чтобы ссылки в коде не рвались
  const nativeExt = ext;

  const targets = [
    { w: width2x, fmt: nativeFormat, name: `${base}@2x${nativeExt}` },
    { w: width1x, fmt: nativeFormat, name: `${base}${nativeExt}` },
    { w: width2x, fmt: 'webp',       name: `${base}@2x.webp` },
    { w: width1x, fmt: 'webp',       name: `${base}.webp` },
  ];

  const written = [];
  for (const t of targets) {
    let pipeline = sharp(buf, { failOn: 'none' }).rotate().resize({ width: t.w, withoutEnlargement: true });
    if (t.fmt === 'jpeg') pipeline = pipeline.jpeg({ quality: QUALITY, mozjpeg: true });
    else if (t.fmt === 'png') pipeline = pipeline.png({ compressionLevel: 9 });
    else if (t.fmt === 'webp') pipeline = pipeline.webp({ quality: QUALITY });
    const outPath = path.join(dir, t.name);
    const info = await pipeline.toFile(outPath);
    written.push({ path: outPath, bytes: info.size, w: info.width, h: info.height });
  }

  // Если исходник не совпадает ни с одним из выходных путей — удалить.
  const outputPaths = new Set(written.map((w) => w.path));
  if (!outputPaths.has(absPath)) {
    await fs.unlink(absPath);
  }

  return {
    ok: true,
    bytesIn: stat.size,
    bytesOut: written.reduce((s, w) => s + w.bytes, 0),
    written,
  };
}

async function main() {
  const args = process.argv.slice(2);
  if (args.length === 0) {
    console.error('Usage: node scripts/batch-reprocess.mjs <subdir1> [<subdir2> ...]');
    process.exit(1);
  }

  let totalIn = 0;
  let totalOut = 0;
  let processed = 0;
  let skipped = 0;

  for (const sub of args) {
    const dir = path.join(PUBLIC_IMG, sub);
    const entries = await fs.readdir(dir);
    for (const name of entries.sort()) {
      const abs = path.join(dir, name);
      const st = await fs.stat(abs);
      if (!st.isFile()) continue;
      const res = await processFile(abs);
      const rel = path.relative(REPO_ROOT, abs);
      if (res.skipped) {
        console.log(`SKIP  ${rel}  (${res.skipped})`);
        skipped++;
        continue;
      }
      const inKB = (res.bytesIn / 1024).toFixed(0);
      const outKB = (res.bytesOut / 1024).toFixed(0);
      console.log(`OK    ${rel}  ${inKB}KB -> 4 файла, суммарно ${outKB}KB`);
      totalIn += res.bytesIn;
      totalOut += res.bytesOut;
      processed++;
    }
  }

  console.log('');
  console.log(`Обработано: ${processed},  Пропущено: ${skipped}`);
  console.log(`Исходники: ${(totalIn / 1024 / 1024).toFixed(1)} MB`);
  console.log(`Выход (×4 варианта): ${(totalOut / 1024 / 1024).toFixed(1)} MB`);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
