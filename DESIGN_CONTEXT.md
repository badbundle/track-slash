# Design Context

Use this as lightweight product/design memory alongside `MANIFESTO.md` and `COMPONENTS.md`.

## Responsive Shell

- Below the `md` breakpoint, use an off-canvas navigation drawer opened from a persistent mobile app bar. Do not reserve space for a permanent icon rail on narrow screens.
- Keep mobile page gutters compact and consistent while preserving the established desktop content width and spacing.
- Dense issue rows should reflow into stacked, readable metadata on narrow screens and return to the compact column layout at `sm` and above.
- Keep primary tabs on one line. On narrow project pages, keep `Sprint`, `Planned`, and `All` visible and move `Changelog` and `About` into the project overflow menu instead of wrapping or scrolling the tab bar.

## User Identity

- Render profile images and initials fallbacks as circles everywhere. The shared `user-avatar` component owns the crop shape so individual screens cannot diverge.

## Project View

- Treat the project page as a focused planning console, not a place to introduce new workflow controls by default.
- Prefer the stronger hierarchy of the issue detail page: clear title card, compact metadata, purposeful cards, and restrained section language.
- Keep the project header cohesive. Project identity, actions, and tabs should feel like one unit, with the tab bar close to the project title and flush to the bottom of the header.
- Keep `Deleted issues` in the project actions menu, not in the primary tab bar.
- Use the wide-layout project tabs `Sprint`, `Planned`, `All`, `Changelog`, and `About`. `Sprint` is singular; use a human/running-style Lucide icon when available. Below `lg`, show only the first three as tabs and expose `Changelog` and `About` from project actions so the expanded desktop sidebar cannot squeeze the tab bar.
- Show assignee filters only where they apply. Do not preserve or display assignee filters on `About`.
- The `All` tab is the triage and discovery surface. It should feel dense and scan-friendly, with all current, past, completed, planned, and unplanned issues available through one list.
- Keep `All` page controls in one coherent section. Avoid loose chip clusters; group filters in aligned rows and separate sort controls visually while keeping them in the same control shell.
- For filters, support multi-select where it helps scanning. Statuses, priorities, and assignees use OR semantics within each group, while different groups combine together.
- Put project tag management in the project About details sidebar, parallel to issue tag management. Keep it out of the project overflow menu.
- Keep visual changes layout-focused unless the user explicitly asks for new creation, editing, drag/drop, or planning workflow controls.

## Context IA

- Treat context as supporting project/issue metadata on parent pages. Project About and issue detail should show context in the Details sidebar as a compact `Context` row with a count badge and a book-open manage action.
- Do not render large context cards, nested boxes, inline create/upload forms, or body previews on Project About or issue detail. Those parent pages should stay scannable and only answer "how much context is attached?" plus "where do I manage it?"
- Use the project context manager page `/{owner}/projects/{key}/context` for project-scoped taxonomy work: create, upload, edit, delete, link, and unlink.
- Use an issue modal for issue-scoped context work from issue detail. It should preserve the issue page, avoid URL pushes, show attached context first, support search/attach for existing project context, and allow issue-only create/upload/view/edit/remove without turning issue detail into a fullscreen manager.
- Keep context manager rows compact by default. Show title, metadata, scope/link counts, and actions only. Body content appears only in explicit view/edit states.
- Use user-facing titles for finding and attaching context. Do not expose refs such as `context-1` as visible row labels or search/link inputs; refs may remain in URLs/API mechanics.
- Keep issue context modal actions explicit: one action to create issue-scoped context and one action to attach existing project context. Project context manager actions stay project-scoped.
- Render text context as escaped pre-wrapped text when viewing; do not turn context body into Markdown HTML unless a future product decision changes that behavior.

## Tag IA

- Treat issue tags as lightweight issue-detail metadata. Show them near the issue title, not buried in the Details sidebar.
- Use a modal for issue-scoped tag attach/detach from issue detail. The modal should preserve the user's place on the issue, avoid URL pushes, support quick repeated changes, and expose search over existing project tags.
- Keep broader tag taxonomy work in the project tag manager. Issue tag modals should link out to that manager for create/edit/delete rather than becoming a second fullscreen management surface.
- Ticket numbers are identifiers, not prose. Render issue identifiers with the shared compact monospace `issue-key` treatment, including modal/header contexts.
