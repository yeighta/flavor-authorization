// Copy data/products.json from the repo root into frontend/public/data/ before
// next build, minified (the repo copy is indented for reviewable diffs).
import { promises as fs } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const src = path.resolve(here, '../../data/products.json');
const dstDir = path.join(here, '..', 'public', 'data');
const dst = path.join(dstDir, 'products.json');

await fs.mkdir(dstDir, { recursive: true });
try {
  const products = JSON.parse(await fs.readFile(src, 'utf8'));
  await fs.writeFile(dst, JSON.stringify(products));
  console.log(`wrote ${products.length} products → ${dst}`);
} catch (err) {
  if (err.code !== 'ENOENT') throw err;
  // Allow a missing DB during early development; the page renders empty.
  await fs.writeFile(dst, '[]');
  console.warn(`stubbed missing ${src}`);
}
