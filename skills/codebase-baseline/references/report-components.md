# Report template: how to fill it

Template: `assets/report-template.html`. It is one self-contained page with ten sections, seven JSON data blocks and a footer, and it renders as a demo out of the box. You do not edit it. You keep the report's content in a folder of small files, `<ws>/report-src/`, and `codebase-baseline build-report` builds `<ws>/report/index.html` from the template and that folder. A later version of the report changes one file and builds again, so never edit the built page: the next build overwrites it.

## The source folder

Start from the template's own slots:

```
codebase-baseline build-report --init --src <ws>/report-src
```

`--init` refuses a folder that already has files. It writes one file per slot, holding the template's example text, and a `README.md` with the shape of each data file. The template is `~/.claude/skills/codebase-baseline/assets/report-template.html` unless `--template <file>` says otherwise.

- `sections.json` lists the sections in page order: `{"sections": [{"id", "title", "nav", "open", "asof"}, ...]}`. `title` is the heading, `nav` the label in the left index (default the title), `open` starts the section expanded, `asof` is the date on the section's "as of" badge (default `asOf` in `meta-data`). All text is plain, not HTML.
- `sections/<id>.html` is the body of one section, an HTML fragment. The tool adds the heading, the `<details>` wrapper and the index entry. HTML comments are dropped from the page, so leave notes to yourself in them.
- `data/<block>.json` is one of the template's JSON data blocks, named by its id: `meta-data`, `summary-data`, `scoreboard-data`, `treemap-data`, `commits-data`, `bars-data`, `quality-data`. Plain JSON, no comments. Shapes are under Sections and Components below.
- `changelog.txt` holds the footer: one `YYYY-MM-DD text` line per update, newest first (the text may hold inline HTML), and one `note: text` line for the closing sentence on what was read, run and left alone. Lines starting with `#` are comments.
- `README.md` is ignored by the build.

Any other file in the folder is an error, so keep scratch files elsewhere in the workspace.

Fill the data files first, then the sections. Sections that draw from a data file keep the element the page's script fills (`<div id="summary-body">`, `<tbody id="scoreboard-body">`, `<div id="treemap">` with `tm-crumbs` and `tm-ramp`, `<div id="axes">`, a chart's own `<div id>`); the starter files have them, and the build names any that is missing.

Two data files can point at what a subcommand wrote, so nobody pastes it:

