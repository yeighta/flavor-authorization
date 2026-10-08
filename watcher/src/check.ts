// Detects new 財務省 小売定価認可 PDFs and starts the update workflow.
// Kept free of Worker globals so it can be exercised with a stubbed fetch.

export const INDEX_URL = 'https://www.mof.go.jp/policy/tab_salt/topics/kouriteika.html';
const USER_AGENT = 'flavor-authorization-watcher/1.0 (+https://github.com/yeighta/flavor-authorization)';

// Same file-name rules as internal/source/urls.go: relative hrefs, "_1"/"1"
// suffixes, the "kouritaika" typo and 9-digit date typos.
const PDF_HREF = /href="([^"]*\d{8,9}_kourit[ae]ika[^"]*\.pdf)"/g;

/** A run started this recently is treated as already handling the new PDF. */
const RECENT_RUN_MS = 20 * 60 * 1000;

export interface Config {
  repo: string; // "owner/name"
  workflow: string; // "update.yml"
  token: string; // fine-grained PAT with Actions: read & write
}

export type Fetch = (input: string, init?: RequestInit) => Promise<Response>;

export interface Result {
  fresh: string[];
  action: 'none' | 'dispatched' | 'skipped-active-run';
}

export function pdfNames(html: string): string[] {
  const names = new Set<string>();
  for (const m of html.matchAll(PDF_HREF)) {
    const path = new URL(m[1], INDEX_URL).pathname;
    names.add(path.slice(path.lastIndexOf('/') + 1));
  }
  return [...names].sort();
}

export async function check(cfg: Config, fetchFn: Fetch, now = Date.now()): Promise<Result> {
  const page = await fetchFn(INDEX_URL, { headers: { 'User-Agent': USER_AGENT } });
  if (!page.ok) throw new Error(`index: HTTP ${page.status}`);
  const onPage = pdfNames(await page.text());

  // The repo's URL list is the source of truth for what has been picked up.
  const known = await fetchFn(`https://raw.githubusercontent.com/${cfg.repo}/main/data/pdf-urls.json`, {
    headers: { 'User-Agent': USER_AGENT, 'Cache-Control': 'no-cache' },
  });
  if (!known.ok) throw new Error(`pdf-urls.json: HTTP ${known.status}`);
  const knownNames = new Set(((await known.json()) as { filename: string }[]).map((r) => r.filename));

  const fresh = onPage.filter((n) => !knownNames.has(n));
  if (fresh.length === 0) return { fresh, action: 'none' };

  const gh = (path: string, init?: RequestInit) =>
    fetchFn(`https://api.github.com/repos/${cfg.repo}${path}`, {
      ...init,
      headers: {
        Accept: 'application/vnd.github+json',
        Authorization: `Bearer ${cfg.token}`,
        'User-Agent': USER_AGENT,
        'X-GitHub-Api-Version': '2022-11-28',
        ...(init?.body ? { 'Content-Type': 'application/json' } : {}),
      },
    });

  // Until the run's data PR merges, the new PDF still looks fresh; don't pile up runs.
  const runs = await gh(`/actions/workflows/${cfg.workflow}/runs?per_page=1`);
  if (!runs.ok) throw new Error(`list runs: HTTP ${runs.status}`);
  const latest = ((await runs.json()) as { workflow_runs: { status: string; created_at: string }[] }).workflow_runs[0];
  if (latest && (latest.status !== 'completed' || now - Date.parse(latest.created_at) < RECENT_RUN_MS)) {
    return { fresh, action: 'skipped-active-run' };
  }

  const res = await gh(`/actions/workflows/${cfg.workflow}/dispatches`, {
    method: 'POST',
    body: JSON.stringify({ ref: 'main' }),
  });
  if (res.status !== 204) throw new Error(`dispatch: HTTP ${res.status} ${await res.text()}`);
  return { fresh, action: 'dispatched' };
}
