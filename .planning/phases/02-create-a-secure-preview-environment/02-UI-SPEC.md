---
phase: 2
slug: create-a-secure-preview-environment
status: approved
reviewed_at: 2026-10-12
shadcn_initialized: true
preset: none (components.json present: style new-york, baseColor neutral, cssVariables true, lucide)
extends: .planning/phases/01-telemetry-spine-github-login/01-UI-SPEC.md
created: 2026-10-12
---

# Phase 2 — UI Design Contract

> Extends the approved Phase 1 contract. Everything not restated here is inherited unchanged: Tailwind 4 + shadcn (new-york, neutral, Radix), lucide-react, Inter + mono stack, spacing scale 4/8/16/24/32/48/64, four type sizes 12/14/20/28 with weights 400/600 (line height 1.5 body/label, 1.2 heading/display), light and dark palettes (all hex values and contrast ratios), health-badge icon+text rule, 2px accent focus ring, 44px (h-11) minimum control height, 56px fixed header, 1024px content width, PageHeading focus on route change, trace-aware loading/error contract (independent skeletons, no silent retries, trace ID with copy button on every error), reduced-motion rules. Sources: 02-CONTEXT D-01..D-19, ENV-09. Deliberate additions are marked **ADDED**.

Screens in scope: (1) Environments entry in shell nav, (2) Create-environment form, (3) Environment detail and creation progress, (4) Environments launch page, (5) Audit log (admin), (6) Preview access interstitial and denied pages.

---

## Design System

| Property | Value |
|----------|-------|
| Tool | shadcn (CLI), already initialized (`web/components.json`, new-york, neutral) |
| Preset | not applicable (default init, as Phase 1) |
| Component library | radix |
| Icon library | lucide-react |
| Font | unchanged from Phase 1 (Inter stack; mono for env IDs, SHAs, trace IDs, preview URLs, ref values) |

Installed today: avatar, badge, button, card, dropdown-menu, skeleton, table, tooltip.
**ADDED (official shadcn registry only):** `input`, `label`, `select`, `radio-group`, `switch`, `alert`. No other new components. Stepper, definition list, field wrapper, copy-trace button and status badge are app components composed from these primitives (see Component Inventory).

---

## Spacing Scale

Unchanged (4/8/16/24/32/48/64). Phase 2 usage:

| Context | Token |
|---------|-------|
| Label to control | sm (8px); control to hint or error text: xs (4px) |
| Field to field within a group | md (16px) |
| Group to group (Target, Template options, Size) | lg (24px) |
| Form card inner padding | xl (32px) |
| Size-preset cards gap | md (16px); card inner padding md |
| Audit filter bar gap | sm (8px) horizontally, md below |

Exceptions: all inputs, selects, radio cards, switch rows and icon buttons are at least 44px high (h-11), per Phase 1. Form column max-width 640px (not a scale token; layout constraint inside the 1024px content area).

---

## Typography

Unchanged: Body 14/400/1.5, Label 12/600/1.5, Heading 20/600/1.2, Display 28/600/1.2. No new sizes or weights.

Phase 2 role mapping: Display = interstitial and preview-denied titles only (they are auth-style pages). Heading = page titles (PageHeading), card titles, form group titles. Label = field labels, table column heads, badge text, stepper step names. Body = hints, errors, table cells. Mono (Body size, 400) = environment ID, SHA, trace ID, preview URL, repo, ref value.

---

## Color

Unchanged palettes and contrast table. Phase 2 additions to the rules:

**Accent reserved list (ADDED items marked):** (1) "Continue with GitHub", (2) "Open in Grafana" link button on the status page, (3) focus ring, (4) active-route nav indicator, (5) **ADDED** "Create environment" primary button (create page only), (6) **ADDED** "Open preview" button (detail page, only when status is Running). At most one accent-filled button per page. "View trace in Grafana" and "View Actions run" are `outline` buttons, not accent. Accent is never used for status badges, stepper, links in tables, or selected radio cards (selection is shown with a 2px foreground-colored border plus a check-circle-2 icon; focus uses the accent ring).

Destructive (existing hex) is used for: field error text and error icons, `Failed` status, `Denied` audit outcome, error Alerts. No destructive actions exist in this phase, so no confirm dialogs. Warning color remains **Degraded badge only**; Phase 2 does not use it.

Status/outcome encoding (icon + text always; color never alone):

