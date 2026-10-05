'use client';

import { useEffect, useRef } from 'react';
import { formatDate, gramsValue, yen, type Flavor } from '@/lib/data';
import { FAMILIES } from '@/lib/flavor';
import type { PricePoint } from '@/lib/types';
import { FAMILY_BG } from './Ledger';

export function FlavorSheet({
  sizes,
  siblings,
  onClose,
  onBrand,
}: {
  sizes: Flavor[];
  /** Other flavors of the same brand, one entry per flavor. */
  siblings: Flavor[];
  onClose: () => void;
  onBrand: (brand: string) => void;
}) {
  const heading = useRef<HTMLHeadingElement>(null);
  const first = sizes[0];
  const sorted = [...sizes].sort((a, b) => gramsValue(a.grams) - gramsValue(b.grams));
  const since = sizes.reduce((m, f) => (f.since < m ? f.since : m), first.since);

  useEffect(() => {
    heading.current?.focus();
  }, [first.id]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose();
    document.addEventListener('keydown', onKey);
    const overflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      document.removeEventListener('keydown', onKey);
      document.body.style.overflow = overflow;
    };
  }, [onClose]);

  const family = FAMILIES.find((f) => f.id === first.family)!;

  return (
    <div className="fixed inset-0 z-40" role="presentation">
      <button
        type="button"
        aria-label="閉じる"
        tabIndex={-1}
        onClick={onClose}
        className="absolute inset-0 animate-veil-in bg-ink/30 backdrop-blur-[2px]"
      />
      <aside
        role="dialog"
        aria-modal="true"
        aria-labelledby="sheet-title"
        className="absolute inset-y-0 right-0 flex w-full max-w-[34rem] animate-sheet-in flex-col overflow-y-auto bg-glass shadow-[-24px_0_60px_-30px_rgb(var(--ink)/0.45)]"
      >
        <div className="flex items-center justify-between px-6 pt-5 sm:px-9">
          <button
            type="button"
            onClick={() => onBrand(first.brand)}
            className="font-display text-[1.05rem] font-bold uppercase tracking-[0.02em] underline-offset-4 hover:underline"
          >
            {first.brand}
          </button>
          <button type="button" onClick={onClose} className="-mr-2 rounded-full p-2 text-haze hover:text-ink" aria-label="閉じる">
            <svg aria-hidden viewBox="0 0 20 20" className="h-5 w-5">
              <path d="M5 5l10 10M15 5L5 15" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
            </svg>
          </button>
        </div>

        <div className="px-6 pb-10 pt-8 sm:px-9">
          <h2
            id="sheet-title"
            ref={heading}
            tabIndex={-1}
            className="break-words font-display text-[clamp(2.75rem,9vw,4.25rem)] font-extrabold uppercase leading-[0.92] outline-none"
          >
            {first.name}
          </h2>
          <p className="mt-4 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-haze">
            <span className="flex items-center gap-1.5">
              <span aria-hidden className={`h-2 w-2 rounded-full ${FAMILY_BG[first.family]}`} />
              {family.label}
            </span>
            {first.country && <span>{first.country}製</span>}
            <span>{formatDate(since)}に初めて認可</span>
          </p>

          <div className="mt-10 space-y-10">
            {sorted.map((f) => (
              <Size key={f.id} flavor={f} />
            ))}
          </div>

          {siblings.length > 0 && (
            <section className="mt-14 border-t border-veil pt-5">
              <h3 className="text-sm text-haze">{first.brand}のほかのフレーバー</h3>
              <ul className="mt-3 flex flex-wrap gap-x-4 gap-y-1">
                {siblings.slice(0, 30).map((s) => (
                  <li key={s.id}>
                    <a href={`#f=${s.id}`} className="font-display text-[1.2rem] font-medium leading-snug hover:underline hover:underline-offset-4">
                      <span aria-hidden className={`mr-1 inline-block h-[0.4em] w-[0.4em] -translate-y-[0.1em] rounded-full ${FAMILY_BG[s.family]}`} />
                      {s.name}
                    </a>
                  </li>
                ))}
              </ul>
              {siblings.length > 30 && (
                <button type="button" onClick={() => onBrand(first.brand)} className="mt-4 text-sm underline underline-offset-4">
                  {first.brand}の{siblings.length + 1}種類をすべて見る
                </button>
              )}
            </section>
          )}
        </div>
      </aside>
    </div>
  );
}

