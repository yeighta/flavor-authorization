import { check } from './check';
import { installationToken } from './github-app';

interface Env {
  GITHUB_APP_ID: string;
  GITHUB_APP_PRIVATE_KEY: string; // PKCS#8 PEM
  REPO: string;
  WORKFLOW: string;
}

export default {
  async scheduled(_event: ScheduledEvent, env: Env, ctx: ExecutionContext) {
    const token = () => installationToken(env.GITHUB_APP_ID, env.GITHUB_APP_PRIVATE_KEY, env.REPO, fetch);
    ctx.waitUntil(
      check({ repo: env.REPO, workflow: env.WORKFLOW, token }, fetch).then((r) => {
        if (r.fresh.length > 0) console.log(JSON.stringify(r));
      }),
    );
  },
};
