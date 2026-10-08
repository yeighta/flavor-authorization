// GitHub App authentication with WebCrypto (works in Workers and Node 22).
// The app's private key must be PKCS#8 ("BEGIN PRIVATE KEY"); GitHub issues
// PKCS#1, so convert once with `openssl pkcs8 -topk8 -nocrypt`.

import type { Fetch } from './check.ts';

const USER_AGENT = 'flavor-authorization-watcher/1.0 (+https://github.com/yeighta/flavor-authorization)';

function b64url(data: ArrayBuffer | string): string {
  const bytes = typeof data === 'string' ? new TextEncoder().encode(data) : new Uint8Array(data);
  let bin = '';
  for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

async function appJWT(appId: string, pkcs8Pem: string, now: number): Promise<string> {
  const body = pkcs8Pem.replace(/-----(BEGIN|END) PRIVATE KEY-----/g, '').replace(/\s+/g, '');
  const der = Uint8Array.from(atob(body), (c) => c.charCodeAt(0));
  const key = await crypto.subtle.importKey('pkcs8', der, { name: 'RSASSA-PKCS1-v1_5', hash: 'SHA-256' }, false, ['sign']);
  const iat = Math.floor(now / 1000) - 60; // allow for clock drift
  const unsigned = `${b64url(JSON.stringify({ alg: 'RS256', typ: 'JWT' }))}.${b64url(
    JSON.stringify({ iat, exp: iat + 540, iss: appId }),
  )}`;
  const sig = await crypto.subtle.sign('RSASSA-PKCS1-v1_5', key, new TextEncoder().encode(unsigned));
  return `${unsigned}.${b64url(sig)}`;
}

/** Returns a 1-hour installation token for the app's installation on repo. */
export async function installationToken(
  appId: string,
  pkcs8Pem: string,
  repo: string,
  fetchFn: Fetch,
  now = Date.now(),
): Promise<string> {
  const jwt = await appJWT(appId, pkcs8Pem, now);
  const headers = {
    Accept: 'application/vnd.github+json',
    Authorization: `Bearer ${jwt}`,
    'User-Agent': USER_AGENT,
    'X-GitHub-Api-Version': '2022-11-28',
  };
  const inst = await fetchFn(`https://api.github.com/repos/${repo}/installation`, { headers });
  if (!inst.ok) throw new Error(`app installation lookup: HTTP ${inst.status} (is the app installed on ${repo}?)`);
  const { id } = (await inst.json()) as { id: number };
  const tok = await fetchFn(`https://api.github.com/app/installations/${id}/access_tokens`, { method: 'POST', headers });
  if (tok.status !== 201) throw new Error(`installation token: HTTP ${tok.status}`);
  return ((await tok.json()) as { token: string }).token;
}