function Size({ flavor }: { flavor: Flavor }) {
  const latest = flavor.history[flavor.history.length - 1];
  return (
    <section className="border-t border-veil pt-5">
      <div className="flex items-baseline justify-between gap-4">
        <h3 className="text-sm text-haze">
          {gramsValue(flavor.grams) ? `${gramsValue(flavor.grams)}g` : '容量不明'}
          {flavor.variant && <>・{flavor.variant}</>}
        </h3>
        {flavor.delta != null && flavor.delta !== 0 && (
          <p className={`text-sm font-bold tabular-nums ${flavor.delta > 0 ? 'text-ember' : 'text-cool'}`}>
            {flavor.delta > 0 ? '値上げ' : '値下げ'} {flavor.delta > 0 ? '+' : '−'}
            {yen(Math.abs(flavor.delta))}
          </p>
        )}
      </div>
      <p className="mt-1 font-display text-[3.25rem] font-bold leading-none tabular-nums">{yen(flavor.priceYen)}</p>
      {flavor.history.length === 1 ? (
        <p className="mt-3 text-[0.8125rem] text-haze">
          {formatDate(latest.date)}に認可。
          <a href={latest.sourceUrl} target="_blank" rel="noreferrer" className="underline underline-offset-4 hover:text-ink">
            公表PDF
            <span className="sr-only">（財務省、新しいタブで開きます）</span>
          </a>
        </p>
      ) : (
        <>
          <p className="mt-2 text-[0.8125rem] text-haze">{formatDate(latest.date)}から</p>
          <PriceSteps history={flavor.history} />
          <PriceList history={flavor.history} />
        </>
      )}
    </section>
  );
}

function PriceList({ history }: { history: PricePoint[] }) {
  return (
      <ol className="mt-5 space-y-1.5 text-[0.8125rem]">
        {[...history].reverse().map((h) => (
          <li key={h.date} className="flex items-baseline justify-between gap-3">
            <span className="tabular-nums text-haze">{formatDate(h.date)}</span>
            <span className="flex-1 border-b border-dotted border-veil" aria-hidden />
            <span className="tabular-nums">{yen(h.priceYen)}</span>
            <a href={h.sourceUrl} target="_blank" rel="noreferrer" className="text-haze underline underline-offset-4 hover:text-ink">
              {h.source === 'henkou' ? '改定の公表' : '認可の公表'}
              <span className="sr-only">（財務省のPDF、新しいタブで開きます）</span>
            </a>
          </li>
        ))}
      </ol>
  );
}

/** Step chart: an authorized price holds until the next notice replaces it. */
function PriceSteps({ history }: { history: PricePoint[] }) {
  const W = 440;
  const H = 96;
  const pad = 6;
  const t = (d: string) => new Date(`${d}T00:00:00Z`).getTime();
  const start = t(history[0].date);
  const end = Math.max(Date.now(), t(history[history.length - 1].date) + 1);
  const prices = history.map((h) => h.priceYen);
  // Scale to the price itself rather than to the min–max range, so a ¥100 change
  // on ¥1,600 reads as the small step it is instead of a cliff.
  const lo = Math.min(...prices) * 0.8;
  const hi = Math.max(...prices) * 1.05;
  const x = (d: number) => pad + ((d - start) / (end - start)) * (W - pad * 2);
  const y = (p: number) => (hi === lo ? H / 2 : H - pad - ((p - lo) / (hi - lo)) * (H - pad * 2));

  let d = `M${x(start)},${y(history[0].priceYen)}`;
  history.forEach((h, i) => {
    if (i > 0) d += ` H${x(t(h.date))} V${y(h.priceYen)}`;
  });
  d += ` H${x(end)}`;

  return (
    <svg viewBox={`0 0 ${W} ${H}`} className="mt-5 h-24 w-full overflow-visible" role="img" aria-label={`価格の推移: ${history.map((h) => `${formatDate(h.date)} ${yen(h.priceYen)}`).join('、')}`}>
      <line x1={pad} x2={W - pad} y1={H - 0.5} y2={H - 0.5} stroke="rgb(var(--veil))" />
      <path d={d} fill="none" stroke="rgb(var(--ink))" strokeWidth="2" strokeLinejoin="round" />
      {history.map((h, i) => (
        <circle
          key={h.date}
          cx={x(t(h.date))}
          cy={y(h.priceYen)}
          r="3.5"
          fill={i === 0 ? 'rgb(var(--glass))' : h.priceYen > history[i - 1].priceYen ? 'rgb(var(--ember))' : 'rgb(var(--cool))'}
          stroke="rgb(var(--ink))"
          strokeWidth={i === 0 ? 1.5 : 0}
        />
      ))}
    </svg>
  );
}
