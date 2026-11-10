export const meta = {
  name: 'codebase-baseline-scan',
  description: 'Wave 1 code-only scan per lens, adversarial check of critical/high findings, then wave 2 reads docs as claims',
  whenToUse: 'Called by the codebase-baseline skill after measurement; not on its own',
  phases: [
    { title: 'Scan', detail: 'one scanner per lens, docs-stripped export only' },
    { title: 'Verify', detail: 'skeptics try to refute critical and high findings, batches of up to 8' },
    { title: 'Docs', detail: 'docs checked against verified facts; history the code cannot show' },
  ],
}

// args:
// {
//   skillDir: "/abs/path/to/skills/codebase-baseline",
//   workspace: "/abs/path/<name>-baseline",
//   repos: [{ name, path, sha, scan: "<ws>/scan/<name>", run: "<ws>/run/<name>" }],
//   lenses: [{ key (unique), topic, paths?: ["<ws>/scan/<name>/sub"], history?: true, model?: "sonnet", effort?: "high" }],
//   verify: { model?: "sonnet", effort?: "xhigh", batch?: 8, maxPerLens?: 16 },
//   docs: { model?: "sonnet", effort?: "high", maxChunks?: 6, chunks: [{ label, paths: ["<ws>/run/<name>/docs/..."] }] }
// }
// Scanners and verifiers only see the docs-stripped scan exports. A lens with
// history: true (delivery) also gets the original repo path, for read-only git.
// Agents read their briefs from <skillDir>/agents/ and write full results into the
// workspace; they return only the small summaries below.

const ws = args.workspace
const brief = name => `${args.skillDir}/agents/${name}.md`
const scanRepos = args.repos.map(r => `- ${r.name} at commit ${r.sha}: ${r.scan}`).join('\n')
const historyRepos = args.repos.map(r => `- ${r.name}: original repo ${r.path} (read-only git only)`).join('\n')
const docsRepos = args.repos
  .map(r => `- ${r.name}: run export ${r.run} (docs live here), original repo ${r.path} (read-only git only)`)
  .join('\n')

const seen = new Set()
const dupes = args.lenses.map(l => l.key).filter(k => seen.has(k) || !seen.add(k))
if (dupes.length) throw new Error(`lens keys must be unique; duplicated: ${dupes.join(', ')}`)

const SCAN = {
  type: 'object',
  properties: {
    lens: { type: 'string' },
    file: { type: 'string' },
    crit_high_ids: { type: 'array', items: { type: 'string' } },
    counts: {
      type: 'object',
      properties: {
        crit: { type: 'integer' }, high: { type: 'integer' }, med: { type: 'integer' },
        low: { type: 'integer' }, info: { type: 'integer' },
      },
    },
    not_covered: { type: 'string' },
  },
  required: ['lens', 'file', 'crit_high_ids'],
}
const VERDICTS = {
  type: 'object',
  properties: {
    file: { type: 'string' },
    confirmed: { type: 'integer' }, refuted: { type: 'integer' }, unsure: { type: 'integer' },
  },
  required: ['file', 'confirmed', 'refuted', 'unsure'],
}
const DOCS = {
  type: 'object',
  properties: {
    chunk: { type: 'string' },
    file: { type: 'string' },
    claims_checked: { type: 'integer' },
    false_or_stale: { type: 'integer' },
    history_items: { type: 'integer' },
  },
  required: ['chunk', 'file'],
}

function scanPrompt(l) {
  const paths = l.paths && l.paths.length ? `\nLimit yourself to: ${l.paths.join(', ')}` : ''
  const hist = l.history ? `\nFor history you may also run read-only git commands in:\n${historyRepos}` : ''
  return `Read ${brief('scanner')} and follow it.

Lens: ${l.key}
Topic: ${l.topic}${paths}

Code to read (docs removed on purpose):
${scanRepos}${hist}

Workspace: ${ws}
Measurements: ${ws}/snapshots/<repo>/ (newest file per repo), ${ws}/treemap/<repo>.json, ${ws}/loop-results/<repo>.json
Write your result to: ${ws}/findings/${l.key}.json

Your final message is the summary object: lens, file, the ids of findings rated crit or high (crit first, most severe first), counts per severity, and one line on what you did not cover. This overrides the brief's last section.`
}

