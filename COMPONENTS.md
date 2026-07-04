# Component Reference

Reusable server-rendered UI components live in `internal/server/templates/components.html`. Prefer these before adding new markup to feature templates.

## Navigation

- `tab-bar`: sibling-view navigation with optional Lucide icons. Use for project/work/settings style view switches.
- `sidebar-favorites`: shell sidebar favorite-project shortcuts backed by `uiSidebarFavoritesData`. Keep it directly below the `Projects` nav item with a subtle divider from standard navigation, and refresh it with OOB HTMX swaps when favorite state changes.
- `issue-list-controls`: collapsible shared status, priority, tag, assignee, sort, and direction controls for issue list views. Closed by default; summary shows active filter count plus current sort/direction. Sort uses dropdown options including due date; direction uses Asc/Desc dropdown options with arrow icons. Expects `uiIssueControlsData`; omit tag fields for cross-project lists and omit assignee fields for current-user scoped lists.

## Badges

- `issue-key`: compact monospace issue identifier badge. Use it for ticket numbers wherever possible; if a generic data-driven badge must show an issue identifier, mirror this component's monospace, uppercase, compact bordered treatment.
- `project-key`: compact project key badge.
- `count-badge`: small numeric count badge.
- `status-badge`: issue status badge using `statusClass`.
- `close-reason-badge`: close reason badge for closed issues.
- `missing-close-reason-badge`: dashed placeholder for invalid or incomplete closed issue detail states.
- `priority-badge`: circular P0-P4 priority marker.
- `tag-badge`: compact hashtag badge for `model.IssueTag`, using `DisplayName` and `tagClass .Color`.
- `issue-due-badge`: due-date badge with overdue/today/future styling.

## Forms

- `option-dropdown`: expanded dropdown/listbox for choosing one option and submitting immediately. Backed by `uiOptionDropdownData`; use for compact enum-like changes such as issue status and close reason.
- `autocomplete-input` and `autocomplete-options`: shared search/autofill building blocks backed by `uiAutocompleteEditData` and `uiAutocompleteOption`. Supports local option filtering, optional hidden target values, addressable option containers, collapsible suggestions, and optional debounced HTMX refresh on input for server-filtered suggestions.
- `autocomplete-edit`: search-style edit row with suggestions and save/cancel actions. Use for member, sprint, and similar lookup fields.

## Modals

- `modal-open` and `modal-close`: reusable modal shell with title, optional description, badges, and cancel action. Wrap workflow-specific body content between the two templates.
- Issue-scoped relationship edits should prefer modals over fullscreen manager pages when the user is making a small local change from issue detail. Keep the surrounding issue context visible, avoid URL pushes for open/submit/close, support repeated HTMX updates inside the modal, and link out to the fuller manager when the task expands.
- Issue tag modal convention: show attached tags first, then a searchable list of available project tags. Attach/detach existing tags only; create/edit/delete project tags stays in the project tag manager.
- Issue context modal convention: show attached context first, then either a searchable list of available project context or the issue-only create/upload editor. View and edit bodies inside the modal; keep text escaped and pre-wrapped. Project-wide context creation, deletion, and linked-issue management stays in the project context manager.

## Rows And Notices

- `issue-summary-row-cells`: shared issue list row cells with key, priority, title, due date, status, and close reason.
- `issue-delete-notice`: restore notice shown after deleting an issue.
- Context detail row: parent project/issue pages should use a Details-sidebar row labeled `Context`, a `count-badge`, and a compact book-open manage link. On issue detail this link opens the context modal with `hx-push-url="false"`; on Project About it opens the project context manager. Keep create/attach/edit/delete controls out of parent panels.

## Feature Panels

- `project-favorite-action`: project header star toggle backed by `uiProjectFavoriteData`/`uiProjectPanelData`. Keep it adjacent to the project title and update only the action wrapper plus `sidebar-favorites`.
- `context-manager-panel`: project context manager page in `internal/server/templates/context_manager.html`; expects `uiContextManagerData`. Use this as the full project context workflow surface, not an inline parent-page form. Issue detail context management now renders as a modal inside `issue-panel`.

### Context Manager Conventions

- Project manager route: `/{owner}/projects/{key}/context`. Issue modal route: `/{owner}/issues/{issueRef}/context`.
- Project mode supports project-scoped create/upload/edit/delete and linked-issue management. Issue modal mode supports creating issue-scoped context, attaching existing project context, viewing/editing linked context, and removing links.
- Rows stay compact by default: show title, metadata, scope/link counts, and icon actions. Do not show body previews in rows.
- Body text appears only in explicit view/edit manager states, rendered as escaped pre-wrapped text.
- User-facing attach/search controls use context titles. Do not present refs such as `context-1` as visible identifiers, badges, placeholders, or option labels.

When adding a reusable component, document its template name, purpose, and expected data shape here.