| State | Icon (lucide) | Text color | Label |
|-------|---------------|-----------|-------|
| Requested | clock | muted | Requested |
| Provisioning | loader-2 (spins; static under reduced motion) | foreground | Provisioning |
| Running | check-circle-2 | foreground | Running |
| Failed | x-circle | destructive | Failed |
| Audit: success | check-circle-2 | foreground | Succeeded |
| Audit: denied (guardrail) | ban | destructive | Denied |
| Audit: rejected (validation) | circle-alert | foreground | Rejected |
| Audit: unknown outcome | help-circle | muted | Unknown |

Badges use the existing secondary Badge surface with the icon at 16px, 4px gap, Label type. Denied vs Rejected differ in icon shape, label text and color; Denied is the only audit outcome in destructive color.

---

## Screens and Layout

### 1. Shell navigation (ADDED items)
- `NAV_ITEMS` becomes: **Telemetry** (`/`, exact match), **Environments** (`/environments`, active for any `/environments/*` path, so non-exact), **Audit log** (`/admin/audit`, rendered only when `role === "admin"`). Items get `adminOnly?: boolean` and `exact?: boolean` fields; markup, 56px height and accent active indicator are unchanged.
- Hiding Audit log for non-admins is a hint only; the route and API enforce access (screen 5 defines the in-shell denied view).
- `RoleBadge` gains a `Creator` value (**ADDED**): roles shown are Admin, Creator, Viewer, same secondary badge, text only. `/api/me` role type becomes `admin | creator | viewer` (API change for the planner). Role shown is the login-time hint; the server remains authoritative.

### 2. Environments launch page (`/environments`) — lightest page that reaches a new environment
Vertical stack, gap lg, inside shell.
1. Header row: PageHeading "Environments"; right: an `outline`-variant "New environment" button (plus icon) linking to `/environments/new`, rendered only for creator and admin. (Accent is kept for the single Create environment button on the form page.) For viewers the button is replaced by Body muted text: "Creating environments requires the Creator role."
2. Card "Your recent environments": up to **5** most recently created environments owned by the signed-in user, newest first, as a table (Environment, Status, Template, Created). Environment cell is a link (mono short ID) to `/environments/{id}`. Status uses the status badge. No filters, sort, paging, search, owner column or destroy control; those are Phase 3.
3. Caption under the card (Body, muted): "The full environment inventory arrives in a later phase."
- Empty: heading "No environments yet", body creators: "Create your first preview environment from an allowed repository." with the New environment button; viewers: "Environments you create will appear here. Ask an admin for the Creator role to create one."
- Loading: card skeleton with 3 row skeletons. Error: Phase 1 error pattern ("Couldn't load environments", trace, button "Retry Loading Environments").
- Phase 3 replaces the card body in place; route and heading are stable.

### 3. Create-environment form (`/environments/new`)
- PageHeading "New environment". Single card, max-width 640px, padding xl, left-aligned in content area. Form groups top to bottom (each group has a Heading-role title at 20px only for "Target", "Template", "Size"; fields inside use Label + control):

**Target**
- **Repository** (`select`, required): options from `ALLOWED_REPOS` via API, mono `org/name`. Preselected when exactly one exists. If the list is empty (fail closed, D-07): whole form disabled with Alert (see Guardrail copy).
- **Ref type** (`radio-group`, horizontal, 3 options): Branch, Pull request, Commit SHA. Default Branch.
- **Ref** (`input`, required): label and placeholder follow the type: Branch "main"; Pull request "123" (numeric, `inputmode="numeric"`); Commit SHA "7 to 40 hex characters". Hint under input (Body, muted): "Resolved to a commit SHA when you create. The image ghcr.io/{repo}:{sha} must already exist." Switching type clears the input and its error. No client-side resolution or live lookup (server-side only, D-12).

**Template**
- **Template** (`select`, required): options from the server catalog (chart name, with chart description as second line in the option). Each option shows its name; a template whose schema cannot be rendered (see below) is shown disabled with "Not available in this form". No default selection: placeholder "Select a template".
- **Template options**: fields rendered from the selected chart's `values.schema.json` (see Schema-driven fields). Shown inside a bordered region (secondary surface) titled "Template options" only when the template has at least one renderable property. Changing template replaces the region, discards previous option values and errors, and announces "Template options updated" via `aria-live="polite"`.

