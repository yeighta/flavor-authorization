'use client';

import { useEffect, useMemo, useState } from 'react';
import type {
  Classification,
  ManufacturerMap,
  PipeProduct,
  PipeType,
  Product,
} from '@/lib/types';

const FLAT_VIEW_LIMIT = 300;

interface DataState {
  products: PipeProduct[];
  loading: boolean;
  error: string | null;
  lastUpdate: string;
}

const initial: DataState = {
  products: [],
  loading: true,
  error: null,
  lastUpdate: '—',
};

export function ProductBrowser() {
  const [state, setState] = useState<DataState>(initial);
  const [query, setQuery] = useState('');
  const [country, setCountry] = useState('');
  const [grams, setGrams] = useState('');
  const [selectedManufacturer, setSelectedManufacturer] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const [productsRes, manufacturersRes] = await Promise.all([
          fetch('/data/products.json'),
          fetch('/data/manufacturers.json'),
        ]);
        if (!productsRes.ok) throw new Error(`products: HTTP ${productsRes.status}`);
        const products: Product[] = await productsRes.json();
        const manufacturers: ManufacturerMap = manufacturersRes.ok
          ? await manufacturersRes.json()
          : { entries: {} };
        const enriched = products
          .filter((p) => p.category === 'パイプたばこ')
          .map<PipeProduct>((p) => {
            const cls: Classification | undefined = manufacturers.entries[p.manufacturer];
            return {
              ...p,
              pipeType: cls?.type ?? 'unknown',
              confidence: cls?.confidence,
            };
          });
        const dates = enriched.map((p) => p.updatedDate).sort();
        if (!cancelled) {
          setState({
            products: enriched,
            loading: false,
            error: null,
            lastUpdate: dates[dates.length - 1] ?? '—',
          });
        }
      } catch (err) {
        if (!cancelled) {
          setState({
            products: [],
            loading: false,
            error: err instanceof Error ? err.message : String(err),
            lastUpdate: '—',
          });
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  // products.json is already filtered to shisha-only at merge time, so no
  // additional pipe-type filtering is needed here.
  const byTab = state.products;

  const matchQuery = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (q === '') return () => true;
    return (p: PipeProduct) =>
      `${p.manufacturer} ${p.name} ${p.variant ?? ''}`.toLowerCase().includes(q);
  }, [query]);

  // Each dropdown's options are derived from the set with all OTHER filters applied,
  // so picking grams=50g doesn't kill the grams options list, but it does reflect
  // the current country / search context. The currently-selected value is always
  // pinned to the list — even at count 0 — so the dropdown can render its value
  // (otherwise the native <select> falls back to displaying the placeholder while
  // the filter state silently keeps applying).
  const countryOptions = useMemo(() => {
    const subset = byTab.filter(
      (p) =>
        matchQuery(p) &&
        (!grams || p.grams === grams) &&
        (selectedManufacturer == null || p.manufacturer === selectedManufacturer),
    );
    return ensureSelected(topValues(subset, (p) => p.country ?? ''), country);
  }, [byTab, matchQuery, grams, country, selectedManufacturer]);

  const gramsOptions = useMemo(() => {
    const subset = byTab.filter(
      (p) =>
        matchQuery(p) &&
        (!country || p.country === country) &&
        (selectedManufacturer == null || p.manufacturer === selectedManufacturer),
    );
    return ensureSelected(topValues(subset, (p) => p.grams), grams);
  }, [byTab, matchQuery, country, grams, selectedManufacturer]);

  // The "fully filtered" set used everywhere downstream.
  const filtered = useMemo(
    () =>
      byTab.filter(
        (p) =>
          matchQuery(p) &&
          (!country || p.country === country) &&
          (!grams || p.grams === grams),
      ),
    [byTab, matchQuery, country, grams],
  );

  const manufacturerList = useMemo(() => {
    const counts = new Map<string, number>();
    for (const p of filtered) {
      counts.set(p.manufacturer, (counts.get(p.manufacturer) ?? 0) + 1);
    }
    return [...counts.entries()].sort((a, b) => a[0].localeCompare(b[0]));
  }, [filtered]);

  const productsForSelected = useMemo(() => {
    if (selectedManufacturer == null) return [];
    return filtered
      .filter((p) => p.manufacturer === selectedManufacturer)
      .sort((a, b) => a.name.localeCompare(b.name));
  }, [filtered, selectedManufacturer]);

  const filtersActive = query.trim() !== '' || country !== '' || grams !== '';
  const showFlatResults = filtersActive && selectedManufacturer == null;

  if (state.loading) return <Loading />;
  if (state.error) return <ErrorView message={state.error} />;

  const onResetSelection = () => setSelectedManufacturer(null);

  return (
    <>
      <p className="text-xs text-stone-500">
        総レコード数: {state.products.length.toLocaleString()} 件 ／ 最新収録日: {state.lastUpdate}
      </p>

      <FilterBar
        query={query}
        setQuery={setQuery}
        country={country}
        setCountry={setCountry}
        grams={grams}
        setGrams={setGrams}
        countryOptions={countryOptions}
        gramsOptions={gramsOptions}
        onClear={() => {
          setQuery('');
          setCountry('');
          setGrams('');
          onResetSelection();
        }}
        filtersActive={filtersActive}
      />

      <div className="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-[280px_1fr]">
        <aside className="space-y-2">
          <div className="text-xs text-stone-500">
            {manufacturerList.length} メーカー / {filtered.length.toLocaleString()} 件
          </div>
          <ul className="max-h-[60vh] overflow-y-auto rounded-md border border-stone-200 bg-white dark:border-stone-800 dark:bg-stone-900">
            {manufacturerList.length === 0 ? (
              <li className="px-3 py-2 text-sm text-stone-500">該当するメーカーなし</li>
            ) : (
              manufacturerList.map(([m, count]) => (
                <li key={m}>
                  <button
                    onClick={() => setSelectedManufacturer(m)}
                    className={`flex w-full items-center justify-between px-3 py-2 text-left text-sm transition ${
                      selectedManufacturer === m
                        ? 'bg-stone-200 font-medium dark:bg-stone-800'
                        : 'hover:bg-stone-100 dark:hover:bg-stone-800'
                    }`}
                  >
                    <span className="truncate">{m || '(メーカー名なし)'}</span>
                    <span className="ml-2 shrink-0 text-xs text-stone-500">{count}</span>
                  </button>
                </li>
              ))
            )}
          </ul>
        </aside>

        <main>
          {selectedManufacturer != null ? (
            <ProductTable
              title={selectedManufacturer || '(メーカー名なし)'}
              firstType={productsForSelected[0]?.pipeType}
              products={productsForSelected}
              onBack={() => setSelectedManufacturer(null)}
            />
          ) : showFlatResults ? (
            <ProductTable
              title="検索結果（メーカー横断）"
              products={filtered.slice(0, FLAT_VIEW_LIMIT)}
              showManufacturerColumn
              note={
                filtered.length > FLAT_VIEW_LIMIT
                  ? `上位 ${FLAT_VIEW_LIMIT.toLocaleString()} 件のみ表示中（全 ${filtered.length.toLocaleString()} 件）。絞り込みを追加してください。`
                  : undefined
              }
            />
          ) : (
            <div className="rounded-md border border-dashed border-stone-300 p-12 text-center text-sm text-stone-500 dark:border-stone-700">
              左のメニューからメーカーを選択するか、検索/フィルタを使用してください
            </div>
          )}
        </main>
      </div>
    </>
  );
}

