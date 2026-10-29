# Web style guide

The web app is one person's place for the things that follow them from device to device: contacts, calendar, mail, chats, files and
the browser with its tabs. That is personal data, used many times a day, mostly on a phone. The interface should feel like a calm
tool that is already on your side, not like an app that wants attention.

The look is the **Etesian design language**: the Nokia N9's (light type, lit squircle icons, dark chrome over light pages), carried to
the web. Its tokens, base styles and Quasar theme are vendored in `src/css/etesian/` (MIT, from the etesian-design repo, never edited
here); the app's own tokens in `src/css/tokens.css` point at them, the global rules are in `src/css/app.css`, the name and logo in
`src/brand.ts`, the sections in `src/nav.ts`, the desktop view in `src/desktop/`. If this document and the code disagree, fix one of them
in the same change.

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
`src/brand.ts`.** It holds `name`, `short`, `tagline`, the icon colours and `logoPath` (a 64 x 64 path drawn in `currentColor`). The build
reads it for the page title, the web app manifest and the app icon (`vite.config.js`); the app bar, the pairing page and the document title
read it at run time. To rename or re-logo the product: edit that file, rebuild. Do not write the product name anywhere else, in code,
copy or tests; refer to "the app" in prose and use `brand.name` in code. (The Go server and the phone clients have their own names:
this token is for the web app only.)

## 3. Design tokens

All values are CSS custom properties on `:root` (light) and `body.body--dark` (dark). Components use tokens, never literals. Quasar's brand
variables (`--q-primary` and friends) are mapped onto them, so Quasar components follow the theme.

### 3.1 Colour

One accent, the Etesian neutral ramp, four status colours. Surfaces, text and outlines come from the Etesian tokens (`--hd-c-*`), so they
follow the colour mode (section 7); the accent and status colours are the app's own, because the default Etesian blue (`#4591FF`) is
only 3:1 under white text. Each section also has its N9 application colour (`nav.ts`), used only on its icon tile, never for text.
Ratios are WCAG contrast ratios, computed from the values below.

| token | hybrid (light) | dark | used for |
|---|---|---|---|
| `--ph-bg` | `#E0E1E2` | `#000000` | page background |
| `--ph-surface` | `#FFFFFF` | `#161616` | cards, dialogs, inputs |
| `--ph-surface-2` | `#F2F3F4` | `#222222` | chips, selected rows, notes |
| `--ph-divider` | black 12 % | white 16 % | hairlines between rows (decoration only) |
| `--ph-border` | `#6A6A6A` | `#A0A0A0` | outlines of controls |
| `--ph-text` | `#282828` | `#FFFFFF` | body text |
| `--ph-text-muted` | `#505050` | `#C8C8C8` | secondary text, captions |
| `--ph-accent` | `#1C51AF` | `#7FB0FF` | links, primary buttons, the selected section (the N9 link blue) |
| `--ph-on-accent` | `#FFFFFF` | `#0B1220` | text on an accent fill |
| `--ph-accent-soft` | accent 12 % on surface | accent 18 % on surface | selected navigation item, active tab |
| `--ph-positive` | `#2E6B00` | `#8FD65A` | success, "Connected" |
| `--ph-warning` | `#8A4B00` | `#F2C36B` | caution |
| `--ph-negative` | `#B3191A` | `#FF8A80` | errors, destructive actions |
| `--ph-chrome` | `#3A3A3A` to `#141414` | same | the N9 toolbar: phone bottom bar, window header bars |

Contrast (hybrid / dark):

| pair | hybrid | dark |
|---|---|---|
| text on page / on surface | 11.3 / 14.7 | 21.0 / 18.1 |
| muted text on page / on surface / on surface-2 | 6.2 / 8.1 / 7.3 | 12.6 / 10.8 / 9.5 |
| accent text on surface | 7.4 | 8.2 |
| accent text on accent-soft | 6.1 | 5.9 |
| on-accent text on accent | 7.4 | 8.5 |
| positive / warning / negative text on surface | 6.5 / 6.8 / 6.8 | 10.3 / 11.0 / 7.9 |
| control border on surface / on page | 5.4 / 4.1 | 6.9 / 8.0 |
| muted chrome text (`#C8C8C8`) on the toolbar, darkest / lightest | 11.0 / 6.8 | same |

Rules: dark mode is not an inversion; surfaces get lighter as they rise, and shadows give way to hairlines. Do not add a colour: if a new
meaning is needed, use an icon and a word with an existing colour. Text over a page image or tile always sits on a surface. Section
colours appear only as lit squircle tiles (`SectionTile.vue`), with the label beside them carrying the meaning.

### 3.2 Type

**Nokia Pure Text** when the person has it installed (it is proprietary: referenced with `local()` only, never shipped), otherwise
**Fira Sans** (OFL-1.1, from `@fontsource/fira-sans`, latin, weights 300/400/700, about 70 KB), which the Etesian design repo measured as
the closest free look-alike and size-adjusts to Nokia Pure's cap height (`src/css/etesian/fonts.css`). The fonts are served by this server;
nothing is fetched from a font service. Monospace for ids and code: `ui-monospace, SFMono-Regular, Menlo, Consolas, monospace`.

