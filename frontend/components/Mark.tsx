/** Logo mark: a check (authorized) whose stroke rises and curls like shisha smoke. */
export function Mark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 64 64" aria-hidden className={className}>
      <circle cx="32" cy="32" r="31" className="fill-ink" />
      <path
        d="M15 33.5 L26 44.5 C32 38 36.8 30.5 36.3 24 C35.9 18.5 39.8 14 45 14.3 C48.6 14.5 49.6 18.2 47 19.4"
        fill="none"
        className="stroke-smoke"
        strokeWidth="5.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}
