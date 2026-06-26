# Design Context

Use this as lightweight product/design memory alongside `MANIFESTO.md` and `COMPONENTS.md`.

## Project View

- Treat the project page as a focused planning console, not a place to introduce new workflow controls by default.
- Prefer the stronger hierarchy of the issue detail page: clear title card, compact metadata, purposeful cards, and restrained section language.
- Keep the project header cohesive. Project identity, actions, and tabs should feel like one unit, with the tab bar close to the project title and flush to the bottom of the header.
- Keep `Deleted issues` in the project actions menu, not in the primary tab bar.
- Use the project tabs `Sprint`, `Planned`, `All`, and `About`. `Sprint` is singular; use a human/running-style Lucide icon when available.
- Show assignee filters only where they apply. Do not preserve or display assignee filters on `About`.
- The `All` tab is the triage and discovery surface. It should feel dense and scan-friendly, with all current, past, completed, planned, and unplanned issues available through one list.
- Keep `All` page controls in one coherent section. Avoid loose chip clusters; group filters in aligned rows and separate sort controls visually while keeping them in the same control shell.
- For filters, support multi-select where it helps scanning. Statuses, priorities, and assignees use OR semantics within each group, while different groups combine together.
- Keep visual changes layout-focused unless the user explicitly asks for new creation, editing, drag/drop, or planning workflow controls.

## Context IA

- Treat context as supporting project/issue metadata on parent pages. Project About and issue detail should show context in the Details sidebar as a compact `Context` row with a count badge and a book-open manage action.
- Do not render large context cards, nested boxes, inline create/upload forms, or body previews on Project About or issue detail. Those parent pages should stay scannable and only answer "how much context is attached?" plus "where do I manage it?"
- Use context manager pages for the full workflow: project `/{owner}/projects/{key}/context` and issue `/{owner}/issues/{issueRef}/context`. These pages own create, upload, attach, view, edit, delete, link, and unlink states.
- Keep context manager rows compact by default. Show title, metadata, scope/link counts, and actions only. Body content appears only in explicit view/edit states.
- Use user-facing titles for finding and attaching context. Do not expose refs such as `context-1` as visible row labels or search/link inputs; refs may remain in URLs/API mechanics.
- Keep issue context manager actions explicit: one action to create issue-scoped context and one action to attach existing project context. Project context manager actions stay project-scoped.
- Render text context as escaped pre-wrapped text when viewing; do not turn context body into Markdown HTML unless a future product decision changes that behavior.
