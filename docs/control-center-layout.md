# Control center adaptive layout contract

SynFactory's operator UI is one Vue application with mobile-first layout behavior. The control plane remains authoritative; viewport adaptation must never duplicate workflow, authorization, retry, merge-gate or repository lifecycle policy in the browser.

## Viewport hierarchy

The base layout targets narrow touch screens down to a 360px-class viewport. Larger breakpoints progressively add density and persistent chrome:

- phone: fixed bottom primary navigation, two-column attention metrics where space permits, single-column cards, local overflow for intrinsically wide tables/log/code, and bottom-sheet detail surfaces;
- small tablet: wider card grids and denser metrics while retaining touch-safe controls;
- tablet/laptop: three-column metrics and larger panels;
- desktop: persistent left navigation, right-side detail drawer and denser six-column overview metrics.

Do not fork mobile and desktop applications and do not select layout with user-agent detection. Preserve the current logical view and selected detail when the viewport changes whenever the underlying data remains valid.

## Mobile interaction requirements

- primary navigation and routine actions have a practical minimum 44px touch target;
- phone navigation respects `env(safe-area-inset-bottom)` and remains within thumb reach;
- content and overlays use dynamic viewport units where appropriate so browser chrome and virtual keyboards do not make controls unreachable;
- the page itself must not horizontally scroll at phone widths; intrinsically wide tables, terminal output, logs and code may scroll inside a bounded local container;
- identifiers and errors wrap or truncate inside their owning component instead of expanding the page width;
- detail surfaces become bottom sheets on phones and right-side drawers on desktop;
- hover is enhancement only. Every supported operator action remains available through click/touch/focus interaction.

## Attention hierarchy

On constrained screens, show operational risk before secondary metadata. Blocked/parked/failing workflows, unhealthy workers, stale reconciliation and exhausted repair budgets have higher visual priority than descriptive fields that can be reached through detail drill-down.

## Accessibility and motion

Keyboard focus remains visible at all viewport sizes. Desktop keyboard navigation must not regress when touch behavior is added. Honor `prefers-reduced-motion` and avoid interaction that depends only on animation, hover or precise pointer input.

## Terminal integration

Issue #29 terminal UI must reuse this shell rather than creating a separate desktop-oriented page. On phones the terminal owns the available content viewport above the bottom navigation, resizes when the visual viewport/virtual keyboard changes, and keeps session/reconnect/close controls touch-safe. Horizontal scrolling is allowed inside terminal output when required by terminal semantics, never at page level.

## Verification

Every significant control-center change should at minimum preserve production TypeScript/Vite build success. Where automated browser coverage is introduced, representative viewport contracts should include a 360px-class phone, tablet and desktop width and assert that primary navigation/actions remain reachable with no page-level horizontal overflow.
