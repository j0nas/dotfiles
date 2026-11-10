# Report template: how to fill it

Template: `assets/report-template.html`. One self-contained file; copy it, then edit. It renders as a demo out of the box: every dummy block is marked `<!-- DUMMY: replace -->` and every section has a `<!-- PLACEHOLDER: ... -->` comment saying what belongs there. Delete both kinds of comment once filled.

Rule of thumb: fill the JSON blocks at the top of `<body>` first, then the prose placeholders. Do not touch the `<script>` at the bottom or the CSS.

## Publishing and limits

- Open it from disk to check it, then publish the same file as an Artifact. Only external resources: Google Fonts (system fallbacks exist) and `mermaid@11.4.1` from cdn.jsdelivr.net (pinned). If the CDN is blocked, diagram source stays visible as monospace text.
- Keep the file under 1 MB. Treemap: at most about 600 leaf files (keep the biggest 600 by lines and fold the rest into a `{name: "other", ...}` leaf). Commit chart: at most 36 months and 4 series. Mermaid: at most about 25 nodes per diagram.
- Text fields in JSON take a small inline-HTML allowlist (`code`, `b`, `i`, `em`, `strong`, `a href="#…"`, `sup`, `span`, `abbr`); the page strips anything else. Text quoting the repo (code, error messages, findings) must be HTML-escaped (`&lt;` for `<`), because a quoted `<img onerror>` must render as text. Escape `"` as `\"`, write `</` as `<\/` inside JSON blocks, and never put a raw newline in a JSON string.
- Dates are ISO (`2026-11-10`). Commits and file paths in `<code>`.
- After editing, run `node --check` on the extracted script if you touched it, open the page at 1280 and 375 px width in light and dark, and fix overflow. The browser console warns when `summary-data` breaks the 5/5/3 rule or a glossary slug is missing.

## Page frame

- `meta-data`: `{project, repo, ref, surveyed, asOf}`. Fills the title, heading, left index and eyebrow. `asOf` is the default for each section's "as of" badge.
- Each `<section data-asof="...">` shows its own badge. Update it only for sections you re-checked.
- Sections are `<details>`; only `what` and `summary` carry `open`. Index links and any `href="#id"` open their target, so cross-link freely.
- Footer changelog: add one `<li><time>date</time><span>what changed</span></li>` per update, newest first.

## Sections

**what.** One paragraph (what it does, for whom, size, age, who maintains it). Then replace the dummy SVG with a hand-written system map. Use the `d-*` classes (`d-box`, `d-box acc|sunk|dash`, `d-t` title, `d-s` subtitle, `d-m` mono, `d-l` arrow label, `d-line acc|dash`), reuse markers `m1` and `m1a`, `viewBox` 960 wide, and a `role="img"` plus `aria-label` that states the flow in words. Label every arrow. Accent = the main user path. Wrap in `<div class="scroll">`.

**summary.** All content comes from `summary-data`: `verdict` (at most 2 sentences), `facts` (exactly 5 `{v, l}`), `risks` (exactly 5 `{sev, t}` plus optional `label`), `actions` (exactly 3 strings). `sev` is `crit|high|med|good`. Order risks by what hurts most on a bad day. It must fit one laptop screen: keep lines short.

**scoreboard.** `scoreboard-data.rows[]`: `{metric, value, first, better, trend, how, asOf, unit?, delta?}`.
- `value` is a number or string; `first` is the value at the first snapshot (`null` if this is it); `better` is `up|down|none` and colours the delta and sparkline; `trend` is an array of numbers, oldest first, last = current (empty array for none).
- `how` is a command that reproduces the number, copy-pasteable from the repo root. `delta` overrides the computed text.
- Later snapshots: append to each `trend`, update `value` and `asOf`, never change `first`. Set top-level `note` to say which snapshot this is.

**how.** Subsections stack: repo map, key flows, data, auth, getting started.
- Repo map: fill `treemap-data` (below). Edit the caption only.
- Key flows and data: `<figure><pre class="mermaid">...</pre><figcaption>..</figcaption></figure>`. Use `sequenceDiagram` for flows and `erDiagram` for data. Write `&lt;` for a literal `<` (raw `<` breaks the HTML). Do not add `%%{init}` or colours; the page themes diagrams from its tokens and re-renders on theme change. Diagrams in closed sections render when opened.
- Auth: bullet list, weak spots get a chip.
- Getting started: `<pre class="cmd">` with only commands that ran; put the result in a `<span class="c"># ...</span>` comment, including failures and the fix.

**quality.** Fill `quality-data`: `autonomy.level` (0-5), `harness.level` (1-5), `names` lists for the side legend (6 autonomy names, 5 harness names), optional `target`. The plot shades "autonomy outruns harness" and draws the gap. Then the per-pillar table: one row per pillar, status chip `good|med|high|crit`, evidence (file, command or number), gap to the next level.