function verifyPrompt(lensKey, file, ids, n) {
  return `Read ${brief('verifier')} and follow it.

Findings file: ${file}
Finding ids to check: ${ids.join(', ')}

Code to read (docs removed on purpose):
${scanRepos}

Write your verdicts to: ${ws}/verified/${lensKey}-${n}.json
Your final message is the summary object: file, and the counts of confirmed, refuted and unsure. This overrides the brief's last section.`
}

function docsPrompt(c, scanned) {
  return `Read ${brief('docs-reader')} and follow it.

Chunk: ${c.label}
Docs to read: ${c.paths.join(', ')}

Repos:
${docsRepos}

Wave 1 findings: ${scanned.map(s => s.file).join(', ')}
Verdicts: ${ws}/verified/
Measurements: ${ws}/snapshots/<repo>/ (newest file per repo; its docs section has words and last-touched dates)
Write your result to: ${ws}/docs/${c.label}.json
Your final message is the summary object: chunk, file, claims checked, how many were false or stale, history items found. This overrides the brief's last section.`
}

const v = Object.assign({ model: 'sonnet', effort: 'xhigh', batch: 8, maxPerLens: 16 }, args.verify || {})

const lensResults = await pipeline(
  args.lenses,
  l => agent(scanPrompt(l), {
    label: `scan:${l.key}`, phase: 'Scan', schema: SCAN,
    model: l.model || 'sonnet', effort: l.effort || 'high',
  }),
  async (scan, l) => {
    if (!scan) { log(`scanner ${l.key} returned nothing; its lens is missing from the report`); return null }
    const all = scan.crit_high_ids || []
    const ids = all.slice(0, v.maxPerLens)
    const capped = all.slice(v.maxPerLens)
    if (capped.length) log(`${l.key}: ${capped.length} crit/high findings over the cap stay unverified`)
    const batches = []
    for (let i = 0; i < ids.length; i += v.batch) batches.push(ids.slice(i, i + v.batch))
    const verdicts = await parallel(batches.map((b, n) => () =>
      agent(verifyPrompt(l.key, scan.file, b, n + 1), {
        label: `verify:${l.key}#${n + 1}`, phase: 'Verify', schema: VERDICTS,
        model: v.model, effort: v.effort,
      })))
    const failed = batches.filter((b, n) => !verdicts[n]).flat()
    if (failed.length) log(`${l.key}: verifier died; ${failed.length} findings unverified`)
    return { lens: l.key, scan, verdicts: verdicts.filter(Boolean), unverified: capped.concat(failed) }
  },
)

const scanned = lensResults.filter(Boolean).map(r => r.scan)
const d = Object.assign({ model: 'sonnet', effort: 'high', maxChunks: 6 }, args.docs || {})
const allChunks = d.chunks || []
const chunks = allChunks.slice(0, d.maxChunks)
if (allChunks.length > chunks.length) {
  log(`docs: ${allChunks.length - chunks.length} chunks over the cap were not read: ${allChunks.slice(d.maxChunks).map(c => c.label).join(', ')}`)
}
let docsResults = []
if (!chunks.length) {
  log('no docs chunks given; wave 2 skipped')
} else {
  // Barrier on purpose: docs readers compare against ALL verified wave 1 facts.
  docsResults = (await parallel(chunks.map(c => () =>
    agent(docsPrompt(c, scanned), {
      label: `docs:${c.label}`, phase: 'Docs', schema: DOCS,
      model: d.model, effort: d.effort,
    })))).filter(Boolean)
}

const tally = lensResults.filter(Boolean).reduce((t, r) => {
  r.verdicts.forEach(x => { t.confirmed += x.confirmed; t.refuted += x.refuted; t.unsure += x.unsure })
  return t
}, { confirmed: 0, refuted: 0, unsure: 0 })
log(`lenses ${scanned.length}/${args.lenses.length}; crit/high verdicts: ${tally.confirmed} confirmed, ${tally.refuted} refuted, ${tally.unsure} unsure; docs chunks ${docsResults.length}/${allChunks.length}`)

return {
  lenses: lensResults.filter(Boolean),
  missing_lenses: args.lenses.filter((l, i) => !lensResults[i]).map(l => l.key),
  unverified: lensResults.filter(Boolean).map(r => ({ lens: r.lens, ids: r.unverified })).filter(x => x.ids.length),
  docs: docsResults,
  docs_not_read: allChunks.slice(d.maxChunks).map(c => c.label),
  tally,
}
