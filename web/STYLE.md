# Web style guide

The web app is one person's place for the things that follow them from device to device: contacts, calendar, mail, chats, files and
the browser with its tabs. That is personal data, used many times a day, mostly on a phone. The interface should feel like a calm
tool that is already on your side, not like an app that wants attention.

Everything here is implemented: the tokens are in `src/css/tokens.css`, the global rules in `src/css/app.css`, the name and logo in
`src/brand.js`, the sections in `src/nav.js`. If this document and the code disagree, fix one of them in the same change.

## 1. Principles

1. **Calm.** One accent colour, neutral surfaces, no decoration that carries no meaning. No badges that count unread items for the sake
   of counting, no animation that loops, no sounds. Status is stated once, in words.
2. **Content first.** The person's mail, messages and pages are the interface. Chrome (bars, borders, shadows) stays quiet and recedes in
   dark mode. A list row shows what the item is, not what the app could do with it.
3. **Phone first.** Design the 390 px layout first, then widen it. Anything that works with one thumb on a phone works on a desktop; the
   reverse is not true. Nothing relies on hover.
4. **Touch targets of at least 44 x 44 css px**, for every control, with at least 8 px between neighbours. A smaller icon is fine if its
   target is 44 px.
5. **Contrast to WCAG 2.2 AA at least:** 4.5:1 for text, 3:1 for control outlines, icons that carry meaning and focus rings. Information is
   never carried by colour alone (a status has an icon and a word).
6. **Keyboard and assistive technology work everywhere.** Every action is reachable by keyboard in a sensible order, with a visible
   focus ring (never removed), a "Skip to content" link, landmarks (`header`, `nav`, `main`), real buttons and links, labels on every
   control, `role="alert"` for errors and `role="status"` for progress.
7. **Reduced motion is respected.** Motion only explains a change (a panel arriving, a row leaving); under `prefers-reduced-motion` all
   durations collapse to nothing.
8. **Plain words** (section 8), short sentences, the person's own terms.
9. **Private by default.** Nothing about the content of a person's data appears in a page title, a notification preview or a screenshot
   thumbnail without their say-so. Device keys stay on the device (never in `localStorage`).

## 2. Brand token

The product name is undecided (candidates: Cloud Companion, Cloud Pal, Porter). **The name and the logo exist in one file,
`src/brand.js`.** It holds `name`, `short`, `tagline`, the icon colours and `logoPath` (a 64 x 64 path drawn in `currentColor`). The build
reads it for the page title, the web app manifest and the app icon (`vite.config.js`); the app bar, the pairing page and the document title
read it at run time. To rename or re-logo the product: edit that file, rebuild. Do not write the product name anywhere else, in code,
copy or tests; refer to "the app" in prose and use `brand.name` in code. (The Go server and the phone clients have their own names:
this token is for the web app only.)

## 3. Design tokens

All values are CSS custom properties on `:root` (light) and `body.body--dark` (dark). Components use tokens, never literals. Quasar's brand
variables (`--q-primary` and friends) are mapped onto them, so Quasar components follow the theme.

### 3.1 Colour

One accent, a neutral ramp, four status colours. Ratios are WCAG contrast ratios, computed from the values below.

| token | light | dark | used for |
|---|---|---|---|
| `--ph-bg` | `#f6f7f8` | `#101317` | page background |
| `--ph-surface` | `#ffffff` | `#181c22` | app bar, cards, dialogs, inputs |
| `--ph-surface-2` | `#eceff2` | `#222831` | chips, selected rows, notes |
| `--ph-divider` | `#d9dde2` | `#2c333c` | hairlines between rows (decoration only) |
| `--ph-border` | `#7a838e` | `#6b7480` | outlines of controls |
| `--ph-text` | `#1a1d21` | `#e8eaed` | body text |
| `--ph-text-muted` | `#4a525c` | `#aab1bb` | secondary text, captions |
| `--ph-accent` | `#0b5cad` | `#8ab4f8` | links, primary buttons, the selected section |
| `--ph-on-accent` | `#ffffff` | `#0b1220` | text on an accent fill |
| `--ph-accent-soft` | `#e4effa` | `#1f2d42` | selected navigation item, active tab |
| `--ph-positive` | `#17663a` | `#7bd9a0` | success, "Connected" |
| `--ph-warning` | `#8a4b00` | `#f2c36b` | caution |
| `--ph-negative` | `#b3261e` | `#f2a19b` | errors, destructive actions |

Contrast (light / dark):

| pair | light | dark |
|---|---|---|
| text on page / on surface | 15.8 / 16.9 | 15.5 / 14.2 |
| muted text on page / on surface / on surface-2 | 7.4 / 7.9 / 6.9 | 8.6 / 7.9 / 6.9 |
| accent text on surface | 6.7 | 8.1 |
| accent text on accent-soft | 5.7 | 6.6 |
| on-accent text on accent | 6.7 | 8.9 |
| positive / warning / negative text on surface | 7.0 / 6.8 / 6.5 | 10.0 / 10.4 / 8.4 |
| control border on surface / on page | 3.8 / 3.6 | 3.6 / 3.9 |

