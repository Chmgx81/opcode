# Site and docs redesign: editorial paper

## Goal

Rebuild `site/` (landing, docs, and the missing legal pages) so it stops
reading as generated marketing and starts reading as a technical manual
for a tool that ships as one binary. Every item on the "30 reasons your
site looks vibe-coded" list is treated as a requirement, not a hint.

## Non-Goals

- No change to Go code, the TUI, or the product's own palette.
- No new framework, no CMS, no analytics, no animation library.
- No third-party component library. `/home/chmgx81/Documents/UI Components`
  is read for ideas only (Amicro's loader patterns for the one real
  loading state); nothing is installed from it.
- No testimonials, logos of users, or pricing. We have none, and
  inventing them is the point the list is making.

## Approach

### The 30 tells, and what replaces each

| Tell | What we do |
|---|---|
| Harsh gradients | None. Flat fills only: paper, one recessed paper, one ink band. |
| Lucide icons | `lucide-react` removed from `package.json`. Hand-written inline SVG and typographic marks only, used three times total. |
| Pure white background | Ground is `#f3f0e8`, a warm paper. |
| Rainbow coloring | Two hues: ink and one burnt red. Semantic colour appears only inside a real diff. |
| Drop shadows | Zero `shadow-*` in the codebase. Depth is hairlines. |
| 3 feature cards in a row | Ruled lists and tables. No grid of equal boxes anywhere. |
| Emojis | No emoji, no `✓`, no `❯` in site copy. Product output keeps its own glyphs, verbatim. |
| Liquid glass | No `backdrop-filter`, no blur, no translucency. |
| Em dashes | Removed from all authored copy (landing, docs, legal, footer). Commas, colons, periods. Product captures are quoted verbatim. |
| Inter/Geist/Space Grotesk | Source Serif 4 Variable for prose, IBM Plex Mono for every label, key, datum and command. Both self-hosted via Fontsource. |
| Colored left stripe | None. Section numbers sit in the margin, in ink. |
| Fake testimonials | None. |
| Bento grids | None. Asymmetric editorial spreads with a numbered margin column. |
| Terminal window | No traffic lights, no window chrome, no rounded frame. Figures are real captured text in a ruled box with a `Fig. n.` caption. |
| "It's not X, it's Y" | Copy written declaratively. The hero is three short sentences. |
| Checkmark bullets | Numbered items, table rows, and running prose. |
| 3 pricing tiers | No pricing. |
| No real product demos | Three captures recorded from a real `v0.6.0` build: first run, an approval dialog, an expanded diff. Plus a real `--json` event stream and real `--help`. See "Captures" below. |
| Soft corner radius | Radius is `0` everywhere. |
| Purple and black | Ink is a warm near-black `#16181d`; accent is burnt red `#a8391a`. The violet in the shipped favicon and README badges is not carried onto the site. |
| No skeleton loaders | The masthead reads the latest release from the GitHub API and renders a skeleton while it is in flight, then the value, then an error state. |
| Radial orbs | None. |
| Dot grids | None. |
| Sparkle icons | None. |
| Animated arrows | No arrow moves on hover. Hover changes colour only, instantly. |
| No TOS | `/terms` added. |
| No privacy policy | `/privacy` added. |
| Hover animations | No transforms, no transitions longer than a colour change. `prefers-reduced-motion` disables even the skeleton shimmer. |
| Neon colors | Accent `#a8391a` sits at 0.70 saturation on paper. |
| Basic pastel colours | No pastels. Paper, ink, one accent. |

### Design tokens

```
paper      #f3f0e8   ground
paper-2    #eae6dc   recessed bands, figure boxes, code
paper-3    #e2ddd0   skeleton block
ink        #16181d   text, rules, the inverted band
ink-2      #454a52   secondary text
ink-3      #5d6268   tertiary text, captions
rule       #d6d0c2   hairlines
rule-2     #bdb5a2   stronger hairlines, kbd bottoms
accent     #a8391a   section numbers' marker, links, focus
```

Every foreground/background pair in that table clears WCAG AA at normal
text size: ink on paper 15.59, ink-2 on paper 7.83, ink-3 on paper 5.40,
ink-3 on paper-2 4.94, ink-3 on paper-3 4.54, accent on paper 5.65,
paper on ink 15.59. `ink-3` was `#64696f` during the build, which fell to
4.44 on paper-2 and was darkened to `#5d6268` to clear AA on all three
grounds.

Type: Source Serif 4 Variable (display and prose), IBM Plex Mono 400/500/600
(labels, keys, data, code). No sans-serif on the site at all.

Geometry: `border-radius: 0` universally. Hairlines are `1px solid var(--rule)`.
No box shadows. Base spacing unit 4px; section rhythm 96px desktop / 64px mobile.

### Dark theme

The same editorial language, not a different design. The ground turns to
warm charcoal and every step is re-picked against it, rather than being
inverted mechanically.

