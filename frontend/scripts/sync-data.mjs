// Copy data/*.json from the repo root into frontend/public/data/ before next build.
import { promises as fs } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, '../..');
const srcDir = path.join(root, 'data');
const dstDir = path.join(here, '..', 'public', 'data');

await fs.mkdir(dstDir, { recursive: true });

for (const name of ['products.json', 'manufacturers.json']) {
  const src = path.join(srcDir, name);
  const dst = path.join(dstDir, name);
  try {
    await fs.copyFile(src, dst);
    console.log(`copied ${src} → ${dst}`);
  } catch (err) {
    if (err.code === 'ENOENT') {
      // Allow missing files during early development; the page will render with empty data.
      await fs.writeFile(dst, name === 'manufacturers.json' ? '{"entries":{}}' : '[]');
      console.warn(`stubbed missing ${src}`);
    } else {
      throw err;
    }
  }
}