Rules: dark mode is not an inversion; surfaces get lighter as they rise, and shadows give way to hairlines. Do not add a colour: if a new
meaning is needed, use an icon and a word with an existing colour. Text over a page image or tile always sits on a surface.

### 3.2 Type

System font stack (no web font to download, native feel on every platform, good for privacy and speed):
`system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, "Noto Sans", sans-serif`; monospace for ids and code:
`ui-monospace, SFMono-Regular, Menlo, Consolas, monospace`.

| token | size | use |
|---|---|---|
| `--ph-text-caption` | 13 px | tab labels in the bottom bar, timestamps |
| `--ph-text-small` | 14 px | secondary lines in lists, hints |
| `--ph-text-body` | 16 px | all reading and typing (never smaller: phones zoom into smaller inputs) |
| `--ph-text-title-s` | 18 px | app name, card headings |
| `--ph-text-title` | 22 px | section headings (`h2`) |
| `--ph-text-headline` | 28 px | the page heading (`h1`, one per page) |

Line height 1.5 for text, 1.25 for headings. Weight 400 for text, 600 for headings and the selected item; no other weights. Sentence case
everywhere. Respect the user's browser font size (sizes are in `rem`; layouts must survive 200 % zoom).

### 3.3 Space, shape, elevation, motion

* **Space** (4 px grid): `--ph-s1` 4, `s2` 8, `s3` 12, `s4` 16, `s6` 24, `s8` 32, `s12` 48. Page padding is 16 px (12 px on a phone). Related
  things sit 8 px apart, groups 16 px, sections 24 px. `--ph-tap` is 44 px.
* **Radii:** controls 8 px, cards and dialogs 12 px, bottom sheets 20 px at the top corners, pills and avatars fully round.
* **Elevation:** 0 flat (bars, lists), 1 cards (a hairline and a faint shadow), 2 menus, 3 dialogs and sheets. In dark mode elevation is a
  hairline plus a lighter surface.
* **Motion:** 120 ms for hover and press, 200 ms for panels and dialogs, 300 ms for page-level changes; easing `cubic-bezier(0.2, 0, 0, 1)`.
  Nothing loops except a progress spinner, and the spinner has text for assistive technology. Under `prefers-reduced-motion` the durations
  are 0.01 ms.

## 4. Layout and the app shell

Content width is at most 960 px, centred; the web page itself (the cloud browser canvas) is 360 px wide by design.

| width | navigation | notes |
|---|---|---|
| under 600 px (phone) | slim top bar + **bottom bar** with the four primary sections and "More" | thumb reach; "More" opens a bottom sheet |
| 600 to 1023 px (tablet) | top bar + **icon rail** (72 px) with tooltips | labels on hover and as `aria-label` |
| 1024 px and up (desktop) | top bar + **labelled drawer** (240 px) | |

The sections, in order: **Browser, Chats, Mail, Calendar** (primary: bottom bar on a phone), **Contacts, Files, Devices** (admin only),
**Settings** (under More on a phone). They are defined once in `src/nav.js`; adding a section is one line there plus one route. A
section that does not exist yet shows the empty state with one sentence about what will live there and a way back; it never shows a
disabled copy of the future UI.

The top bar holds only the product mark and name and the connection status ("Connected", "Connecting", "Offline": an icon and a word).
Page-level actions live in the page, not in the bar. Every page has exactly one `h1`.

## 5. Components

**Buttons.** One primary (filled accent) button per view at most; secondary buttons are outlined or flat. Labels are verbs ("Connect",
"Save", "Sign out"), never "OK" unless it answers a dialog that only informs. Destructive actions are never the primary style and are
confirmed once, in words that name what happens. Icon-only buttons have an `aria-label` and a tooltip.

**Address bar and navigation controls (browser).** One row: back, forward, reload (44 px round, flat), then the address field (outlined, 44 px
high, flexible), with the loading spinner inside the field's end. The page title is shown once below the row, in muted small text; the
zoom controls sit under the page with the current scale in words ("150%"). The field accepts a web address or search words.

**Tab strip.** Up to three tabs of one device share one connection. Each tab is a 44 px high button with the page title (ellipsis) and a
44 px close button; the active tab has the accent outline and the soft fill. A "+" button adds a tab and is disabled, with its tooltip
saying why, at the limit. Tabs are `role="tab"` inside a `role="tablist"`.