- `data/scoreboard-data.json` as `{"from": "../../scoreboard-<name>.json", "note": "Snapshot 2 of 2, ..."}` takes the rows from the file `codebase-baseline scoreboard` wrote, and sets the one-line note above the table (without a `note`, the file's own generic note shows). Only the fields the page reads are kept: `source` and `headline` name snapshot fields, which stay out of the report. Run `scoreboard` again, then build again, for a later snapshot.
- `data/treemap-data.json` as `{"from": "../../treemap/<name>.json"}` takes `tree` and `churnLabel` from the file `hotspots` wrote.

Paths are relative to the source folder. Everything else in a data file is yours.

## Building

```
codebase-baseline build-report --src <ws>/report-src --out <ws>/report/index.html
```

The same files always build the same bytes. A build that finds problems lists every one by file (and line where there is one), exits 1 and writes nothing, so the last good page stays. What it checks, so you need not:

- every data file parses, repeats no key, and has the shape the page draws: `summary-data` has 5 facts, 5 risks and 3 actions, risks have a `sev` the page knows, a chart's series match its months, `bars-data` rows carry a number and a known colour, the quality levels are in range;
- no string holds `<script`, `javascript:` or an `on...=` handler, and no text field uses a tag the page would strip (see the allowlist below);
- `</` inside JSON is written as `<\/` and `<!--` as `\u003c!--` in the page, so you write plain JSON;
- a section keeps its markup balanced and holds no `<script>`, `<iframe>`, `<style>`, event handler or `javascript:` link, and a mermaid diagram holds no tag;
- every section and data file the template has is present; a file or directory that fits no slot is an error;
- ids are unique across the page, every `#` link (in sections, JSON strings and the changelog) resolves, every chart in `bars-data` and `commits-data` has its element and every `bars` element has its data;
- a file still holding the template's example text is refused: a `DUMMY` comment, data equal to the example, the example changelog line, a scoreboard line still reading "Snapshot 1 of N" without a note. `--draft` builds anyway and lists them as notes, to look at the page before it is filled.

The build does not run `check-report`; run it on the result for jargon, glossary and session talk.

## Adding a section

A section the template lacks, such as "What changed" or "Decisions for you" in a later version (`report.md`, "A later version"), takes two steps. Add its entry to `sections.json` where it belongs in the order, with an `id` (lowercase letters, digits, dashes) and a `title`, and write `sections/<id>.html`. The index entry follows from the list. Link to it from any other section with `<a href="#id">`; ids of rows inside it (`<tr id="d-1">`) work as link targets too. The template's own ten sections stay in the list, in any order you choose.

## A later version

Open the folder, change the files the new inputs touch (data files, the sections whose facts moved, `asof` on the sections you re-checked, a line at the top of `changelog.txt`), add the update's sections, and build again. A section that did not change keeps its `asof`.

## Publishing and limits

- Open the built page from disk to check it, then publish the same file as an Artifact. Only external resources: Google Fonts (system fallbacks exist) and `mermaid@11.4.1` from cdn.jsdelivr.net (pinned). If the CDN is blocked, diagram source stays visible as monospace text.
- Keep the file under 1 MB; the build warns above that. Treemap: at most about 600 leaf files (`hotspots --max-leaves 600` folds the rest into `other` leaves). Commit chart: at most 36 months and 4 series. Mermaid: at most about 25 nodes per diagram.
- Text fields in JSON take a small inline-HTML allowlist (`code`, `b`, `i`, `em`, `strong`, `a href="#…"` or `https://…`, `sup`, `sub`, `span`, `br`, `small`, `abbr`, `dfn`); the page strips anything else, and the build refuses it. Text quoting the repo (code, error messages, findings) must be HTML-escaped (`&lt;` for `<`), because a quoted `<img onerror>` must render as text. Escape `"` as `\"` and never put a raw newline in a JSON string. In a section the same escaping applies: a raw `<` that starts a tag is a tag.
- Dates are ISO (`2026-11-10`). Commits and file paths in `<code>`.
- After building, open the page at 1280 and 375 px width in light and dark, and fix overflow. The browser console warns when a glossary slug is missing.

## Page frame

- `data/meta-data.json`: `{project, repo, ref, surveyed, asOf}`. `project` and `asOf` are required. Fills the page title, heading, left index and eyebrow. `asOf` is the default for each section's "as of" badge.
- Each section shows its own badge from its `asof` in `sections.json`. Update it only for sections you re-checked.
- Sections are `<details>`; only those with `"open": true` start open (`what` and `summary` in the template). Index links and any `href="#id"` open their target, so cross-link freely.
- Footer changelog: add one dated line at the top of `changelog.txt` per update.

## Sections

**what.** One paragraph (what it does, for whom, size, age, who maintains it). Then a hand-written SVG system map in place of the starter's. Use the `d-*` classes (`d-box`, `d-box acc|sunk|dash`, `d-t` title, `d-s` subtitle, `d-m` mono, `d-l` arrow label, `d-line acc|dash`), reuse markers `m1` and `m1a`, `viewBox` 960 wide, and a `role="img"` plus `aria-label` that states the flow in words. Label every arrow. Accent = the main user path. Wrap in `<div class="scroll">`.

**summary.** All content comes from `summary-data`: `verdict` (at most 2 sentences), `facts` (exactly 5 `{v, l}`), `risks` (exactly 5 `{sev, t}` plus optional `label`), `actions` (exactly 3 strings). `sev` is `crit|high|med|good`. Order risks by what hurts most on a bad day. It must fit one laptop screen: keep lines short. `sections/summary.html` holds only `<div id="summary-body"></div>`.

**scoreboard.** `scoreboard-data.rows[]`: `{metric, value, first, better, trend, how, asOf, unit?, delta?}`.
- `value` is a number or string; `first` is the value at the first snapshot (`null` if this is it); `better` is `up|down|none` and colours the delta and sparkline; `trend` is an array of numbers, oldest first, last = current (empty array for none).
- `how` is a command that reproduces the number, copy-pasteable from the repo root. `delta` overrides the computed text.
- Rows come from `codebase-baseline scoreboard` through the pointer file (see The source folder), so later snapshots arrive by running it again. Never edit a row: `first` stays the oldest snapshot's value and `trend` grows by itself. The pointer's `note` says which snapshot this is. `sections/scoreboard.html` keeps the table and the `sb-note` line and can carry prose of its own (what a row cannot see, for instance).

**how.** Subsections stack: repo map, key flows, data, auth, getting started.
- Repo map: fill `treemap-data` (below). Edit the caption only.
- Key flows and data: `<figure><pre class="mermaid">...</pre><figcaption>..</figcaption></figure>`. Use `sequenceDiagram` for flows and `erDiagram` for data. Write `&lt;` for a literal `<` (raw `<` breaks the HTML). Do not add `%%{init}` or colours; the page themes diagrams from its tokens and re-renders on theme change. Diagrams in closed sections render when opened.
- Auth: bullet list, weak spots get a chip.
- Getting started: `<pre class="cmd">` with only commands that ran; put the result in a `<span class="c"># ...</span>` comment, including failures and the fix.

**quality.** Fill `quality-data`: `autonomy.level` (0-5), `harness.level` (1-5), `names` lists for the side legend (6 autonomy names, 5 harness names), optional `target`. The plot shades "autonomy outruns harness" and draws the gap. Then the per-pillar table: one row per pillar, status chip `good|med|high|crit`, evidence (file, command or number), gap to the next level.

**delivery.** A `.signals` strip of 3-5 tiles (plain markup), then charts from JSON:
- `commits-data`: `{id, unit, months: ["YYYY-MM"], series: [{name, values, c}], partial}`. `c` is `s1|s2|s3|muted`; series stack bottom-up. `partial` maps a month to a note such as `"10 days"`. The container is `<div id="commits">`. For a second chart, make the file a list of chart objects and add a `<div id>` for each.
- Authorship (bus factor) is a share bar: a `bars-data` key with `"kind": "share"`.

**production.** Only with read access to hosting, analytics, error tracking or logs. Otherwise replace the body with `<p class="stub">No production access yet. See <a href="#sources">Sources &amp; gaps</a>.</p>`. With access: say which dashboards and the date, a `.signals` strip, a findings table (chip + finding), one `.op` callout on what real load says about priorities.

**plan.** Table rows, best first (rows number themselves). Columns: action; metric it moves with its current value, naming a scoreboard row; impact, confidence, effort as `<span class="sev n">high|med|low</span>` (effort may be S/M/L); systemic? is `<span class="sev sys">systemic</span>` or `<span class="sev n">one-off</span>`. "Systemic" means the change stops a class of problem rather than fixing one instance. Opinions go in `.op` callouts (the "My take" label is automatic); facts go above them.

**sources.** Access register: one row per system. Status chip is `granted`, `requested`, `found` (label it "found-in-code") or `unknown`. Method is a read-only recipe, never a credential or token. Last pulled is a date or `never`. Then "Not covered" (what and why) and questions, each addressed to a role.

**glossary.** `<dt id="g-slug">Term</dt><dd>Definition</dd>`. Definitions live only here: the tooltips read them. Entries are this codebase's own terms and the report's scales; a dozen is a lot.

## Components

- **Glossary reference:** `<sup class="g"><a href="#g-slug">?</a></sup>` right after the term. Write any text in the link; JS numbers references 1, 2, 3 by first appearance in the page and shows the definition in the tooltip on hover or focus. Use it for terms specific to this codebase or report (the repo's own name for a module, role or process; harness level), never for general engineering vocabulary (XSS, JWT, bus factor, churn). `report.md` has the rule.
- **Chips:** `<span class="sev X">label</span>`. Severity: `crit`, `high`, `med`, `good`, `low`. Status: `granted`, `requested`, `found`, `unknown`. Neutral: `n`. Accent: `sys`. Use words in the label; the symbol is added.
- **Tooltip:** any element with `data-tip="text"` (`\n` for a new line). Charts set their own.
- **Bars:** `<div class="bars" id="x"></div>` plus `bars-data.x = {unit, pre, rows}`. Rows are `{l, v, c, note}` or `{group: "Heading"}`. `c`: empty (blue), `b2` orange, `b3` grey, `b4` green, `high`, `crit`. `pre` prefixes values (`"$"`). Use grey for context rows, red or orange only for the thing to look at. Each id must be unique; the JSON key is the id.
- **Share bar:** `<div id="x"></div>` plus `bars-data.x = {kind: "share", unit, rows}`; one bar split by value, legend included.
- **Treemap:** `treemap-data = {name, churnLabel, children}`. Directories `{name, children}`; leaves `{name, path, loc, churn, complexity, lang}`. `loc` sets area; `churn` sets colour (commits touching the file over a stated window; say the window in `churnLabel`). Click a directory to zoom; the breadcrumb zooms out. Generated by `codebase-baseline hotspots` (with `--max-leaves`); point the data file at its output.
- **Figure:** `<figure>` then optional `<h3>`, optional `<div class="legend"><span><i style="background:var(--s1)"></i>label</span></div>`, the visual, then `<figcaption><b>Takeaway.</b> one sentence</figcaption>`. Wide SVGs go in `<div class="scroll">` (min width 680 px, scrolls on phones). Colours only via tokens: `--s1 --s2 --s3 --s-muted`, `--good --high --crit --med`, `--accent`. Never hard-code hex; dark mode would break.
- **Table:** `<div class="tw"><table class="wide">...</table></div>`; `class="n"` on right-aligned number cells; `td class="cmd"` for long commands.
- **Callout:** `<div class="op"><p>..</p></div>`. **Signals strip:** `<div class="signals"><div><b>value</b><span>label</span></div>...</div>`.
- **Hand-drawn SVG:** `d-*` classes only, no `style` colours, text 11-14 px at a 960 viewBox, no more than about 15 boxes. For a legend, use `.legend` with `i.line`, `i.line.dash`, `i.line.acc` swatches.

## Style

Terse: a fact, its number, its command. No filler in starter text you keep. Facts are checked against the repo; opinions go in `.op`. Name every number's source in the scoreboard `how` column or the sources table.
