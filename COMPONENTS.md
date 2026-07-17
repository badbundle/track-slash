# Component Reference

Reusable server-rendered UI components live in `internal/server/templates/components.html`. Prefer these before adding new markup to feature templates.

## Navigation

- `breadcrumb`: subtle entity hierarchy navigation backed by `uiBreadcrumbData`. Project pages show `Projects / Project name / Current view`; issue pages append the current issue key to the project, with a parent issue key between them for sub-issues.
- `tab-bar`: single-line sibling-view navigation with optional Lucide icons, backed by `uiTabBarData`. Set an item's `MobileOverflow` flag when its owning page provides an equivalent constrained-screen overflow-menu link; those tabs return at `lg`.
- `sidebar-favorites`: shell sidebar favorite-project shortcuts backed by `uiSidebarFavoritesData`. Keep it directly below the `Projects` nav item with a subtle divider from standard navigation, and refresh it with OOB HTMX swaps when favorite state changes.
- `issue-list-controls`: collapsible shared status, priority, tag, assignee, sort, and direction controls for issue list views. Closed by default; summary shows active filter count plus current sort/direction. Sort uses dropdown options including due date; direction uses Asc/Desc dropdown options with arrow icons. Expects `uiIssueControlsData`; omit tag fields for cross-project lists and omit assignee fields for current-user scoped lists.

## Controls

- App tooltip: one body-level tooltip in `shell_scripts.html` automatically labels interactive controls that have an `aria-label` but no visible text. It appears on pointer hover and keyboard focus, follows the control through the shared CSS anchor in `frontend/tailwind.css`, and stays outside card overflow. Keep labels concise and action-oriented; do not add a tooltip to controls whose text is already visible.

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

## Avatars

- `user-avatar`: circular user avatar with thumbnail-or-initials fallback. Pass `userAvatar <user-like value> <class>` where the value is `model.User`, `model.ProjectMember`, `model.ProjectAssignee`, `model.ProjectAssigneeIssueStats`, `model.ProjectChangelogActor`, or `uiIssueCommentItem`. The shared component owns the circular crop and clipping; callers own dimensions, colors, and borders through the class string. The helper adds cache-busting thumbnail URLs with `?v={thumbnail_object_id}` and falls back to initials from display name, username, or email.
- `project-icon`: square project image with a small corner radius and project-initial fallback, backed by `uiProjectIconData`. Pass `projectIcon <project> <class>`; the helper adds a cache-busting thumbnail URL and the component owns the square crop and radius.

## Forms

- `option-dropdown`: expanded dropdown/listbox for choosing one option and submitting immediately. Backed by `uiOptionDropdownData`; use for compact enum-like changes such as issue status and close reason.
- `autocomplete-input` and `autocomplete-options`: shared search/autofill building blocks backed by `uiAutocompleteEditData` and `uiAutocompleteOption`. Supports local option filtering, optional hidden target values, addressable option containers, collapsible suggestions, and optional debounced HTMX refresh on input for server-filtered suggestions.
- `autocomplete-edit`: search-style edit row with suggestions and save/cancel actions. Use for member, sprint, and similar lookup fields.

## Modals

- `modal-open` and `modal-close`: reusable modal shell with title, optional description, badges, and cancel action. Wrap workflow-specific body content between the two templates.
- `image-picker`: shared client-controlled profile/project image modal backed by `uiImagePickerData`. Keep only the current avatar/icon and its Add/Change trigger in the owning panel; file selection, upload, and removal live inside this modal. Profile previews remain circular and project previews remain square with a small corner radius.
- Issue-scoped relationship edits should prefer modals when the user is making a small local change from issue detail. Issue Context is the deliberate exception because browsing and editing multiple documents benefits from its integrated manager. Other modal workflows should keep the surrounding issue visible, avoid URL pushes for open/submit/close, support repeated HTMX updates, and link out when the task expands.
- Issue tag modal convention: show attached tags first, then a searchable list of available project tags. Attach/detach existing tags only; create/edit/delete project tags stays in the project tag manager.

## Rows And Notices

- `issue-summary-row`: responsive issue list row content accepting an issue. It stacks key/priority, title/tags, and due/status metadata on mobile, then restores the compact four-column row from `sm` upward.
- `issue-delete-notice`: restore notice shown after deleting an issue.
- Context detail row: issue detail uses a Details-sidebar row labeled `Context`, a `count-badge`, and a compact book-open action that opens the integrated issue Context manager and pushes its URL. Project About does not render context; project context is a top-level tab.

## Feature Panels

- `project-favorite-action`: project header star toggle backed by `uiProjectFavoriteData`/`uiProjectPanelData`. Keep it adjacent to the project title and update only the action wrapper plus `sidebar-favorites`.
- `project-panel-context`: integrated project Context tab in `internal/server/templates/project_panel_context.html`; expects `uiContextManagerData` through `uiProjectPanelData.ContextManager`. It owns the ordered page list, selected Markdown page, and complete linked-issue list in a separate section below the document card.
- `project-member-page`: full project member manager in `internal/server/templates/project_member_page.html`; expects the member fields on `uiProjectPanelData`. Keep the owner fixed, use avatar/name/username rows with inline role selectors, confirm removals, and re-render the page after each mutation.
- `context-manager-panel`: routes issue mode to the integrated list/document manager in `internal/server/templates/issue_context_manager.html`; project mode remains a compatibility fallback because project Context normally renders through `project-panel-context`.
- `description-body`: shared safe Markdown display backed by `uiDescriptionBodyData`. Project, issue, and sprint adapters pass attachment-scoped rendered HTML.
- `description-editor`: shared Markdown textarea backed by `uiDescriptionEditorData`, with optional upload and attachment-list URLs. Creation forms omit upload configuration until a parent ref exists.
- `description-attachment-list`: shared project/issue/sprint attachment rows backed by `uiAttachmentListData`, including previews, metadata, Markdown copy, download, delete, pagination notice, and editing state.
- `sprint-description`: shared active/planned/completed-history sprint cropped-Markdown preview backed by `uiSprintDescriptionData` or the matching fields on `uiPlannedSprint`. It lazily swaps full Markdown and attachment rows through `See more` without affecting the sprint-issues disclosure.

### Context Page Conventions

- Project tab route: `/{owner}/projects/{key}/context`; selected pages use `/context/{contextRef}`. Issue manager route: `/{owner}/issues/{issueRef}/context`, with the same selected-page suffix.
- Project pages support create/import/edit/delete, page-scoped attachments, ordering, and linked-issue management. The page list stays compact without per-page issue counts; only the selected page renders content, followed by its complete linked-issue list and count.
- Markdown pages use the shared safe Markdown renderer and attachment components. Plain-text imports remain escaped and pre-wrapped.
- Issue manager mode supports creating issue-scoped context, attaching existing project pages, viewing/editing linked content, and removing links in the same responsive list/document pattern as project Context.
- User-facing attach/search controls use context titles. Do not present refs such as `context-1` as visible identifiers, badges, placeholders, or option labels.

When adding a reusable component, document its template name, purpose, and expected data shape here.