function topValues(products: PipeProduct[], pick: (p: PipeProduct) => string): [string, number][] {
  const counts = new Map<string, number>();
  for (const p of products) {
    const v = pick(p).trim();
    if (!v) continue;
    counts.set(v, (counts.get(v) ?? 0) + 1);
  }
  return [...counts.entries()].sort((a, b) => b[1] - a[1]);
}

function ensureSelected(options: [string, number][], selected: string): [string, number][] {
  if (!selected) return options;
  if (options.some(([v]) => v === selected)) return options;
  return [...options, [selected, 0]];
}

function FilterBar({
  query,
  setQuery,
  country,
  setCountry,
  grams,
  setGrams,
  countryOptions,
  gramsOptions,
  onClear,
  filtersActive,
}: {
  query: string;
  setQuery: (v: string) => void;
  country: string;
  setCountry: (v: string) => void;
  grams: string;
  setGrams: (v: string) => void;
  countryOptions: [string, number][];
  gramsOptions: [string, number][];
  onClear: () => void;
  filtersActive: boolean;
}) {
  return (
    <div className="mt-4 grid grid-cols-1 gap-3 sm:grid-cols-[1fr_auto_auto_auto]">
      <input
        type="search"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        placeholder="メーカー / フレーバー名で検索..."
        className="rounded-md border border-stone-300 bg-white px-3 py-2 text-sm placeholder-stone-400 focus:border-stone-500 focus:outline-none dark:border-stone-700 dark:bg-stone-900 dark:placeholder-stone-500"
      />
      <SelectFilter
        label="製造国"
        value={country}
        onChange={setCountry}
        options={countryOptions}
      />
      <SelectFilter
        label="容量"
        value={grams}
        onChange={setGrams}
        options={gramsOptions}
      />
      <button
        onClick={onClear}
        disabled={!filtersActive}
        className="rounded-md border border-stone-300 px-3 py-2 text-sm text-stone-700 hover:bg-stone-100 disabled:cursor-not-allowed disabled:opacity-40 dark:border-stone-700 dark:text-stone-300 dark:hover:bg-stone-800"
      >
        クリア
      </button>
    </div>
  );
}