```
paper      #191b20   ground
paper-2    #0f1114   recessed bands, figure boxes, code
paper-3    #232630   skeleton block
ink        #ebe8e0   text, rules, the inverted band
ink-2      #b3b0a8   secondary text
ink-3      #8d8b85   tertiary text, captions
rule       #31353d   hairlines
rule-2     #464b55   stronger hairlines, kbd bottoms
accent     #e07a55   section numbers' marker, links, focus
```

The accent is lifted from `#a8391a` because burnt red at that value only
clears 2.64 on a `#191b20` ground; `#e07a55` clears 5.6.

**The inverted band carries its own tokens in both themes.** A straight
paper/ink swap makes the plate either glare (cream plate on a dark ground
measures L 0.872, so the plate shouts louder than the text on it) or vanish
(deep plate on a dark ground measures 1.13 against the ground, so the edge
disappears). So `band` has six values of its own and changes meaning per
theme: in light it is the deep near-black plate; in dark it is a *raised*
slate that sits above the ground rather than punching through it.

```
              light                  dark
band          #16181d   (deep)       #303540   (raised)
band-fg       #f3f0e8               #f0ede5
band-mid      #b6b4ac               #bab7ae
band-dim      #8c8b86               #a3a099
band-rule     #4a4d55               #5a6070
band-accent   #e08a63               #f0a184
```

Every band pair clears AA: light band-fg 15.59, band-mid 8.58, band-dim
5.20, band-accent 6.43; dark band-fg 10.66, band-mid 7.04, band-dim 4.71,
band-accent 6.14.

This also fixed a **pre-existing light-theme AA failure**. The footer and
section 04 had used `text-paper/45`, which measures 4.08 against the plate
and fails. Those three usages are now `text-band-dim` at 5.20.

Selection: the tokens are declared once in `@theme` and overridden twice,
because CSS has no way to share one block between a media query and an
attribute selector:

```css
@media (prefers-color-scheme: dark) { :root:not([data-theme="light"]) { ... } }
:root[data-theme="dark"]            { ... }
```

The media-query form gives a system-preference default and updates live if
the OS flips; the attribute form lets a recorded choice win over it.
`color-scheme` is set alongside so form controls and scrollbars follow.

#### The control

One bordered mono button in the masthead, next to `menu`. It names the mode
it will switch to, the same way `menu` names the action it performs, with
`aria-label="Switch to dark mode"` describing the action for screen
readers. State lives in `localStorage` under `opcode-theme`; with no
recorded value the system preference is followed, and it keeps being
followed until the visitor chooses.

`index.html` carries a small inline script in `<head>` that sets
`data-theme` and the `theme-color` meta before first paint, so the ground
is never painted the wrong colour and then corrected. It is deliberately
before the module script: it has to run in HTML, not in React. It is also
written to survive blocked `localStorage` (falls through to the system
preference) — verified, see Results.

### Page structure

1. Masthead: wordmark SVG from `opcode-logo.svg`, mono uppercase nav, live
   release badge with skeleton.
2. Lead: kicker, three-sentence serif headline, standfirst, install line
   with copy control, Fig. 1 (approval dialog).
3. Fact strip: one ruled row of four numbers, no boxes.
4. Sections numbered `01`–`07` in a margin column, each separated by a
   hairline. Content is ruled lists, tables and figures, never cards.
5. One inverted ink band (Safety) for contrast.
6. FAQ as a ruled `<details>` list.
7. Closing install line.
8. Footer colophon: docs, security, privacy, terms, license, GitHub.

### Docs

Same tokens. Sidebar in mono uppercase, article in serif, `h2` under a
hairline, tables ruled, `pre` recessed with no radius, `kbd` in mono.
The `Source:` provenance line is kept.

### Legal

`/privacy` and `/terms` state what is actually true: the site sets no
cookies and runs no analytics; it makes one request to `api.github.com`
to read the latest release; the binary's only outbound request is the
daily update check; the licence is MIT.

### Captures

Every figure is text the real binary printed, recorded from a build of
the `v0.6.0` tag with `-ldflags "-X main.version=v0.6.0"`, driven through
a pseudo-terminal by `scripts/site-demo/`. The model behind those runs is
a scripted local server (see `scripts/site-demo/README.md`): it exists
only so a real session can be driven end to end. The rig, the commands,
and the raw output are committed so any figure can be re-derived.

## Edge Cases

- Terminal width: captures are recorded at 100 columns so a figure fits
  a 900px measure without horizontal scrolling; narrower viewports scroll
  the figure horizontally inside its own box rather than reflowing it.
- The release fetch can fail or be rate-limited. Three states are
  designed: skeleton, value, and a quiet fallback that shows the last
  known version. A failed fetch never leaves a broken badge.
- The site is served under `/opcode/` on GitHub Pages. All new routes
  are declared in `App.tsx` and `index.html` is copied to `404.html` by
  the existing build script, so deep links resolve.
- `prefers-reduced-motion` and `--plain` postures: no animation at all.
- Long capture lines must not blow out the layout: `pre` scrolls inside
  its own box; the page body never gains horizontal overflow.

