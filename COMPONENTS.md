# Component Reference

Reusable server-rendered UI components live in `internal/server/templates/components.html`. Prefer these before adding new markup to feature templates.

## Navigation

- `tab-bar`: sibling-view navigation with optional Lucide icons. Use for project/work/settings style view switches.

## Badges

- `issue-key`: compact issue identifier badge.
- `project-key`: compact project key badge.
- `count-badge`: small numeric count badge.
- `status-badge`: issue status badge using `statusClass`.
- `close-reason-badge`: close reason badge for closed issues.
- `missing-close-reason-badge`: dashed placeholder for invalid or incomplete closed issue detail states.
- `priority-badge`: circular P0-P4 priority marker.
- `issue-due-badge`: due-date badge with overdue/today/future styling.

## Forms

- `option-dropdown`: expanded dropdown/listbox for choosing one option and submitting immediately. Backed by `uiOptionDropdownData`; use for compact enum-like changes such as issue status and close reason.
- `autocomplete-edit`: search-style edit row with suggestions and save/cancel actions. Use for member, sprint, and similar lookup fields.

## Modals

- `modal-open` and `modal-close`: reusable modal shell with title, optional description, badges, and cancel action. Wrap workflow-specific body content between the two templates.

## Rows And Notices

- `issue-summary-row-cells`: shared issue list row cells with key, priority, title, due date, status, and close reason.
- `issue-delete-notice`: restore notice shown after deleting an issue.

## Feature Panels

- `context-manager-panel`: shared project/issue context manager page in `internal/server/templates/context_manager.html`; expects `uiContextManagerData`.

When adding a reusable component, document its template name, purpose, and expected data shape here.
