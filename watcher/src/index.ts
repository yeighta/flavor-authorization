import { check } from './check';

interface Env {
  GITHUB_TOKEN: string;
  REPO: string;
  WORKFLOW: string;
}

export default {
  async scheduled(_event: ScheduledEvent, env: Env, ctx: ExecutionContext) {
    ctx.waitUntil(
      check({ repo: env.REPO, workflow: env.WORKFLOW, token: env.GITHUB_TOKEN }, fetch).then((r) => {
        if (r.fresh.length > 0) console.log(JSON.stringify(r));
      }),
    );
  },
};
