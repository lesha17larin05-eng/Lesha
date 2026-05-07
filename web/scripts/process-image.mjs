#!/usr/bin/env node
// Обработка изображений: исходник -> {name.ext, name@2x.ext, name.webp, name@2x.webp}
//
// Использование:
//   npm run img -- <input> [subdir] [--name=slug] [--width=N] [--quality=N] [--keep]
//
// <input>      путь к файлу-источнику (относительно корня репо или абсолютный).
//              Обычно — только что положенный в корень репо файл (foo.jpg, hero.png и т.п.).
// [subdir]     подпапка внутри web/public/img/ (например, "hero", "results"). По умолчанию "".
// --name=slug  переопределить базовое имя (kebab-case, без расширения).
// --width=N    максимальная ширина для @2x варианта (px). По умолчанию: исходная ширина, но не более 2400.
// --quality=N  качество JPEG/WebP. По умолчанию 82.
// --keep       не удалять исходник после обработки.
//
// Поддерживаемые форматы входа: .jpg .jpeg .png .webp
// Выход всегда: original-format + webp, в 1x и 2x.

import { promises as fs } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import sharp from 'sharp';

const __filename = fileURLToPath(import.meta.url);
const REPO_ROOT = path.resolve(path.dirname(__filename), '..', '..');
const PUBLIC_IMG = path.resolve(REPO_ROOT, 'web', 'public', 'img');

function parseArgs(argv) {
  const positional = [];
  const flags = {};
  for (const a of argv.slice(2)) {
    if (a.startsWith('--')) {
      const [k, v] = a.slice(2).split('=');
      flags[k] = v ?? true;
    } else {
      positional.push(a);
    }
  }
  return { positional, flags };
}

function slugify(s) {
  return s
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '') || 'image';
}

async function main() {
  const { positional, flags } = parseArgs(process.argv);
  if (positional.length < 1) {
    console.error('Usage: npm run img -- <input> [subdir] [--name=slug] [--width=N] [--quality=N] [--keep]');
    process.exit(1);
  }

  const inputArg = positional[0];
  const subdir = positional[1] ?? '';
  const inputAbs = path.isAbsolute(inputArg) ? inputArg : path.resolve(REPO_ROOT, inputArg);

  const stat = await fs.stat(inputAbs).catch(() => null);
  if (!stat || !stat.isFile()) {
    console.error(`Файл не найден: ${inputAbs}`);
    process.exit(1);
  }

  const ext = path.extname(inputAbs).toLowerCase();
  if (!['.jpg', '.jpeg', '.png', '.webp'].includes(ext)) {
    console.error(`Не поддерживаемое расширение: ${ext}. Разрешены: .jpg .jpeg .png .webp`);
    process.exit(1);
  }

  const baseRaw = flags.name ?? path.basename(inputAbs, ext);
  const base = slugify(baseRaw);
  const quality = Number(flags.quality ?? 82);

  const outDir = path.join(PUBLIC_IMG, subdir);
  await fs.mkdir(outDir, { recursive: true });

  const img = sharp(inputAbs, { failOn: 'none' }).rotate(); // авто-ориентация по EXIF
  const meta = await img.metadata();
  const srcWidth = meta.width ?? 0;
  if (!srcWidth) {
    console.error('Не удалось прочитать размеры изображения.');
    process.exit(1);
  }

  const maxRetinaWidth = Math.min(Number(flags.width ?? 2400), srcWidth);
  const width2x = maxRetinaWidth;
  const width1x = Math.round(width2x / 2);

  // Выходной "родной" формат (jpeg/png; webp-источник сохраняем в jpeg для совместимости).
  const nativeFormat = ext === '.png' ? 'png' : 'jpeg';
  const nativeExt = nativeFormat === 'png' ? '.png' : '.jpg';

  const targets = [
    { w: width2x, suffix: '@2x', fmt: nativeFormat, file: `${base}@2x${nativeExt}` },
    { w: width1x, suffix: '',    fmt: nativeFormat, file: `${base}${nativeExt}` },
    { w: width2x, suffix: '@2x', fmt: 'webp',       file: `${base}@2x.webp` },
    { w: width1x, suffix: '',    fmt: 'webp',       file: `${base}.webp` },
  ];

  const results = [];
  for (const t of targets) {
    const outPath = path.join(outDir, t.file);
    let pipeline = sharp(inputAbs, { failOn: 'none' }).rotate().resize({ width: t.w, withoutEnlargement: true });
    if (t.fmt === 'jpeg') pipeline = pipeline.jpeg({ quality, mozjpeg: true });
    else if (t.fmt === 'png') pipeline = pipeline.png({ compressionLevel: 9 });
    else if (t.fmt === 'webp') pipeline = pipeline.webp({ quality });
    const info = await pipeline.toFile(outPath);
    results.push({ file: path.relative(REPO_ROOT, outPath), w: info.width, h: info.height, bytes: info.size });
  }

  if (!flags.keep) {
    await fs.unlink(inputAbs);
  }

  const publicPath = subdir ? `/img/${subdir}/${base}` : `/img/${base}`;
  console.log('\nГотово. Сгенерированы файлы:');
  for (const r of results) console.log(`  ${r.file}  (${r.w}x${r.h}, ${(r.bytes / 1024).toFixed(1)} KB)`);
  console.log('\nИспользование в Astro:');
  console.log(`  <ResponsiveImage base="${publicPath}" ext="${nativeExt.slice(1)}" width="${width1x}" height="${Math.round((meta.height ?? 0) * (width1x / srcWidth))}" alt="..." />`);
  console.log('\nИли вручную:');
  console.log(`  <picture>`);
  console.log(`    <source type="image/webp" srcset="${publicPath}.webp 1x, ${publicPath}@2x.webp 2x">`);
  console.log(`    <img src="${publicPath}${nativeExt}" srcset="${publicPath}${nativeExt} 1x, ${publicPath}@2x${nativeExt} 2x" alt="...">`);
  console.log(`  </picture>`);
}

main().catch(err => {
  console.error(err);
  process.exit(1);
});