**Size**
- **Size preset** (`radio-group` rendered as 3 selectable cards in a row; stack below 768px). Cards show name (Heading role), then Body lines for CPU and memory limits from the API (single source of truth for the ResourceQuota/LimitRange values; users never type raw values, GRD-01). Default **S**. Default display values (Claude's discretion, fits a laptop kind cluster; the API supplies the real numbers and the UI must render whatever it returns):

| Preset | CPU (namespace limit) | Memory (namespace limit) | Card helper text |
|--------|----------------------|--------------------------|------------------|
| S | 0.5 vCPU | 512 MiB | Small: demos and smoke checks |
| M | 1 vCPU | 1 GiB | Medium: typical web app and database |
| L | 2 vCPU | 2 GiB | Large: heavier services; uses more of the shared cluster |

  Under the cards: Body muted "Limits apply to the whole environment namespace."

**Footer**: right-aligned, "Create environment" accent button (44px) and a ghost "Back to Environments" button linking to `/environments`. Also a Body muted line left of the buttons: "Expires 24 hours after creation (informational)" with the actual default from API (D-13).

**Roles**
- Creator, admin: form as above.
- Viewer: page and form are visible but entirely disabled (`<fieldset disabled>`, controls non-focusable), with an `Alert` (info, icon `info`) above the card: title "You have view-only access", body "Creating environments requires the Creator role. Ask an admin to add you to a creator team, then sign in again." Create button is disabled and described by the alert (`aria-describedby`). Disabled text keeps AA contrast for the explanation; the explanation is never conveyed only by the disabled state. The viewer reaches this URL only by direct link (launch page hides the button), which is why it explains instead of redirecting.
- Role is a hint; the server re-verifies on submit (D-06). If the server answers 403, the guardrail rejection flow below applies even to users shown the enabled form.

**Schema-driven fields (D-10, D-12)**

Schema source is the chart's `values.schema.json` (JSON Schema), delivered as a trimmed form schema by the API. Render in schema property order; label = `title` else humanized key; hint = `description`; initial value = `default`; required marker = text "(required)" appended to the Label, not an asterisk alone.

| Schema construct | Control | Notes |
|------------------|---------|-------|
| `type: string` | `input` | Shows `minLength`/`maxLength` as hint "3 to 30 characters" when present. `pattern` is validated by the server only; hint shows the schema `description`. |
| `type: string` + `enum` | `select` | Enum values as option labels (use `x-labels` if present, else the raw value). |
| `type: integer` / `number` | `input type=number` | `minimum`/`maximum` shown as hint "1 to 5" and set as min/max attributes; server still authoritative. Example: `replicas`. |
| `type: boolean` | `switch` with Label on the left | Default false unless `default`. |
| `type: object` with only supported children, one level deep | `fieldset` with legend (Label role) containing the child fields | Dotted path ids: `values.ingress.host`. |

Unsupported constructs degrade, never crash and never expose a raw JSON editor:
- `oneOf`, `anyOf`, `allOf`, `$ref`, `if/then`, arrays, `patternProperties`, objects nested deeper than one level, `const`-only, unknown `type`: the property is **not rendered** and the chart default is used.
- If an unsupported property is `required` and has no `default`, the template is disabled in the picker with the option text "Not available in this form" (the form cannot produce a valid request).
- If one or more properties were omitted but the template is usable, a muted Body line appears at the bottom of the options region: "Some advanced options aren't available in this form. Chart defaults are used." (no expandable details).
- Server schema violations on any path map to the field whose id matches the JSON pointer (`/replicas` becomes `values.replicas`). A violation with no matching rendered field (including an omitted property) appears in the summary only, with its path in mono.

**Server field errors (D-10, D-11, GRD-04) — contract**
- API returns 422 with `errors: [{ field, code, message }]` plus the trace ID header. UI is the same form with entries preserved (no field reset except none).
- **Summary:** an `Alert` (destructive, icon `circle-alert`, `role="alert"`) above the form: title "Fix {n} {field|fields} to create this environment" and a list of links, one per error, text "{Field label}: {message}". Each link moves focus to its field (`#field-id`). The summary is announced by the live region but does **not** take focus.
- **Focus:** on a failed submit, focus moves to the first invalid control in DOM order (Repository, Ref type group, Ref, Template, Template options in schema order, Size) and it scrolls into view with 24px margin. This is the only automatic focus move.
- **Per field:** the control gets `aria-invalid="true"`, `aria-describedby` pointing to its error message (and hint), destructive 2px border; error message below it with `circle-alert` icon (16px) + text in destructive color, Body size. Error clears when the user edits that field. Hint text remains visible with the error.
- No validation on blur and no per-keystroke validation. Client-side checks only block an empty required field on submit and use the same presentation with copy "Enter a {field}." / "Select a {field}."
- Submit pending: button label "Creating environment…" with loader-2, button `disabled` and `aria-busy`, fields stay enabled but a second submit is blocked. No optimistic navigation: navigate only after the server returns 202 with the environment id.
- Request failure with no response/5xx: `Alert` "Couldn't create the environment" with the trace ID and copy button; form values preserved; button returns to "Create environment" (a user retry is a new traced request; no auto retry).
- Server success: `navigate` to `/environments/{id}`; focus lands on PageHeading there.
- Every rejection and denial is recorded in the audit log (D-19). Alerts therefore end with "This attempt was recorded in the audit log." when the server marks the event as audited.

**Guardrail rejection (explained, not just blocked)**
| Case | Where shown | Copy |
|------|-------------|------|
| 403 not a creator or admin (checked at request time) | Alert at top, destructive, `role="alert"`; form stays visible and editable | Title "You can't create environments". Body "Creator access is checked each time you create, and {login} no longer has it. Ask an admin to add you to {creator teams}, then try again. This attempt was recorded in the audit log." {teams} rendered mono from API; if none provided omit the sentence about teams. Includes trace ID + copy. |
| 422 repo not allowed (`repo_not_allowed`) | Field error on Repository + summary | "{org/name} isn't an allowed repository. Choose one from the list." |
| ALLOWED_REPOS empty (fail closed) | Alert (info) replacing the form body; form disabled | Title "No repositories are available". Body "An admin must add allowed repositories before environments can be created." |

**Field error copy (per code)**
| Code | Field | Copy |
|------|-------|------|
| `ref_not_found` | Ref | "{ref} wasn't found in {org/name}. Check the {branch name \| pull request number \| commit SHA} and try again." |
| `ref_invalid` | Ref | "Enter a valid {branch name \| pull request number \| commit SHA}." |
| `pr_not_eligible` (fork PR not allowed) | Ref | "Pull requests from forks can't be previewed." |
| `image_not_found` | Ref | "No image was found for {sha7} in {org/name}. Wait for the repository's CI to publish ghcr.io/{org/name}:{sha7}, then try again." |
| `template_required` / `size_required` | Template / Size | "Select a template." / "Select a size." |
| `schema_violation` | option field | Server message verbatim if present, else "This value isn't valid for the {template} template." |
| `schema_violation` (unmapped) | summary only | "{path}: {message}" |

### 4. Environment detail and creation progress (`/environments/$envId`)
- PageHeading: "Environment {shortId}" (mono ID inside heading text), status badge to its right (not inside the h1). Reached after Create and from the launch page.
- Stack, gap lg:
  1. **Progress card.** An ordered list (`<ol>`) of three steps: Requested, Provisioning, Running. Each step shows its icon (check-circle-2 for completed, current state icon for current, circle outline `circle` for not reached), Label-role name, and for current/completed steps a Body muted detail ("Request accepted 14:03:11", "Waiting for the Actions run", "Preview responded at 14:04:02"). Current step has `aria-current="step"`. If failed, the failed step shows x-circle + "Failed" in destructive color and later steps stay unreached; a Body paragraph below shows the failure reason from the API, then the Actions run link and trace.
  2. **Result card** (Running only): row with mono preview URL (`{env-id}.preview.localhost`, full `http://` URL in a link), accent "Open preview" button (external-link icon, `target="_blank"`, `rel="noopener noreferrer"`), Body muted below: "Opening a preview checks that you're signed in and allowed. You may see a short redirect through Ephematool." Before Running the card is replaced by the muted line "The preview URL appears here once the environment is running." (not a disabled button).
  3. **Details card:** two-column definition list (stack <768px): Owner (avatar 24px + login), Repository, Ref (`branch main` / `PR #123` / `SHA`, plus resolved `@{sha7}` mono with full SHA in `title`), Template, Size (`M · 1 vCPU · 1 GiB`), Created (absolute local time; relative in `title`), **Expires at** (absolute local time; Body muted helper "Informational. Automatic cleanup isn't enforced yet." exact wording, so nobody assumes TTL enforcement).
  4. **Trace and run card:** Trace ID (mono, full value) with icon-only copy button (`aria-label="Copy trace ID"`, tooltip toggling to "Trace ID copied" for 2s, `aria-live="polite"`, exactly as Phase 1) and an `outline` button "View trace in Grafana" (external-link icon; URL from API Grafana base). Actions run: `outline` button "View Actions run" (external-link icon) once the run URL is known; before that, muted "Waiting for the workflow run to start." The trace card is shown in all states, not only on error, because the single trace from the Create click is the point of OBS-02.
- **Polling:** while status is Requested or Provisioning the page polls `GET /api/environments/{id}` every 3s via TanStack Query, in place with no skeleton flash; polling stops at Running or Failed. A failed poll shows "Updated {n}s ago" stale indicator (Phase 1 rule) and continues; it does not retry silently beyond the polling cadence. Status changes are announced with a visually hidden `aria-live="polite"` region: "Status changed to Provisioning", "Environment is running", "Environment failed". Focus is never moved by polling.
- **Loading:** progress card and details card each render skeleton blocks matching final size; the heading renders as soon as the ID is known from the route. **Not found (404):** in-shell, Heading "Environment not found", body "No environment with this ID exists, or you don't have access to it.", link "Back to Environments". **Error:** Phase 1 pattern, "Couldn't load this environment", trace + copy, button "Retry Loading Environment".
- Phase 3 extends this page with destroy, health and drift; this phase only places those in the unused space and adds none now.

### 5. Audit log (`/admin/audit`, admin only, D-19)
- PageHeading "Audit log". Body muted under the heading: "Read-only record of environment creations, denials and rejections. Events can't be edited or removed."
- **Filter bar** (a `form`, wraps below 768px): Outcome (`select`: All outcomes, Succeeded, Denied, Rejected), Action (`select`: All actions, plus the action list from the API, label e.g. "Create environment"), Actor (`input`, placeholder "GitHub login", exact login match), then "Apply filters" (outline button) and "Clear filters" (ghost, shown only when any filter is set). Filters are applied only on submit (Enter or button), are mirrored in URL search params (`?outcome=denied&actor=alice`), and each application is one new traced request. After applying, the result region updates in place and the result count line (Body muted, `aria-live="polite"`) reads "{n} events" or "No events match these filters".
- **Table** (existing `table`), newest first (`occurred_at` descending, fixed; no column sorting). Columns: Time, Outcome, Action, Actor, Environment, Trace.
  - Time: absolute local `YYYY-MM-DD HH:mm:ss` with ISO UTC in `title`.
  - Outcome: status badge (icons above). Denied rows additionally show a 2px destructive left border on the row; the icon, label and the Action reason (below) carry the meaning, so the border is decoration only.
  - Action: label (Body), and for Denied/Rejected a muted second line with the reason: "Not a creator", "Repository not allowed", "Ref not found", "Image not found", "Invalid template options".
  - Actor: avatar 24px + login (mono-free Body).
  - Environment: mono short ID link to `/environments/{id}`; "—" (with `aria-label="No environment"`) when none exists (denials).
  - Trace: mono first 8 characters (full in `title`), icon-only copy button (same copy contract) and icon-only link (`aria-label="Open trace {id} in Grafana"`, tooltip "Open in Grafana", external-link).
- **Paging:** 50 rows per request; a "Load older events" outline button below the table appends the next page (cursor); hidden when no more. No numbered pagination.
- **Loading:** 8 skeleton rows matching table dimensions; header and filters render immediately. **Empty (no events at all):** heading "No audit events yet", body "Creating an environment, or a denied attempt, adds an event here." **Empty (filtered):** heading "No events match these filters", body "Clear filters or widen the actor and outcome." with Clear filters button. **Error:** "Couldn't load the audit log", body "The request to the API failed. Select Retry Loading Audit Log, or look up trace {traceId} in Grafana.", copy button, button "Retry Loading Audit Log".
- **Responsive (<768px):** each event becomes a stacked block (Time and Outcome on top, Action/Actor/Environment below, Trace last); filters stack full width.
- **Non-admin (API 403 or role hint is not admin):** rendered inside the shell, never a blank page or silent redirect. PageHeading "Admins only", Body "The audit log is limited to admins. Ask an admin if you need access." plus an outline link button "Back to Environments". Trace ID shown with copy button when the 403 carries one. The 403 itself is recorded server-side if the API chooses; the UI does not claim it.

### 6. Preview access interstitial and denied (D-04, ENV-09)
Public-style pages outside the shell, same centered card shell as `/login` (max-width 400px, top offset 3xl, padding xl). Routes: `/preview/authorize?env={envId}&return={previewUrl}` (interstitial) and `/preview/denied?env={envId}` (denied). The `return` value is validated server-side against the environment's own preview host; the SPA never navigates to an unvalidated URL. This origin never exposes the platform session to the preview host: the SPA only calls the mint endpoint and then does a full-page `window.location.replace` to the returned preview URL.

- **Signed out:** redirect to `/login` carrying the authorize URL as the post-login return; login page copy is unchanged.
- **Checking (default state on arrival when signed in):** PageHeading Display "Checking your access to this preview". Body "Signing you in to {envId}.preview.localhost…" with a loader-2 icon in a `role="status"` container (static icon under reduced motion). The name of the host is mono. No buttons. This state is not delayed or padded (trace contract); it ends as soon as the mint call returns (success: immediate redirect; failure: next states).
- **Slow (no response after 10 seconds, informational only; request is not cancelled or retried):** adds Body "This is taking longer than expected." and a "Try Access Check Again" outline button (new traced request).
- **Denied (`/preview/denied`, also shown inline when the mint endpoint returns 403 or 404):** PageHeading Display "Access denied". Body "You're signed in as {login}. {login} isn't allowed to open the preview {envId}. Previews are limited to signed-in platform users who are allowed to use Ephematool. This preview may also have been removed." Using one message for denied and nonexistent avoids confirming which environments exist. Buttons (both `outline`, since accent is not reserved for them): "Back to Ephematool" (to `/environments`) and "Try a different account" (signs out, then `/login`). Focus lands on the Display heading. No `alert` role (static page); trace ID shown with copy button when present.
- **Error (5xx / network):** Display "Couldn't check your access". Body "The request to Ephematool failed. Select Try Access Check Again, or look up trace {traceId} in Grafana." copy button + "Try Access Check Again" outline button.
- **Environment not yet running:** if the user opens the preview before it is healthy, ingress shows its own error; the dashboard never links to the preview before Running (screen 4 gating), so no extra page is specified.

---

## Component Inventory

| Component | Source | Notes |
|-----------|--------|-------|
| `StatusBadge` (env state + audit outcome) | app, built on `badge` | Replaces ad hoc badges here; Phase 1 `HealthBadge` untouched |
| `CopyTraceButton` | app, extract from `StatusError` | Same behavior, reused on 6 surfaces; refactor of Phase 1 code, no visual change |
| `ProgressSteps` | app | `<ol>`, `aria-current="step"` |
| `DefinitionList` | app | Reuse BuildCard layout |
| `FormField` (label, control, hint, error wiring) | app | Generates ids and `aria-describedby` |
| `SchemaFields` | app | Implements the supported-constructs table |
| `SizePresetCards` | app, built on `radio-group` | |
| `RoleBadge` | existing | **ADDED** `creator` value |
| `PageHeading` | existing | unchanged; used on every new route (focus on mount) |

---

## Interaction and Loading Contract (Phase 2 additions)

Inherits the full Phase 1 contract. Additions:
- Every API call from the new screens goes through `tracedFetch` so Create click, mint call and polling GETs are linked traces; the Create click starts the trace that the backend continues through the Actions run to the first healthy probe. No client debounce or batching on any of these.
- Polling is allowed only on the detail page (3s, stops at terminal state). Audit and launch pages do not poll; they refresh on filter apply or manual reload.
- Keyboard: form order is Repository, Ref type (arrow keys within the radio group), Ref, Template, Template options, Size (arrow keys), Create, Back to Environments. `Enter` in a text input submits. Select, radio-group and switch use Radix keyboard behavior. Skip no controls; no keyboard traps in the interstitial.
- Focus: route change focuses PageHeading (all new routes); failed submit focuses the first invalid field (only in-page automatic move); the summary links move focus to fields; polling and template-change never steal focus.
- Reduced motion: loader-2 spin and skeleton shimmer are static.

---

## Copywriting Contract

| Element | Copy |
|---------|------|
| Primary CTA (form) | Create environment |
| Primary CTA pending | Creating environment… |
| Launch page action | New environment |
| Back link (create form footer) | Back to Environments |
| Nav labels | Environments / Audit log |
| Form page title | New environment |
| Form group titles | Target / Template / Size |
| Field labels | Repository / Ref type / Ref / Template / Size |
| Ref type options | Branch / Pull request / Commit SHA |
| Template placeholder | Select a template |
| Template options update announcement | Template options updated |
| Omitted schema fields note | Some advanced options aren't available in this form. Chart defaults are used. |
| Expiry line on form | Expires {24 hours} after creation (informational) |
| Viewer notice | You have view-only access. Creating environments requires the Creator role. Ask an admin to add you to a creator team, then sign in again. |
| Error summary title | Fix {n} {field/fields} to create this environment |
| Create request failure | Couldn't create the environment. The request to the API failed. Select Create environment to try again, or look up trace {traceId} in Grafana. |
| Guardrail denial | You can't create environments (full copy in screen 2) |
| Status labels | Requested / Provisioning / Running / Failed |
| Step details | Request accepted {time} / Waiting for the Actions run / Preview responded at {time} |
| Failed heading | Provisioning failed |
| Failed body | {reason from API}. Open the Actions run or look up trace {traceId} in Grafana for details. |
| Preview not ready | The preview URL appears here once the environment is running. |
| Preview button | Open preview |
| Preview note | Opening a preview checks that you're signed in and allowed. You may see a short redirect through Ephematool. |
| Expires-at helper | Informational. Automatic cleanup isn't enforced yet. |
| Trace buttons | Copy trace ID (then "Trace ID copied") / View trace in Grafana |
| Run link | View Actions run / Waiting for the workflow run to start. |
| Launch empty heading | No environments yet |
| Launch empty body (creator/admin) | Create your first preview environment from an allowed repository. |
| Launch empty body (viewer) | Environments you create will appear here. Ask an admin for the Creator role to create one. |
| Launch inventory caption | The full environment inventory arrives in a later phase. |
| Detail not found | Environment not found. No environment with this ID exists, or you don't have access to it. |
| Detail error / retry | Couldn't load this environment / Retry Loading Environment |
| Audit page subline | Read-only record of environment creations, denials and rejections. Events can't be edited or removed. |
| Audit empty heading / body | No audit events yet / Creating an environment, or a denied attempt, adds an event here. |
| Audit filtered-empty | No events match these filters / Clear filters or widen the actor and outcome. |
| Audit error / retry | Couldn't load the audit log / Retry Loading Audit Log |
| Audit load more | Load older events |
| Audit filter buttons | Apply filters / Clear filters |
| Audit non-admin | Admins only. The audit log is limited to admins. Ask an admin if you need access. |
| Interstitial title / body | Checking your access to this preview / Signing you in to {envId}.preview.localhost… |
| Interstitial slow | This is taking longer than expected. (button: Try Access Check Again) |
| Preview denied title / body | Access denied / You're signed in as {login}. {login} isn't allowed to open the preview {envId}. Previews are limited to signed-in platform users who are allowed to use Ephematool. This preview may also have been removed. |
| Preview denied buttons | Back to Ephematool / Try a different account |
| Preview check error | Couldn't check your access. The request to Ephematool failed. Select Try Access Check Again, or look up trace {traceId} in Grafana. |
| Destructive confirmation | none: no destructive actions exist in this phase (destroy is Phase 3). Create is non-destructive and needs no confirmation. |

---

## Registry Safety

| Registry | Blocks Used | Safety Gate |
|----------|-------------|-------------|
| shadcn official | existing: button, card, badge, avatar, dropdown-menu, skeleton, table, tooltip. **ADDED:** input, label, select, radio-group, switch, alert | not required |

No third-party registries declared. Registry vetting gate: not applicable.

---

## Focal Points and Clarifications

Focal point per screen (the first thing the eye and, on route change, focus should land on, as in Phase 1):
- **Create form (`/environments/new`):** the page heading, then the Ref field; the single accent-filled "Create environment" button is the primary action.
- **Environment detail (`/environments/$envId`):** the page heading, then the progress card with its status badge; "Open preview" (accent) appears only when Running.
- **Launch page (`/environments`):** the page heading, then the recent-environments card; "New environment" is an outline button.
- **Audit log (`/admin/audit`):** the page heading, then the Outcome badge column in the table.
- **Preview interstitial and denied pages:** the Display title, then the first action button (Denied and Error states only; the Checking state has no buttons), as in Phase 1's sign-in and denied pages.

Clarifications:
- **60/30/10:** inherited unchanged from the Phase 1 contract (dominant surface 60%, secondary surface 30%, accent 10%, in both light and dark palettes); Phase 2 adds only the two accent-filled primary actions listed in the reserved accent list.
- **Failed heading:** "Provisioning failed" is rendered as the card heading on the environment detail page whenever the state is Failed; the failed step in the 3-step list also shows "Failed", and the API's failure reason appears in the card body.
- **Info Alert surface:** the viewer notice and "No repositories are available" alerts use the secondary surface with foreground text and an info icon; they use no accent and no warning color.
- **Unknown audit outcome:** the "Unknown" badge is a display fallback for unrecognized outcome values; it is not a filter option, and unknown values appear under the "All" filter only.
- **Back link:** "Back to Environments" in the create form footer is a navigation link (ghost button), not a form-dismiss control, so it never discards server state.

---

## Decisions and Assumptions

Pre-populated from upstream (not re-asked): form fields and server-side field errors (D-10..D-12), viewer/creator/admin behavior (D-06, D-09), informational expires-at (D-13), audit page content (D-19), preview gate and interstitial (D-04, ENV-09), states requested/provisioning/running/failed (Claude's discretion list), trace-aware loading/error contract, 4 sizes / 2 weights, palettes.

**User-confirmed (2026-10-12):** items 1 (launch page = the user's 5 most recent environments), 2 (viewers see a visible-but-disabled form with an explanation), 4 (size presets S 0.5 vCPU / 512 MiB, M 1 / 1 GiB, L 2 / 2 GiB, default S) and 7 (one shared denied message for unknown and forbidden previews) below were confirmed by the user. The remaining items are accepted defaults.

**Assumptions (items marked above are confirmed; the rest are defaults):**
1. **Launch page.** `/environments` is a minimal page with the user's 5 most recent environments (not the inventory) so Phase 3 can replace the card in place. Alternative: skip it and have the nav go straight to `/environments/new` plus a "recent" strip.
2. **Viewer experience.** `/environments/new` is visible but disabled with an explanation; the launch page hides the New environment button for viewers.
3. **Accent additions.** Create environment and Open preview join the accent reserved list; "New environment" (launch), "View trace in Grafana" and "View Actions run" are outline buttons.
4. **Size values.** S 0.5 vCPU/512 MiB, M 1 vCPU/1 GiB, L 2 vCPU/2 GiB as namespace limits, shown from API values; default size S.
5. **Schema rendering.** Supported: string, enum, integer/number with min/max, boolean, one-level objects. Anything else is omitted; required-and-unsupported disables the template. No JSON editor. Pattern is server-validated only.
6. **API shape the UI needs (planner/API decision):** 422 `errors[{field,code,message}]`, 403 guardrail with creator teams, `/api/me` role includes `creator`, environment GET with state, failure reason, run URL, resolved SHA, expires-at, trace ID, preview URL; preset values and default TTL in config; audit list with filters (outcome, action, actor) and cursor paging with a reason field; a preview mint endpoint distinguishing allowed/denied.
7. **Interstitial route.** `/preview/authorize` and `/preview/denied` are SPA routes on the dashboard origin; unknown and forbidden environments share one denied message to avoid environment enumeration.
8. **Polling.** Detail page polls every 3s only while non-terminal.
9. **Audit filters** apply on submit, actor is exact login match, newest first only, 50 per page, outcomes Succeeded/Denied/Rejected.
10. **Denied vs Rejected** are distinguished by icon (ban vs circle-alert), text and color (only Denied is destructive); no new color introduced.
11. **No timeout state for Requested/Provisioning** is designed (stuck runs); the timeout threshold and failure transition belong to the worker (poll timeouts are Claude's discretion). The UI just renders Failed with the API reason.
12. Times are shown in the browser's local timezone with UTC in `title`.

---

## Checker Sign-Off

- [x] Dimension 1 Copywriting: PASS
- [x] Dimension 2 Visuals: PASS
- [x] Dimension 3 Color: PASS (FLAG: 60/30/10 stated as inherited)
- [x] Dimension 4 Typography: PASS
- [x] Dimension 5 Spacing: PASS
- [x] Dimension 6 Registry Safety: PASS

**Approval:** approved 2026-10-12
