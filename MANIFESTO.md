# track-slash Manifesto

track-slash is a JIRA-like issue tracker for teams that need speed, clarity, and control without vendor lock-in.

For now, architecture stays simple: a single Go binary backed by Postgres.

When a complex solution appears, first look for a way to fold it into the Go binary or Postgres directly. Extra services, queues, caches, brokers, and runtimes must earn their place.

Our ultimate goal is a tracker that is:

- Performance-first: every core workflow should feel fast under real project load, from issue search to board updates to realtime collaboration.
- Open: data models, behavior, and integration points should be understandable, documented, and easy to inspect.
- API-first: every important product capability should be available through stable, well-designed APIs before or alongside frontend support.
- Interoperable: importing from, exporting to, and syncing with other systems should be a priority, not an afterthought.
- Minimal: product surface should stay focused on issue tracking, planning, workflow, and collaboration. Power should come from composable primitives, not sprawling UI.
- Interactive: frontend interactions should be direct, responsive, and stateful where that helps users move faster.
- Frontend-performant: browser work should stay lean. Prefer fast initial loads, small payloads, efficient rendering, and accessible controls over decorative weight.

When deciding between implementation paths, prefer choices that keep track-slash fast, open, API-addressable, interoperable, and pleasant to use repeatedly.
