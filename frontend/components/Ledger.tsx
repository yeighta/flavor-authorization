'use client';

import { useCallback, useDeferredValue, useEffect, useMemo, useRef, useState } from 'react';
import { buildCatalog, formatDate, formatGrams, gramsValue, yen, type Catalog, type Flavor, type Release } from '@/lib/data';
import { FAMILIES, type Family } from '@/lib/flavor';
import type { Product } from '@/lib/types';
import { FlavorSheet } from './FlavorSheet';
import { Mark } from './Mark';

type SortKey = 'brand' | 'name' | 'price' | 'updated';
type SortDir = 'asc' | 'desc';

/** Rows rendered up front; more are added as the reader scrolls. */
const PAGE = 200;

export const FAMILY_BG: Record<Family, string> = {
  mint: 'bg-family-mint',
  citrus: 'bg-family-citrus',
  berry: 'bg-family-berry',
  tropical: 'bg-family-tropical',
  orchard: 'bg-family-orchard',
  dessert: 'bg-family-dessert',
  botanical: 'bg-family-botanical',
  other: 'bg-family-other',
};

export function Ledger() {
  const [catalog, setCatalog] = useState<Catalog | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    fetch('/data/products.json')
      .then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return r.json() as Promise<Product[]>;
      })
      .then((ps) => !cancelled && setCatalog(buildCatalog(ps)))
      .catch((e) => !cancelled && setError(e instanceof Error ? e.message : String(e)));
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <>
      <a href="#q" className="sr-only focus:not-sr-only focus:fixed focus:left-4 focus:top-4 focus:z-50 focus:rounded-full focus:bg-ink focus:px-4 focus:py-1.5 focus:text-smoke">
        検索へ移動
      </a>
      <Masthead latest={catalog?.latest} />
      {error ? (
        <p role="alert" className="mx-auto max-w-[78rem] px-4 py-1.54 text-lg sm:px-8">
          データを読み込めませんでした（{error}）。ページを再読み込みしてください。
        </p>
      ) : catalog ? (
        <Browser catalog={catalog} />
      ) : (
        <p aria-busy className="mx-auto max-w-[78rem] px-4 py-1.54 text-haze sm:px-8">
          台帳を読み込んでいます…
        </p>
      )}
      <Colophon />
    </>
  );
}

function Masthead({ latest }: { latest?: string }) {
  return (
    <header className="mx-auto flex max-w-[78rem] items-center justify-between gap-4 px-4 pb-8 pt-6 sm:px-8 sm:pb-10">
      <a href="/" className="flex items-center gap-2.5 whitespace-nowrap text-[1.05rem] font-bold tracking-[0.02em]">
        <Mark className="h-8 w-8 shrink-0" />
        認可たばこデータベース
      </a>
      <p className="whitespace-nowrap text-right text-xs text-haze">
        {latest && (
          <>
            <span className="hidden sm:inline">最新の公表 {formatDate(latest)}</span>
            <span className="tabular-nums sm:hidden">{latest.replaceAll('-', '.')} 更新</span>
          </>
        )}
      </p>
    </header>
  );
}