| token | size | use |
|---|---|---|
| `--ph-text-caption` | 12 px | tab labels in the bottom bar, timestamps |
| `--ph-text-small` | 14 px | secondary lines in lists, hints |
| `--ph-text-body` | 16 px | all reading and typing (never smaller: phones zoom into smaller inputs) |
| `--ph-text-title-s` | 19 px | card headings (`h3`) |
| `--ph-text-title` | 24 px | section headings (`h2`) |
| `--ph-text-headline` | 32 px | the page heading (`h1`, one per page) |

The N9 voice: text and headings in the **light** weight (300), big light page headers; regular (400) for small headings, menus and
buttons; bold (700) only for emphasis, the brand in the top bar and the current item in the bottom bar. Line height 1.5 for text, 1.2 for
headings. Sentence case everywhere (the brand in the top bar is the one place in capitals). Respect the user's browser font size (sizes
are in `rem`; layouts must survive 200 % zoom).

### 3.3 Space, shape, elevation, motion

* **Space** (4 px grid): `--ph-s1` 4, `s2` 8, `s3` 12, `s4` 16, `s6` 24, `s8` 32, `s12` 48. Page padding is 16 px (12 px on a phone). Related
  things sit 8 px apart, groups 16 px, sections 24 px. `--ph-tap` is 44 px.
* **Radii and shapes:** controls 6 px, cards and dialogs 8 px (Etesian), bottom sheets 16 px at the top corners, pills fully round.
  **Squircles, not rounded squares or circles,** for every thumb-sized object: section tiles, avatars, icon buttons and their pressed
  highlight (`.hd-squircle`, `.hd-sq`, the N9 superellipse mask).
* **Elevation:** N9 surfaces are flat: cards are a surface and a hairline, no shadow; no ripples. Only overlays cast a shadow (menus,
  dialogs, the launcher, desktop windows). In dark mode elevation is a hairline plus a lighter surface.
* **Motion:** 130 ms for hover and press, 200 ms for panels and dialogs, 350 ms for page-level changes; Etesian ease-out.
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
**Settings** (under More on a phone). They are defined once in `src/nav.ts`; adding a section is one line there plus one route. A
section that does not exist yet shows the empty state with one sentence about what will live there and a way back; it never shows a
disabled copy of the future UI.

The top bar is the Etesian site header: the product mark and the name in bold capitals on the page colour, the connection status
("Connected", "Connecting", "Offline": an icon and a word) and, from 1024 px, "Desktop view". Page-level actions live in the page, not in the
bar. Every page has exactly one `h1`. On a phone the bottom bar is the N9 toolbar: the dark chrome gradient, icons with small labels,
the current section in white and bold. In the drawer and the "More" sheet each section has its lit squircle tile.

### 4.1 The desktop view

From 1024 px the app can also be used as a desktop, as if the server were a computer you sit at (`src/desktop/`, route `#/desktop`).
"Desktop view" in the top bar enters it; View > Classic view leaves it; Settings > Appearance > Layout switches either way. The choice and the window arrangement are stored on the device
(`localStorage`: `phecloud-layout`, `phecloud-desktop`), so "/" reopens the desktop; below 1024 px the classic layout is always used.
It follows the approved Etesian desktop design:

* **Menu bar** (top, 28 px, black): the focused window's name in bold and its menus (Window: minimise, maximise, snap left / right,
  close, the open windows; View: launcher, classic view) on the left; the connection status and the date and time on the right.
* **Dock** (left, floating, icons only): the launcher first, a divider, then every section as its lit tile. A short bar marks an open
  section, a longer white bar the focused one. Clicking opens a section, brings it forward, or minimises it if it is already in front.
* **Windows** overlap; each has one 44 px header bar of N9 chrome (the section tile, its name in light type, then minimise, maximise
  and close, separated by a hairline; close turns red on hover; inactive windows show their bar at half strength). The page inside is the
  same page as in the classic layout, without its `h1` on screen (the bar names it; the heading stays for assistive technology).
* **Snapping:** drag a window's bar to the left or right edge for that half, to the top edge to maximise (the zone shows while dragging);
  double-click the bar to maximise or restore; any edge or corner resizes a floating window. Keyboard: Super or Ctrl+Alt with Left /
  Right (snap to that half), Up (maximise), Down (float again, or minimise a floating window).
* **Launcher:** Super alone or the dock's first button: type to filter the sections, Enter opens the first, Escape closes.
* **Quick switcher:** hold Super (or Ctrl+Alt, where the system keeps Super for itself) and press Tab: the open windows, most recent
  first, with the previous one selected; Tab again (Shift+Tab back) moves on, letting go switches, Escape cancels.
* **Events** (the N9 Events screen as a panel at the right edge): click the clock, or Super+N / Ctrl+Alt+N, or View > Events. The date,
  today's and tomorrow's calendar, unread mail and the three latest chats; each row opens its section. A service that is not set up says
  so in one muted line. A dot on the clock marks unread mail (checked every two minutes). Escape, the clock again or a click on a window
  closes it.