## Test Plan

- `npm run build` passes (`tsc -b && vite build`).
- Manual: load `/`, all six docs pages, `/privacy`, `/terms` at 360px,
  768px and 1440px. Check no horizontal page scroll, all figures scroll
  inside themselves.
- Both themes, same matrix. The browser script seeds `localStorage` per
  context so the theme under test is the theme asserted.
- Grep gates (run from `site/`):
  - `grep -rn "lucide" src/` returns nothing.
  - `grep -rn "—" src/` returns nothing outside `src/content/`.
  - `grep -rniE "backdrop-filter|shadow-|rounded-(md|lg|xl|2xl|full)" src/`
    returns nothing.
  - `grep -rn "Inter Variable\|Geist" src/` returns nothing.
- Every figure's text is byte-identical to the file under
  `scripts/site-demo/out/`.

### Results (2026-10-02)

Everything in the plan passed. In addition, a headless Chromium script
walked 10 routes x 4 viewports (1440 / 1024 / 768 / 360) and asserted per
page: `documentElement.scrollWidth === clientWidth`, no computed
`font-family` containing Inter/Geist/Space Grotesk, no element with a
non-zero `border-radius`, no `box-shadow` / `text-shadow` /
`backdrop-filter`, and no console or page errors. 40/40 clean.

Two bugs that script found and that are now fixed:

1. Grid children default to `min-width: auto`, so the widest capture set
   the page's min-content width and the whole page scrolled sideways
   instead of the capture scrolling inside its box. `scrollWidth` was
   1005 against `clientWidth` 390. Fixed with `.grid > * { min-width: 0 }`.
2. The install row's copy control could not shrink below the command's
   min-content, pushing the two buttons off-screen between 640px and
   1024px. Fixed by letting `Install` shrink (`min-w-0 sm:flex-1`).

Also verified by script, not by eye: copy button cycles
copy → copied → copy and writes the clipboard, announcing through
`role="status"`; the mobile menu toggles `menu` ↔ `close` with
`aria-expanded` and reveals only its own links; `<details>` FAQ rows
open and show their answer; the release badge shows its labelled
skeleton while the request is in flight and falls back to the last known
release when GitHub answers 403.

Contrast was computed rather than eyeballed: every token pair clears AA
(see Design tokens). `ink-3` was darkened from `#64696f` to `#5d6268` for
this.

### Results (2026-10-03, dark theme and review)

The theme control was added and the whole matrix re-run in **both**
themes: 9 real routes x 4 viewports x 2 themes = 72 runs, all clean. Each
run asserts the same layout gates as before, plus that
`document.documentElement.dataset.theme` equals the theme seeded for the
context, that `document.body` is actually dark in dark mode (so a theme
that silently failed to apply cannot pass), that an `h1` and the theme
control are present.

Contrast is no longer checked token-by-token but **per element in the
rendered page**: for every element with visible text, the script walks up
the tree compositing alpha, computes the effective ratio against that
element's own background, and fails below 4.5 (3.0 for large text).
Everything passes in both themes.

Fifteen interaction checks on the control, all passing:

- No recorded choice + `prefers-color-scheme: dark` → dark applied.
- Click switches mode, records the choice, and updates the `theme-color`
  meta; the button's label and `aria-label` follow the new mode.
- The choice survives a reload while the OS still prefers the opposite.
- Reachable by keyboard from the skip link in five tabs; `Enter` toggles.
- A recorded choice wins over the opposite system preference.
- **No flash:** with the JS bundle blocked entirely, so React never
  mounts, `data-theme` is already set by the inline head script and the
  body is already the dark ground. This is the check that matters, since
  a flash of wrong ground is invisible to a post-load assertion.
- With `localStorage` throwing on access: the page still renders a theme
  and the control still works.

#### Visual review

The screenshots the earlier session had to skip are now read. The image
reader in this environment returns stale or mismatched files
independently of path, so every capture carries a magenta banner with the
**measured** state burned into its own pixels (`data-theme`, body
background, control label), and was only trusted when that banner matched
the request. Thirteen views confirmed by eye:

- `/` at 1440 in light and dark; `/docs/` at 1440 in light and dark.
- The section 04 plate and the footer in both themes. This is where the
  band's own tokens earn their place: in light the plate is deep
  near-black, in dark it is visibly *raised* above the ground, and the
  edge stays legible both ways.
- `/privacy` and `/terms` at 1440 light.
- `/` at 768 and 360 light, and at 360 dark.

One capture script bug was found and fixed while doing this: the page sets
`scroll-behavior: smooth`, so `scrollIntoView` inherits it and the plate
and footer shots were being taken mid-animation (a shot meant for
`y=8753` landed at `y=6784`). The script now asks for `behavior: "instant"`.

**Still not covered:** nobody has looked at these pages in a real browser
at a real display, and the keyboard path through the whole page (not just
the theme control) has not been walked by hand. The automated checks
cannot judge whether the design is *good*, only whether it holds to the
rules it set itself.