**Dialogs.** For decisions that block the person (a page's alert, a sign-in, a certificate problem, picking an option). A title in the
form of what is happening ("This page says", "Sign in to continue", "This connection is not secure"), the message, then actions on the
right: the safe choice as a flat button on the left ("Cancel", "Go back"), the primary on the right ("Sign in", "Continue anyway" for
a certificate error only after the explanation). Focus moves into the dialog and returns afterwards; `Escape` cancels when cancelling is
safe. A dialog is never used for a success message.

**Lists.** A row is at least 44 px high, shows a primary line (what it is) and one muted secondary line (when, where, status), and has at
most one trailing action. Rows are separated by hairlines inside a card. The selected row uses the soft accent fill and the accent text,
and `aria-current`. Long lines wrap or ellipsise; never clip silently.

**Forms.** Outlined fields, labels above (not placeholder-only), hints below, errors below in the negative colour with an icon and a
sentence that says what to do. One column. Autocomplete and `inputmode` attributes are set; inputs are 16 px so phones do not zoom.

**Empty, loading and error states** share one layout (`EmptyState.vue`): an icon, a heading, one sentence, at most one action.
* Empty: say what will be here and how it gets here ("Your contacts will appear here, merged from all your accounts and devices.").
* Loading: a spinner and what is loading ("Loading devices"); content that already exists stays visible while it refreshes.
* Error: say what happened and what to do ("Could not reach the server. Check your network and try again."), with the retry action.

**Notices and toasts.** A notice that needs action is an inline banner (`role="alert"`) at the top of the content with a "Dismiss" or a
fix. A toast is only for the result of something the person just did ("Saved"), appears at the bottom above the bar for 4 seconds, can be
reached by keyboard, and offers "Undo" when the action can be undone. Errors are never toasts.

**Status indicators.** An icon, a word, a colour; never colour alone. Connection state is in the top bar; per-item states (a tab is loading,
a device is signed out) are text badges.

## 6. Icons

**Material Symbols (outlined), Apache-2.0**, from `@quasar/extras/material-symbols-outlined`, used through Quasar's icon set (names start
with `sym_o_`). One style only: outlined, weight 400. Sizes 20 px inline with text, 24 px in bars and buttons, 48 px in empty states.
An icon is paired with a text label, or has an `aria-label`; purely decorative icons are `aria-hidden`. Used today: `language`, `chat`, `mail`,
`calendar_month`, `contacts`, `folder`, `devices`, `settings`, `more_horiz`, `arrow_back`, `arrow_forward`, `refresh`, `add`, `close`, `zoom_in`,
`zoom_out`, `cloud_done`, `cloud_off`, `sync`, `error`, `download`, `lock`, `check_circle`, `inbox`, `contrast`, `light_mode`, `dark_mode`. The font is
shipped whole today; subset it before a production release.

## 7. Dark mode

Follows the system by default. Settings > Appearance offers **Match system, Light, Dark**; the choice is stored on the device
(`localStorage`, key `phecloud-theme`) and applied before the first paint of the app. Both themes are tested at 390 px and 1280 px.
Images and the page canvas are never recoloured; the cloud page keeps its own colours inside its frame.

## 8. Writing style for UI text

* **Plain words, short sentences.** "Connect this browser", not "Authenticate device". Say "device", "code", "sign out"; avoid "token",
  "session", "revoke", "endpoint", "IndexedDB".
* **Sentence case** for everything (buttons, headings, tabs). No ALL CAPS, no trailing full stop on a button or a label, full stops in sentences.
* **Verbs on buttons** that say what will happen: "Save", "Sign out", "Continue anyway", "Back to the browser".
* **Say what happened, then what to do** in errors. Never blame the person; never show a raw error text without a sentence around it.
  "That code did not work. It may have been used already or expired. Ask your server for a new one."
* **Be specific about privacy and permanence:** "Signing out removes this device. It will need a new code to connect again."
* **No exclamation marks, no emoji, no cute voice, no "Oops".** No marketing language inside the app.
* **Numbers and dates** in the person's locale; relative times only where they stay correct ("Last seen 3 min ago" refreshes; otherwise
  show the date).
* **Placeholders are hints, not labels**; every field has a visible label.

## 9. Accessibility checklist (for every change)

1. Tab through the page: the order matches the visual order, every control gets the focus ring, nothing traps focus except an open dialog.
2. Every control has a name (visible text, `aria-label` or a `<label>`); icons that stand alone have `aria-label`.
3. Check both themes at 390 px and at 1280 px, and at 200 % zoom: no horizontal scroll (the 360 px canvas aside), no clipped text.
4. Contrast: use only token pairs from section 3.1, or compute the new pair (4.5:1 text, 3:1 outlines).
5. Targets are 44 x 44 px or more (`--ph-tap`).
6. With `prefers-reduced-motion: reduce` nothing moves.
7. Errors are announced (`role="alert"`), progress is announced (`role="status"`).
8. Run `sh web/test/run.sh` (behaviour) and `sh web/test/shots.sh after` (look) and read the screenshots.

## 10. Changing things

* **Rename or re-logo the product:** `src/brand.js`, rebuild.
* **Add a section:** `src/nav.js` (label, icon, path, `primary` if it belongs in the phone's bottom bar), a route in `src/router.js`, a page
  in `src/pages/`; until the page exists the `SectionPage` placeholder shows the empty state.
* **Retune a colour:** change the token in `src/css/tokens.css` in both themes and update the contrast table here with the computed ratios.
* **New component:** build it from tokens, give it the states above (hover, focus, disabled, loading, error), test it in both themes.
