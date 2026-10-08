import assert from 'node:assert/strict';
import { check, pdfNames, type Fetch } from '../src/check.ts';

const page = `
<a href="./20261007_kouriteika.pdf">a</a>
<a href="../20250826_kouriteika.pdf">b</a>
<a href="./202606011_kouriteikahenkou.pdf">c</a>
<a href="./20230901_kouriteika1.pdf">d</a>`;

assert.deepEqual(pdfNames(page), [
  '20230901_kouriteika1.pdf',
  '20250826_kouriteika.pdf',
  '202606011_kouriteikahenkou.pdf',
  '20261007_kouriteika.pdf',
]);

let tokenCalls = 0;
const cfg = { repo: 'o/r', workflow: 'update.yml', token: async () => (tokenCalls++, 't') };
const now = Date.parse('2026-10-08T03:00:00Z');

function stub(known: string[], run: { status: string; created_at: string } | null) {
  const calls: string[] = [];
  const f: Fetch = async (url, init) => {
    calls.push(`${init?.method ?? 'GET'} ${url}`);
    if (url.includes('kouriteika.html')) return new Response(page);
    if (url.includes('pdf-urls.json')) return Response.json(known.map((filename) => ({ filename })));
    if (url.endsWith('/runs?per_page=1')) return Response.json({ workflow_runs: run ? [run] : [] });
    if (url.endsWith('/dispatches')) return new Response(null, { status: 204 });
    throw new Error(`unexpected ${url}`);
  };
  return { f, calls };
}
const all = pdfNames(page);

// Nothing new → no GitHub calls at all.
{
  const { f, calls } = stub(all, null);
  assert.deepEqual(await check(cfg, f, now), { fresh: [], action: 'none' });
  assert.equal(calls.length, 2);
  assert.equal(tokenCalls, 0, 'no token is minted when nothing is new');
}
// New PDF and no recent run → dispatch.
{
  const { f, calls } = stub(all.filter((n) => n !== '20261007_kouriteika.pdf'), { status: 'completed', created_at: '2026-10-08T00:10:00Z' });
  const r = await check(cfg, f, now);
  assert.deepEqual(r, { fresh: ['20261007_kouriteika.pdf'], action: 'dispatched' });
  assert.ok(calls.includes('POST https://api.github.com/repos/o/r/actions/workflows/update.yml/dispatches'));
}
// New PDF but a run is in progress → wait.
{
  const { f, calls } = stub([], { status: 'in_progress', created_at: '2026-10-08T02:55:00Z' });
  assert.equal((await check(cfg, f, now)).action, 'skipped-active-run');
  assert.ok(!calls.some((c) => c.startsWith('POST')));
}
// New PDF and a run finished 5 minutes ago (its data PR may not be visible yet) → wait.
{
  const { f } = stub([], { status: 'completed', created_at: '2026-10-08T02:55:00Z' });
  assert.equal((await check(cfg, f, now)).action, 'skipped-active-run');
}
console.log('watcher tests passed');