**delivery.** A `.signals` strip of 3-5 tiles (plain markup), then charts from JSON:
- `commits-data`: `{id, unit, months: ["YYYY-MM"], series: [{name, values, c}], partial}`. `c` is `s1|s2|s3|muted`; series stack bottom-up. `partial` maps a month to a note such as `"10 days"`. The container is `<div id="commits">`. For a second chart, add another `<div id>` and call `stackedMonthly(id, months, series, {unit, partial})` from a small `<script>` after the main one (it is on `window`).
- Authorship (bus factor) is a share bar: a `bars-data` key with `"kind": "share"`.

**production.** Only with read access to hosting, analytics, error tracking or logs. Otherwise replace the body with `<p class="stub">No production access yet. See <a href="#sources">Sources &amp; gaps</a>.</p>`. With access: say which dashboards and the date, a `.signals` strip, a findings table (chip + finding), one `.op` callout on what real load says about priorities.

**plan.** Table rows, best first (rows number themselves). Columns: action; metric it moves with its current value, naming a scoreboard row; impact, confidence, effort as `<span class="sev n">high|med|low</span>` (effort may be S/M/L); systemic? is `<span class="sev sys">systemic</span>` or `<span class="sev n">one-off</span>`. "Systemic" means the change stops a class of problem rather than fixing one instance. Opinions go in `.op` callouts (the "My take" label is automatic); facts go above them.

**sources.** Access register: one row per system. Status chip is `granted`, `requested`, `found` (label it "found-in-code") or `unknown`. Method is a read-only recipe, never a credential or token. Last pulled is a date or `never`. Then "Not covered" (what and why) and questions, each addressed to a role.

**glossary.** `<dt id="g-slug">Term</dt><dd>Definition</dd>`. Definitions live only here: the tooltips read them.

## Components

- **Glossary reference:** `<sup class="g"><a href="#g-slug">?</a></sup>` right after the term. Write any text in the link; JS numbers references 1, 2, 3 by first appearance in the page and shows the definition in the tooltip on hover or focus. Define terms a new reader would not know (bus factor, churn, harness level); skip ones everyone knows.
- **Chips:** `<span class="sev X">label</span>`. Severity: `crit`, `high`, `med`, `good`, `low`. Status: `granted`, `requested`, `found`, `unknown`. Neutral: `n`. Accent: `sys`. Use words in the label; the symbol is added.
- **Tooltip:** any element with `data-tip="text"` (`\n` for a new line). Charts set their own.
- **Bars:** `<div class="bars" id="x"></div>` plus `bars-data.x = {unit, pre, rows}`. Rows are `{l, v, c, note}` or `{group: "Heading"}`. `c`: empty (blue), `b2` orange, `b3` grey, `b4` green, `high`, `crit`. `pre` prefixes values (`"$"`). Use grey for context rows, red or orange only for the thing to look at. Each id must be unique; the JSON key is the id.
- **Share bar:** `<div id="x"></div>` plus `bars-data.x = {kind: "share", unit, rows}`; one bar split by value, legend included.
- **Treemap:** `treemap-data = {name, churnLabel, children}`. Directories `{name, children}`; leaves `{name, path, loc, churn, complexity, lang}`. `loc` sets area; `churn` sets colour (commits touching the file over a stated window; say the window in `churnLabel`). Click a directory to zoom; the breadcrumb zooms out. Generated by `codebase-baseline hotspots` (with `--max-leaves`); paste its JSON as is.
- **Figure:** `<figure>` then optional `<h3>`, optional `<div class="legend"><span><i style="background:var(--s1)"></i>label</span></div>`, the visual, then `<figcaption><b>Takeaway.</b> one sentence</figcaption>`. Wide SVGs go in `<div class="scroll">` (min width 680 px, scrolls on phones). Colours only via tokens: `--s1 --s2 --s3 --s-muted`, `--good --high --crit --med`, `--accent`. Never hard-code hex; dark mode would break.
- **Table:** `<div class="tw"><table class="wide">...</table></div>`; `class="n"` on right-aligned number cells; `td class="cmd"` for long commands.
- **Callout:** `<div class="op"><p>..</p></div>`. **Signals strip:** `<div class="signals"><div><b>value</b><span>label</span></div>...</div>`.
- **Hand-drawn SVG:** `d-*` classes only, no `style` colours, text 11-14 px at a 960 viewBox, no more than about 15 boxes. For a legend, use `.legend` with `i.line`, `i.line.dash`, `i.line.acc` swatches.

## Style

Terse: a fact, its number, its command. No filler in placeholders you keep. Facts are checked against the repo; opinions go in `.op`. Name every number's source in the scoreboard `how` column or the sources table.