function SelectFilter({
  label,
  value,
  onChange,
  options,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  options: [string, number][];
}) {
  return (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className="rounded-md border border-stone-300 bg-white px-3 py-2 text-sm focus:border-stone-500 focus:outline-none dark:border-stone-700 dark:bg-stone-900"
    >
      <option value="">{label}: すべて</option>
      {options.map(([v, n]) => (
        <option key={v} value={v}>
          {label}: {v} ({n})
        </option>
      ))}
    </select>
  );
}

function Loading() {
  return (
    <div className="rounded-md border border-dashed border-stone-300 p-12 text-center text-sm text-stone-500 dark:border-stone-700">
      データを読み込み中...
    </div>
  );
}

function ErrorView({ message }: { message: string }) {
  return (
    <div className="rounded-md border border-rose-300 bg-rose-50 p-6 text-sm text-rose-800 dark:border-rose-800 dark:bg-rose-950 dark:text-rose-200">
      データ取得に失敗しました: {message}
    </div>
  );
}

function ProductTable({
  title,
  firstType,
  products,
  showManufacturerColumn,
  note,
  onBack,
}: {
  title: string;
  firstType?: PipeType;
  products: PipeProduct[];
  showManufacturerColumn?: boolean;
  note?: string;
  onBack?: () => void;
}) {
  return (
    <div className="overflow-hidden rounded-md border border-stone-200 bg-white dark:border-stone-800 dark:bg-stone-900">
      <header className="flex items-center justify-between gap-3 border-b border-stone-200 px-4 py-3 dark:border-stone-800">
        <div className="flex items-center gap-3">
          {onBack && (
            <button
              onClick={onBack}
              className="text-xs text-stone-500 hover:text-stone-800 dark:hover:text-stone-200"
            >
              ← 一覧へ
            </button>
          )}
          <h2 className="text-lg font-semibold">{title}</h2>
          {firstType && <TypeBadge type={firstType} />}
        </div>
        <span className="text-xs text-stone-500">{products.length.toLocaleString()} 件</span>
      </header>
      {note && (
        <div className="border-b border-amber-200 bg-amber-50 px-4 py-2 text-xs text-amber-900 dark:border-amber-800/40 dark:bg-amber-950/30 dark:text-amber-200">
          {note}
        </div>
      )}
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead className="bg-stone-50 text-left text-xs uppercase text-stone-500 dark:bg-stone-950 dark:text-stone-400">
            <tr>
              {showManufacturerColumn && <th className="px-4 py-2">メーカー</th>}
              <th className="px-4 py-2">製品名</th>
              <th className="px-4 py-2">容量</th>
              <th className="px-4 py-2 text-right">価格</th>
              <th className="px-4 py-2">製造国</th>
              <th className="px-4 py-2">区分</th>
              <th className="px-4 py-2">更新日</th>
              <th className="px-4 py-2">出典</th>
            </tr>
          </thead>
          <tbody>
            {products.map((p, i) => (
              <tr
                key={`${p.manufacturer}-${p.name}-${p.variant ?? ''}-${i}`}
                className="border-t border-stone-100 dark:border-stone-800"
              >
                {showManufacturerColumn && (
                  <td className="px-4 py-2 text-stone-700 dark:text-stone-300">
                    {p.manufacturer || '—'}
                  </td>
                )}
                <td className="px-4 py-2 font-medium">{p.name}</td>
                <td className="px-4 py-2">{p.grams || '—'}</td>
                <td className="px-4 py-2 text-right tabular-nums">
                  {p.priceYen.toLocaleString()} 円
                </td>
                <td className="px-4 py-2">{p.country || '—'}</td>
                <td className="px-4 py-2">{p.variant || '—'}</td>
                <td className="px-4 py-2 tabular-nums">{p.updatedDate}</td>
                <td className="px-4 py-2">
                  <a
                    href={p.sourceUrl}
                    target="_blank"
                    rel="noreferrer"
                    className="text-blue-600 hover:underline dark:text-blue-400"
                  >
                    {p.source === 'henkou' ? '価格改定' : '新規認可'}
                  </a>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function TypeBadge({ type }: { type: PipeType }) {
  if (type !== 'shisha') return null;
  return (
    <span className="rounded-full bg-rose-100 px-2.5 py-0.5 text-xs font-medium text-rose-800 dark:bg-rose-900/40 dark:text-rose-300">
      水タバコ
    </span>
  );
}
