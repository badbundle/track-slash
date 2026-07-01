# Component Reference

Reusable server-rendered UI components live in `internal/server/templates/components.html`. Prefer these before adding new markup to feature templates.

## Navigation

- `tab-bar`: sibling-view navigation with optional Lucide icons. Use for project/work/settings style view switches.
- `issue-list-controls`: collapsible shared status, priority, tag, assignee, sort, and direction controls for issue list views. Closed by default; summary shows active filter count plus current sort/direction. Sort uses dropdown options including due date; direction uses Asc/Desc dropdown options with arrow icons. Expects `uiIssueControlsData`; omit tag fields for cross-project lists and omit assignee fields for current-user scoped lists.

## Badges

- `issue-key`: compact issue identifier badge.
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

## Rows And Notices

- `issue-summary-row-cells`: shared issue list row cells with key, priority, title, due date, status, and close reason.
- `issue-delete-notice`: restore notice shown after deleting an issue.
- Context detail row: parent project/issue pages should use a Details-sidebar row labeled `Context`, a `count-badge`, and a compact book-open manage link. Keep create/attach/edit/delete controls out of parent panels.

## Feature Panels

- `context-manager-panel`: shared project/issue context manager page in `internal/server/templates/context_manager.html`; expects `uiContextManagerData`. Use this as the full context workflow surface for project and issue modes, not a modal or inline parent-page form.

### Context Manager Conventions

- Project manager route: `/{owner}/projects/{key}/context`. Issue manager route: `/{owner}/issues/{issueRef}/context`.
- Project mode supports project-scoped create/upload/edit/delete and linked-issue management. Issue mode supports creating issue-scoped context, attaching existing project context, viewing/editing linked context, and removing links.
- Rows stay compact by default: show title, metadata, scope/link counts, and icon actions. Do not show body previews in rows.
- Body text appears only in explicit view/edit manager states, rendered as escaped pre-wrapped text.
- User-facing attach/search controls use context titles. Do not present refs such as `context-1` as visible identifiers, badges, placeholders, or option labels.

When adding a reusable component, document its template name, purpose, and expected data shape here.