function Browser({ catalog }: { catalog: Catalog }) {
  const [query, setQuery] = useState('');
  const deferredQuery = useDeferredValue(query);
  const [families, setFamilies] = useState<Set<Family>>(new Set());
  // Brand and notice date live in the URL (?brand=BALLI, ?date=2026-10-02) so a
  // filtered view can be shared, e.g. from the X announcement of an update.
  const [brand, setBrand] = useState(() => brandFromURL(catalog));
  const [grams, setGrams] = useState('');
  const [country, setCountry] = useState('');
  const [release, setRelease] = useState<string | null>(() => dateFromURL(catalog));

  useEffect(() => {
    const params = new URLSearchParams(location.search);
    for (const [k, v] of [['brand', brand], ['date', release ?? '']] as const) {
      if (v) params.set(k, v);
      else params.delete(k);
    }
    const qs = params.toString();
    history.replaceState(history.state, '', location.pathname + (qs ? `?${qs}` : '') + location.hash);
  }, [brand, release]);
  const [sort, setSort] = useState<{ key: SortKey; dir: SortDir }>({ key: 'brand', dir: 'asc' });
  const [openId, setOpenId] = useState<string | null>(null);
  const returnFocus = useRef<HTMLElement | null>(null);
  const bar = useRef<HTMLDivElement>(null);

  // The table header sticks just below the search bar, whose height varies with wrapping.
  useEffect(() => {
    const el = bar.current;
    if (!el) return;
    const ro = new ResizeObserver(() => document.documentElement.style.setProperty('--bar-h', `${el.offsetHeight}px`));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  // Deep link: #f=<flavor id> opens that flavor.
  useEffect(() => {
    const sync = () => {
      const m = location.hash.match(/^#f=([a-z0-9]+)$/);
      setOpenId(m ? m[1] : null);
    };
    sync();
    window.addEventListener('hashchange', sync);
    return () => window.removeEventListener('hashchange', sync);
  }, []);

  const open = useCallback((id: string, from?: HTMLElement) => {
    returnFocus.current = from ?? null;
    history.pushState(null, '', `#f=${id}`);
    setOpenId(id);
  }, []);

  const close = useCallback(() => {
    history.pushState(null, '', location.pathname + location.search);
    setOpenId(null);
    returnFocus.current?.focus();
  }, []);

  const releaseSet = useMemo(() => {
    if (!release) return null;
    const r = catalog.releases.find((x) => x.date === release);
    return r ? new Set([...r.added, ...r.revised].map((f) => f.id)) : null;
  }, [catalog, release]);

  const q = deferredQuery.trim().normalize('NFKC').toLowerCase();
  const terms = useMemo(() => q.split(/\s+/).filter(Boolean), [q]);

  const visible = useMemo(
    () =>
      catalog.flavors.filter(
        (f) =>
          terms.every((t) => f.search.includes(t)) &&
          (families.size === 0 || families.has(f.family)) &&
          (!grams || String(gramsValue(f.grams)) === grams) &&
          (!country || f.country === country) &&
          (!brand || f.brand === brand) &&
          (!releaseSet || releaseSet.has(f.id)),
      ),
    [catalog, terms, families, grams, country, brand, releaseSet],
  );

  const rows = useMemo(() => sortRows(visible, sort.key, sort.dir), [visible, sort]);

  const familyCounts = useMemo(() => {
    const c = new Map<Family, number>();
    for (const f of catalog.flavors) {
      if (terms.every((t) => f.search.includes(t))) c.set(f.family, (c.get(f.family) ?? 0) + 1);
    }
    return c;
  }, [catalog, terms]);

  const gramOptions = useMemo(() => countBy(catalog.flavors, (f) => String(gramsValue(f.grams))), [catalog]);
  const countryOptions = useMemo(() => countBy(catalog.flavors, (f) => f.country), [catalog]);
  const brandOptions = useMemo(
    () => catalog.brands.map((b) => [b.name, b.flavors.length] as [string, number]),
    [catalog],
  );

  const filtering = q !== '' || families.size > 0 || brand !== '' || grams !== '' || country !== '' || release !== null;
  const reset = () => {
    setQuery('');
    setFamilies(new Set());
    setGrams('');
    setCountry('');
    setRelease(null);
    setBrand('');
  };

  const controls = (
    <>
      <Select label="容量" value={grams} onChange={setGrams} options={gramOptions} format={(v) => `${v}g`} />
      <Select label="ブランド" value={brand} onChange={setBrand} options={brandOptions} />
      <Select label="製造国" value={country} onChange={setCountry} options={countryOptions} />
    </>
  );

  const openFlavor = openId ? catalog.flavors.find((f) => f.id === openId) : undefined;
  const openGroup = openFlavor
    ? catalog.flavors.filter((f) => f.brand === openFlavor.brand && f.name === openFlavor.name)
    : [];
  const siblings = useMemo(() => {
    if (!openFlavor) return [];
    const seen = new Set([openFlavor.name]);
    return (catalog.brands.find((b) => b.name === openFlavor.brand)?.flavors ?? []).filter((f) => {
      if (seen.has(f.name)) return false;
      seen.add(f.name);
      return true;
    });
  }, [catalog, openFlavor]);

  return (
    <>
      <Releases releases={catalog.releases.slice(0, 8)} active={release} onPick={(d) => setRelease((cur) => (cur === d ? null : d))} />

      <div ref={bar} className="sticky top-0 z-20 border-b border-veil bg-smoke/95 backdrop-blur-xl">
        <div className="mx-auto max-w-[78rem] px-4 sm:px-8">
          <div className="-mb-px flex items-center gap-3 border-b-2 border-transparent pt-2.5 transition-colors focus-within:border-ink">
            <label htmlFor="q" className="sr-only">
              フレーバー名やブランドで探す
            </label>
            <svg aria-hidden viewBox="0 0 20 20" className="h-5 w-5 shrink-0 text-haze">
              <circle cx="8.5" cy="8.5" r="5.5" fill="none" stroke="currentColor" strokeWidth="1.6" />
              <path d="M13 13l4.5 4.5" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
            </svg>
            <input
              id="q"
              type="search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="フレーバー名やブランドで探す（例: mint、Al Fakher）"
              autoComplete="off"
              spellCheck={false}
              className="min-w-0 flex-1 bg-transparent py-1.5 text-[1.0625rem] outline-none placeholder:text-haze/70 focus-visible:outline-none"
            />
            {query && (
              <button
                type="button"
                onClick={() => {
                  setQuery('');
                  document.getElementById('q')?.focus();
                }}
                className="shrink-0 rounded-full p-1.5 text-haze hover:text-ink"
                aria-label="検索語を消す"
              >
                <svg aria-hidden viewBox="0 0 20 20" className="h-4 w-4">
                  <path d="M5 5l10 10M15 5L5 15" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
                </svg>
              </button>
            )}
            <div className="hidden items-center gap-1.5 md:flex">{controls}</div>
            <p className="hidden shrink-0 pl-2 text-sm tabular-nums text-haze sm:block" aria-live="polite">
              {visible.length.toLocaleString('ja-JP')} 件
            </p>
          </div>

          <div className="-mx-4 flex items-center gap-x-1.5 overflow-x-auto px-4 pb-3 pt-2 [scrollbar-width:none] sm:mx-0 sm:flex-wrap sm:px-0">
            {FAMILIES.map(({ id, label }) => {
              const on = families.has(id);
              return (
                <button
                  key={id}
                  type="button"
                  aria-pressed={on}
                  onClick={() =>
                    setFamilies((cur) => {
                      const next = new Set(cur);
                      if (next.has(id)) next.delete(id);
                      else next.add(id);
                      return next;
                    })
                  }
                  className={`flex shrink-0 items-center gap-1.5 rounded-full border px-3 py-1 text-[0.8125rem] transition-colors ${
                    on ? 'border-ink bg-ink text-smoke' : 'border-veil hover:border-haze'
                  }`}
                >
                  <span aria-hidden className={`h-2 w-2 rounded-full ${FAMILY_BG[id]}`} />
                  {label}
                  <span className={`tabular-nums ${on ? 'text-smoke/70' : 'text-haze'}`}>{familyCounts.get(id) ?? 0}</span>
                </button>
              );
            })}
            <span aria-hidden className="mx-1 h-5 w-px shrink-0 bg-veil md:hidden" />
            <div className="flex items-center gap-1.5 md:hidden">{controls}</div>
            {filtering && (
              <button type="button" onClick={reset} className="shrink-0 px-2 py-1 text-[0.8125rem] text-haze underline underline-offset-4 hover:text-ink">
                条件をすべて外す
              </button>
            )}
          </div>
        </div>
      </div>

      <main id="index" className="mx-auto max-w-[78rem] px-4 pb-24 sm:px-8">
        {release && (
          <p className="mt-6 text-sm text-haze">{formatDate(release)}の公表に含まれるフレーバーだけを表示しています。</p>
        )}
        {rows.length === 0 ? (
          <div className="py-1.54">
            <p className="text-2xl font-bold">
              {q ? <>「{deferredQuery.trim()}」に一致するフレーバーはありません。</> : '条件に合うフレーバーはありません。'}
            </p>
            <p className="mt-3 text-haze">綴りを変えるか、絞り込みを外してみてください。</p>
            <button type="button" onClick={reset} className="mt-6 rounded-full bg-ink px-5 py-1.5 text-sm text-smoke">
              条件をすべて外す
            </button>
          </div>
        ) : (
          <FlavorTable rows={rows} sort={sort} onSort={setSort} onOpen={open} terms={terms} groupByBrand={sort.key === 'brand'} />
        )}
      </main>

      {openFlavor && (
        <FlavorSheet
          sizes={openGroup}
          siblings={siblings}
          onClose={close}
          onBrand={(b) => {
            setBrand(b);
            close();
            document.getElementById('index')?.scrollIntoView();
          }}
        />
      )}
    </>
  );
}

function brandFromURL(catalog: Catalog): string {
  const want = new URLSearchParams(location.search).get('brand')?.trim().toLowerCase();
  if (!want) return '';
  // Tolerate case differences in hand-typed links (?brand=balli).
  return catalog.brands.find((b) => b.name.toLowerCase() === want)?.name ?? '';
}

function dateFromURL(catalog: Catalog): string | null {
  const want = new URLSearchParams(location.search).get('date');
  return want && catalog.releases.some((r) => r.date === want) ? want : null;
}

function sortRows(rows: Flavor[], key: SortKey, dir: SortDir): Flavor[] {
  const byBrand = (a: Flavor, b: Flavor) => a.brand.localeCompare(b.brand, 'en', { sensitivity: 'base' });
  const byName = (a: Flavor, b: Flavor) => a.name.localeCompare(b.name, 'en', { sensitivity: 'base' });
  const bySize = (a: Flavor, b: Flavor) => gramsValue(a.grams) - gramsValue(b.grams);
  const cmp: Record<SortKey, (a: Flavor, b: Flavor) => number> = {
    brand: (a, b) => byBrand(a, b) || byName(a, b) || bySize(a, b),
    name: (a, b) => byName(a, b) || byBrand(a, b) || bySize(a, b),
    price: (a, b) => a.priceYen - b.priceYen || byBrand(a, b) || byName(a, b),
    updated: (a, b) => a.updated.localeCompare(b.updated) || byBrand(a, b) || byName(a, b),
  };
  const sign = dir === 'asc' ? 1 : -1;
  return [...rows].sort((a, b) => sign * cmp[key](a, b));
}

const COLUMNS: { key: SortKey | null; label: string; className: string; firstDir: SortDir }[] = [
  { key: 'brand', label: 'ブランド', className: 'hidden md:table-cell w-[13rem]', firstDir: 'asc' },
  { key: 'name', label: 'フレーバー', className: '', firstDir: 'asc' },
  { key: null, label: '容量', className: 'hidden sm:table-cell w-[6rem]', firstDir: 'asc' },
  { key: 'price', label: '価格', className: 'w-[7.5rem] text-right', firstDir: 'asc' },
  { key: null, label: '前回から', className: 'hidden lg:table-cell w-[7rem] text-right', firstDir: 'asc' },
  { key: null, label: '製造国', className: 'hidden lg:table-cell w-[9rem]', firstDir: 'asc' },
  { key: 'updated', label: '更新日', className: 'hidden md:table-cell w-[8.5rem] text-right', firstDir: 'desc' },
];

function FlavorTable({
  rows,
  sort,
  onSort,
  onOpen,
  terms,
  groupByBrand,
}: {
  rows: Flavor[];
  sort: { key: SortKey; dir: SortDir };
  onSort: (s: { key: SortKey; dir: SortDir }) => void;
  onOpen: (id: string, from?: HTMLElement) => void;
  terms: string[];
  groupByBrand: boolean;
}) {
  const [limit, setLimit] = useState(PAGE);
  const sentinel = useRef<HTMLDivElement>(null);

  // A new result set starts from the top page again.
  useEffect(() => setLimit(PAGE), [rows]);

  useEffect(() => {
    const el = sentinel.current;
    if (!el) return;
    const io = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting)) setLimit((n) => n + PAGE * 2);
    }, { rootMargin: '1200px 0px' });
    io.observe(el);
    return () => io.disconnect();
  }, [rows]);

  const shown = rows.slice(0, limit);
  // Rows per brand run, so the brand cell can span (and stick across) its rows.
  const span = new Map<number, number>();
  if (groupByBrand) {
    for (let i = 0; i < shown.length; ) {
      let j = i;
      while (j < shown.length && shown[j].brand === shown[i].brand) j++;
      span.set(i, j - i);
      i = j;
    }
  }

  return (
    <>
      <table className="mt-2 w-full border-collapse text-[0.875rem]">
        <caption className="sr-only">フレーバーの一覧。行を選ぶと価格の推移を表示します。</caption>
        <thead className="sticky top-[var(--bar-h,6.5rem)] z-10">
          <tr className="text-left">
            {COLUMNS.map((c) => {
              const active = c.key != null && sort.key === c.key;
              return (
                <th
                  key={c.label}
                  scope="col"
                  aria-sort={active ? (sort.dir === 'asc' ? 'ascending' : 'descending') : undefined}
                  className={`bg-smoke px-2 py-2.5 text-[0.75rem] font-medium text-haze shadow-[inset_0_-1px_0_rgb(var(--ink))] first:pl-0 last:pr-0 ${c.className}`}
                >
                  {c.key ? (
                    <button
                      type="button"
                      onClick={() =>
                        onSort({ key: c.key!, dir: active ? (sort.dir === 'asc' ? 'desc' : 'asc') : c.firstDir })
                      }
                      className={`inline-flex items-center gap-1 hover:text-ink ${active ? 'text-ink' : ''}`}
                    >
                      {c.label}
                      <span aria-hidden className={`text-[0.7rem] ${active ? '' : 'opacity-0'}`}>
                        {active && sort.dir === 'desc' ? '▼' : '▲'}
                      </span>
                    </button>
                  ) : (
                    c.label
                  )}
                </th>
              );
            })}
          </tr>
        </thead>
        <tbody>
          {shown.map((f, i) => {
            const firstOfBrand = groupByBrand && (i === 0 || shown[i - 1].brand !== f.brand);
            const rose = (f.delta ?? 0) > 0;
            const fell = (f.delta ?? 0) < 0;
            return (
              <tr
                key={f.id}
                onClick={(e) => {
                  if ((e.target as HTMLElement).closest('a,button')) return;
                  onOpen(f.id, e.currentTarget.querySelector('button') ?? undefined);
                }}
                className={`cursor-pointer align-baseline transition-colors hover:bg-glass ${
                  groupByBrand && firstOfBrand && i > 0 ? 'border-t border-veil' : ''
                } ${!groupByBrand ? 'border-t border-veil/70' : ''}`}
              >
                {groupByBrand ? (
                  firstOfBrand && (
                    <td rowSpan={span.get(i)} className="hidden bg-smoke py-1.5 pr-2 align-top md:table-cell">
                      <span className="sticky top-[calc(var(--bar-h,6.5rem)+2.5rem)] block py-0.5 font-display text-[1.35rem] font-extrabold uppercase leading-none tracking-[0.01em]">
                        <Highlight text={f.brand} terms={terms} />
                      </span>
                    </td>
                  )
                ) : (
                  <td className="hidden py-1.5 pr-2 align-baseline md:table-cell">
                    <span className="font-display text-[1.05rem] font-bold uppercase tracking-[0.01em] text-haze">
                      <Highlight text={f.brand} terms={terms} />
                    </span>
                  </td>
                )}
                <td className="py-1.5 pl-0 pr-2 md:pl-2">
                  <span className="block font-display text-[0.8125rem] font-bold uppercase tracking-[0.02em] text-haze md:hidden">
                    <Highlight text={f.brand} terms={terms} />
                    <span className="ml-2 font-sans font-normal normal-case tracking-normal sm:hidden">{formatGrams(f.grams)}</span>
                  </span>
                  <button
                    type="button"
                    onClick={(e) => onOpen(f.id, e.currentTarget)}
                    className="text-left font-display text-[1.2rem] font-medium leading-tight hover:underline hover:decoration-2 hover:underline-offset-4"
                  >
                    <span aria-hidden className={`mr-1.5 inline-block h-[0.42em] w-[0.42em] -translate-y-[0.12em] rounded-full ${FAMILY_BG[f.family]}`} />
                    <Highlight text={f.name} terms={terms} />
                  </button>
                </td>
                <td className="hidden px-2 py-1.5 tabular-nums text-haze sm:table-cell">
                  {formatGrams(f.grams)}
                  {f.variant && <span className="ml-1 text-[0.75rem]">{f.variant}</span>}
                </td>
                <td className="px-2 py-1.5 text-right font-display text-[1.2rem] font-bold tabular-nums">
                  {yen(f.priceYen)}
                  {(rose || fell) && (
                    <span className={`ml-1 font-sans text-[0.75rem] lg:hidden ${rose ? 'text-ember' : 'text-cool'}`}>
                      {rose ? '↑' : '↓'}
                      <span className="sr-only">{rose ? '値上げ' : '値下げ'}</span>
                    </span>
                  )}
                </td>
                <td className={`hidden px-2 py-1.5 text-right font-display text-[1rem] font-medium tabular-nums lg:table-cell ${rose ? 'text-ember' : fell ? 'text-cool' : 'text-haze/60'}`}>
                  {rose || fell ? (
                    <>
                      {rose ? '+' : '−'}
                      {yen(Math.abs(f.delta!))}
                    </>
                  ) : (
                    <span aria-label="改定なし">—</span>
                  )}
                </td>
                <td className="hidden px-2 py-1.5 text-haze lg:table-cell">{f.country}</td>
                <td className="hidden py-1.5 pl-2 pr-0 text-right font-display text-[0.95rem] tabular-nums text-haze md:table-cell">{f.updated.replaceAll('-', '.')}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
      {limit < rows.length && (
        <div ref={sentinel} className="py-10 text-center text-sm text-haze">
          続きを読み込んでいます…（{rows.length.toLocaleString('ja-JP')} 件中 {limit.toLocaleString('ja-JP')} 件）
        </div>
      )}
    </>
  );
}

/** While searching, the matched letters stay in ink and the rest recede. */
function Highlight({ text, terms }: { text: string; terms: string[] }) {
  if (terms.length === 0) return <>{text}</>;
  const lower = text.normalize('NFKC').toLowerCase();
  if (lower.length !== text.length) return <>{text}</>;
  const hit = new Array<boolean>(text.length).fill(false);
  for (const t of terms) {
    for (let i = lower.indexOf(t); i !== -1; i = lower.indexOf(t, i + 1)) hit.fill(true, i, i + t.length);
  }
  if (!hit.includes(true)) return <>{text}</>;
  const parts: { s: string; on: boolean }[] = [];
  for (let i = 0; i < text.length; i++) {
    const last = parts[parts.length - 1];
    if (last && last.on === hit[i]) last.s += text[i];
    else parts.push({ s: text[i], on: hit[i] });
  }
  return (
    <>
      {parts.map((p, i) => (
        <span key={i} className={p.on ? 'bg-ink/10' : ''}>
          {p.s}
        </span>
      ))}
    </>
  );
}

function Releases({
  releases,
  active,
  onPick,
}: {
  releases: Release[];
  active: string | null;
  onPick: (date: string) => void;
}) {
  return (
    <section aria-labelledby="releases" className="mx-auto max-w-[78rem] px-4 pb-8 sm:px-8">
      <h2 id="releases" className="text-sm font-bold">
        最近の公表
      </h2>
      <ol className="-mx-4 mt-3 flex gap-px overflow-x-auto px-4 pb-1 [mask-image:linear-gradient(to_right,black_calc(100%-4rem),transparent)] [scrollbar-width:none] sm:mx-0 sm:px-0">
        {releases.map((r) => {
          const on = active === r.date;
          const brands = topBrands([...r.added, ...r.revised]);
          const rises = r.revised.filter((f) => (f.delta ?? 0) > 0).length;
          const falls = r.revised.filter((f) => (f.delta ?? 0) < 0).length;
          return (
            <li key={r.date} className="w-[11.5rem] shrink-0">
              <button
                type="button"
                aria-pressed={on}
                onClick={() => onPick(r.date)}
                className={`flex h-full w-full flex-col border-t-2 px-1 pb-2 pt-3 text-left transition-colors ${
                  on ? 'border-ink' : 'border-veil hover:border-haze'
                }`}
              >
                <time dateTime={r.date} className="text-[0.8125rem] tabular-nums text-haze">
                  {formatDate(r.date)}
                </time>
                <span className="mt-1.5 font-display text-[1.35rem] font-bold leading-tight">
                  {r.added.length > 0 && <>新規 {r.added.length}</>}
                  {r.added.length > 0 && r.revised.length > 0 && <span className="text-haze"> / </span>}
                  {rises > 0 && <span className="text-ember">値上げ {rises}</span>}
                  {rises > 0 && falls > 0 && <span className="text-haze"> / </span>}
                  {falls > 0 && <span className="text-cool">値下げ {falls}</span>}
                </span>
                <span className="mt-1 line-clamp-2 text-[0.8125rem] leading-snug text-haze">{brands}</span>
              </button>
            </li>
          );
        })}
      </ol>
    </section>
  );
}

function Select({
  label,
  value,
  onChange,
  options,
  format = (v) => v,
  hideAll,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  options: [string, number][];
  format?: (v: string) => string;
  hideAll?: boolean;
}) {
  return (
    <label className={`relative flex shrink-0 items-center rounded-full border px-3 py-1 text-[0.8125rem] ${value && !hideAll ? 'border-ink' : 'border-veil hover:border-haze'}`}>
      <span className="text-haze">{label}</span>
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="max-w-[8.5rem] appearance-none truncate bg-transparent pl-1.5 pr-4 text-ink outline-none focus-visible:outline-none"
      >
        {!hideAll && <option value="">すべて</option>}
        {options.map(([v, n]) => (
          <option key={v} value={v}>
            {format(v)}
            {n ? `（${n}）` : ''}
          </option>
        ))}
      </select>
      <svg aria-hidden viewBox="0 0 10 6" className="pointer-events-none absolute right-3 h-1.5 w-2.5 text-haze">
        <path d="M1 1l4 4 4-4" fill="none" stroke="currentColor" strokeWidth="1.4" />
      </svg>
    </label>
  );
}

function Colophon() {
  return (
    <footer className="mx-auto max-w-[78rem] px-4 pb-16 pt-10 text-[0.8125rem] leading-[1.9] text-haze sm:px-8">
      <div className="grid gap-6 sm:grid-cols-2">
        <p className="max-w-[34rem]">
          出典は財務省「
          <a href="https://www.mof.go.jp/policy/tab_salt/topics/kouriteika.html" className="underline underline-offset-4 hover:text-ink">
            製造たばこの小売定価の認可
          </a>
          」の公表PDF（2018年4月以降）です。PDFの読み取りとブランドの分類は自動で行っているため、誤りを含むことがあります。正確な情報は各フレーバーの出典PDFで確認してください。
        </p>
        <p className="max-w-[34rem]">本サイトは個人が運営しており、財務省やたばこメーカーとは関係ありません。</p>
      </div>
    </footer>
  );
}

function countBy(flavors: Flavor[], pick: (f: Flavor) => string): [string, number][] {
  const m = new Map<string, number>();
  for (const f of flavors) {
    const v = pick(f);
    if (v && v !== '0') m.set(v, (m.get(v) ?? 0) + 1);
  }
  return [...m.entries()].sort((a, b) => b[1] - a[1]);
}

function topBrands(flavors: Flavor[]): string {
  const m = new Map<string, number>();
  for (const f of flavors) m.set(f.brand, (m.get(f.brand) ?? 0) + 1);
  const names = [...m.entries()].sort((a, b) => b[1] - a[1]).map(([b]) => b);
  return names.length > 3 ? `${names.slice(0, 3).join('、')} ほか${names.length - 3}ブランド` : names.join('、');
}