* **Widgets** (after the Etesian Belle widgets; `src/desktop/DesktopWidgets.vue`, kinds and sizes in `widgets.ts`): on the wallpaper under
  every window, on a 90 px grid. Big clock, analog clock (both in the menu bar's time format), unread mail, today's agenda, latest chats;
  each is a Belle panel (dark glass, 12 px corners) headed by the section's lit squircle, and a row opens that section as a window.
  View > Show desktop (Super+D / Ctrl+Alt+D) minimises the windows. View > Edit widgets puts the windows aside and shows the phone's
  edit chrome (accent outline, red ✕ squircle) with the Add widgets sheet; drag to move, or focus a widget and use the arrow keys;
  Delete removes; Done or Esc ends. No long-press and no context menu. The arrangement is kept on this device. A feeds (RSS) widget
  waits for RSS support on the server.
* **Android** is a phone, so its window opens tall and narrow at full height, and the screen fits the window's height (the window body is
  a size container; the page sizes the screen with `cqh`, which is the viewport outside a window). In a window and on a phone its
  controls are one row of icons, each named for screen readers and on hover.
* A link from one section to another opens the other as a window instead of leaving the desktop.

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

**Notices and toasts** (toasts: `src/toast.ts` + `components/ToastHost.vue`; `toast(message, { undo, onExpire })`). A notice that needs action is an inline banner (`role="alert"`) at the top of the content with a "Dismiss" or a
fix. A toast is only for the result of something the person just did ("Saved"), appears at the bottom above the bar for 4 seconds, can be
reached by keyboard, and offers "Undo" when the action can be undone. Errors are never toasts.

**Status indicators.** An icon, a word, a colour; never colour alone. Connection state is in the top bar; per-item states (a tab is loading,
a device is signed out) are text badges.

## 6. Icons

**Material Symbols (outlined), Apache-2.0**, from `@quasar/extras/material-symbols-outlined`, used through Quasar's icon set (names start
with `sym_o_`). One style only: outlined, weight 400. Sizes 20 px inline with text, 24 px in bars and buttons, 48 px in empty states.
An icon is paired with a text label, or has an `aria-label`; purely decorative icons are `aria-hidden`. Used today: `language`, `chat`, `mail`,
`calendar_month`, `contacts`, `folder`, `devices`, `settings`, `more_horiz`, `arrow_back`, `arrow_forward`, `refresh`, `add`, `close`, `zoom_in`,
`zoom_out`, `cloud_done`, `cloud_off`, `sync`, `error`, `download`, `lock`, `check_circle`, `inbox`, `contrast`, `light_mode`, `dark_mode`, `search`, `call`,
`location_on`, `chevron_left`, `chevron_right`, and for the desktop view `desktop_windows`, `apps`, `minimize`, `crop_square`, `filter_none`, `web`, `refresh`. The font is subset to exactly the icons found in `src/` (and Quasar's own icon set) by
`tools/subset-icons.py`, run by the Docker build: 6.5 KB instead of 3.6 MB. Add an icon by using its `sym_o_` name; the next build includes it. (The
script freezes the variable axes to regular weight, no fill, 24 px.)

## 7. Dark mode

Follows the system by default. Settings > Appearance offers **Match system, Light, Dark**; the choice is stored on the device
(`localStorage`, key `phecloud-theme`). Light is the Etesian **hybrid** mode, the N9 look: light pages under dark chrome; dark is the
Etesian dark mode. `theme.ts` sets `data-hd-mode` on `<html>` (the tokens follow it) and the `hd-quasar` class on `<body>` (the Quasar
theme); before the script runs, the hybrid colours apply from `:root`. Both themes are tested at 390 px and 1280 px.
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

* **Code is TypeScript, strict.** `npm run check` type-checks the app (`vue-tsc`), and `npm run build` (the Docker build too)
  refuses to build with a type error. The protocol's frames and the hub's objects are typed in `src/proto.ts`, which follows
  `internal/proto/proto.go`: change both together. `src/android/stream.js` stays JavaScript (the Android bridge serves the same
  file to browsers) and is described by `stream.d.ts` next to it.
* **Rename or re-logo the product:** `src/brand.ts`, rebuild.
* **Add a section:** `src/nav.ts` (label, icon, path, `primary` if it belongs in the phone's bottom bar), a route in `src/router.ts`, a page
  in `src/pages/`; until the page exists the `SectionPage` placeholder shows the empty state.
* **Retune a colour:** change the token in `src/css/tokens.css` in both modes and update the contrast table here with the computed ratios.
* **Update the Etesian design files:** copy `src/styles/tokens.css`, `src/styles/base.css` and `src/quasar/etesian-quasar.css` (as
  `quasar-theme.css`) from the etesian-design repo into `src/css/etesian/`, keeping the header that names the commit; never edit them here.
  App-specific changes go in `src/css/tokens.css` and `src/css/app.css`.
* **New component:** build it from tokens, give it the states above (hover, focus, disabled, loading, error), test it in both themes.
