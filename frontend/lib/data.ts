import { familyOf, type Family } from './flavor';
import type { PricePoint, Product } from './types';

export interface Flavor {
  id: string;
  brand: string;
  name: string;
  variant: string;
  grams: string;
  priceYen: number;
  country: string;
  family: Family;
  history: PricePoint[];
  /** First authorization date (YYYY-MM-DD). */
  since: string;
  /** Date of the latest price (authorization or revision). */
  updated: string;
  /** Price change from the previous authorized price, if any. */
  delta: number | null;
  search: string;
}

export interface Brand {
  name: string;
  flavors: Flavor[];
  countries: string[];
}

/** One 財務省 notice date and what it changed. */
export interface Release {
  date: string;
  added: Flavor[];
  revised: Flavor[];
}

export interface Catalog {
  flavors: Flavor[];
  brands: Brand[];
  releases: Release[];
  latest: string;
}

// Stable, URL-safe id so a flavor can be linked to (#f=…).
function makeId(p: Product): string {
  const s = `${p.manufacturer}|${p.name}|${p.grams}`.normalize('NFKC').toLowerCase();
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  return (h >>> 0).toString(36);
}

export function buildCatalog(products: Product[]): Catalog {
  const flavors: Flavor[] = products.map((p) => {
    const history =
      p.history && p.history.length > 0
        ? p.history
        : [{ date: p.updatedDate, priceYen: p.priceYen, source: p.source, sourceUrl: p.sourceUrl }];
    const prev = history.length > 1 ? history[history.length - 2].priceYen : null;
    return {
      id: makeId(p),
      brand: p.manufacturer,
      name: p.name,
      variant: p.variant ?? '',
      grams: p.grams,
      priceYen: p.priceYen,
      country: p.country ?? '',
      family: familyOf(p.name),
      history,
      since: history[0].date,
      updated: history[history.length - 1].date,
      delta: prev == null ? null : p.priceYen - prev,
      search: `${p.manufacturer} ${p.name}`.normalize('NFKC').toLowerCase(),
    };
  });

  const byBrand = new Map<string, Flavor[]>();
  for (const f of flavors) {
    const list = byBrand.get(f.brand) ?? [];
    list.push(f);
    byBrand.set(f.brand, list);
  }
  const brands: Brand[] = [...byBrand.entries()]
    .map(([name, list]) => ({
      name,
      flavors: list.sort((a, b) => a.name.localeCompare(b.name) || gramsValue(a.grams) - gramsValue(b.grams)),
      countries: [...new Set(list.map((f) => f.country).filter(Boolean))],
    }))
    .sort((a, b) => a.name.localeCompare(b.name, 'en', { sensitivity: 'base' }));

  const releaseMap = new Map<string, Release>();
  const release = (date: string) => {
    let r = releaseMap.get(date);
    if (!r) {
      r = { date, added: [], revised: [] };
      releaseMap.set(date, r);
    }
    return r;
  };
  for (const f of flavors) {
    f.history.forEach((h, i) => (i === 0 ? release(h.date).added : release(h.date).revised).push(f));
  }
  const releases = [...releaseMap.values()].sort((a, b) => b.date.localeCompare(a.date));

  return { flavors, brands, releases, latest: releases[0]?.date ?? '' };
}

export function gramsValue(g: string): number {
  const n = parseFloat(g);
  return Number.isFinite(n) ? n : 0;
}

export function formatGrams(g: string): string {
  const n = gramsValue(g);
  return n ? `${n}g` : g;
}

export function yen(n: number): string {
  return `¥${n.toLocaleString('ja-JP')}`;
}

export function formatDate(d: string): string {
  const [y, m, day] = d.split('-').map(Number);
  return `${y}年${m}月${day}日`;
}
