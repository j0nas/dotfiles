#!/usr/bin/env python3
"""claude-quota: read the account's Claude usage, log it, learn what agents cost, plan spend.

The 5-hour and weekly utilisation come from /api/oauth/usage, the endpoint /usage and the
statusline read, with the OAuth token from the macOS keychain (else ~/.claude/.credentials.json).
Every call appends a snapshot to $XDG_STATE_HOME/claude-quota/log.jsonl, and each agent is
logged from `start` (model, kind of task) to `end` (the tokens its notification reported, and
what became of its work). `learn` fits, over every pair of consecutive snapshots, how much of
each window a million tokens of each model costs, spreading an agent's tokens evenly over its
run, plus a per-hour baseline for the main session and whatever else ran. The utilisation is
whole percents and the 5-hour window moves several times faster than the week, so the weekly
rates come from the 5-hour ones times the observed week-to-5h ratio once there's enough of it.
`plan` prices the work still to do against the budget left: spend follows the backlog, never a
line drawn through the clock.

  claude-quota [status]                       snapshot; the windows and any open agents
  claude-quota start "label" sonnet [--kind implement]    -> prints an agent id
  claude-quota end <id> 229k [--outcome merged|fixed|discarded|partial]
  claude-quota plan sonnet:implement=3 opus:design=1 [--size sonnet=250k]
  claude-quota learn                          fitted rates, typical sizes, outcomes per kind
  claude-quota log [-n 20]
"""

from __future__ import annotations

import argparse
import itertools
import json
import os
import statistics
import subprocess
import sys
import time
import urllib.error
import urllib.request
from datetime import datetime, timezone
from pathlib import Path

STATE = Path(os.environ.get("XDG_STATE_HOME", Path.home() / ".local/state")) / "claude-quota"
LOG = STATE / "log.jsonl"
URL = "https://api.anthropic.com/api/oauth/usage"
TARGET = float(os.environ.get("CLAUDE_QUOTA_TARGET", 92))  # % of the week used at its reset
FIVE_CAP = 90.0  # never plan the 5-hour window past this
MODELS = ("haiku", "sonnet", "opus", "fable")
OUTCOMES = ("merged", "fixed", "partial", "discarded")
# Starting guesses, in % of the window per million tokens an agent's notification reports, and a
# typical agent's tokens. Each weighs PRIOR_WEIGHT Mtok of evidence and fades as agents are
# logged. Seeded from 2026-10-13: Sonnet agents moved the week ~5 points over ~2.7M tokens and
# the 5-hour window ~4× as fast; the other models are scaled by price.
PRIOR = {
    "week": {"haiku": 0.5, "sonnet": 1.5, "opus": 3.5, "fable": 3.5},
    "five": {"haiku": 2.0, "sonnet": 6.0, "opus": 14.0, "fable": 14.0},
}
PRIOR_SIZE = {"haiku": 100_000, "sonnet": 250_000, "opus": 300_000, "fable": 300_000}
PRIOR_WEIGHT = 2.0  # Mtok
FRESH = 60.0  # seconds a reading stays good enough for start, end and plan
RATIO_MIN_FIVE = 30.0  # 5h points observed before the week is derived from the 5h rates


# --- reading ------------------------------------------------------------------------------


def token() -> str:
    raw = ""
    if sys.platform == "darwin":
        r = subprocess.run(
            ["security", "find-generic-password", "-s", "Claude Code-credentials", "-w"],
            capture_output=True,
            text=True,
        )
        raw = r.stdout.strip()
    if not raw:
        p = Path.home() / ".claude/.credentials.json"
        raw = p.read_text() if p.exists() else ""
    if not raw:
        sys.exit("claude-quota: no Claude Code OAuth credentials found")
    return json.loads(raw)["claudeAiOauth"]["accessToken"]


def epoch(iso: str | None) -> float | None:
    if not iso:
        return None
    return datetime.fromisoformat(iso.replace("Z", "+00:00")).timestamp()


def snapshot(max_age: float = FRESH) -> dict:
    """A fresh reading, logged; the last one if it's under max_age old (the endpoint is
    rate-limited: a burst of calls gets 429s) or if the endpoint can't be reached."""
    snaps = [e for e in entries() if e["kind"] == "snap"]
    if snaps and time.time() - snaps[-1]["t"] < max_age:
        return snaps[-1]
    req = urllib.request.Request(
        URL,
        headers={"Authorization": f"Bearer {token()}", "anthropic-beta": "oauth-2025-04-20"},
    )
    try:
        with urllib.request.urlopen(req, timeout=10) as r:
            u = json.load(r)
    except (urllib.error.URLError, TimeoutError) as err:
        if not snaps:
            sys.exit(f"claude-quota: can't read the usage ({err}) and there's no earlier reading")
        print(f"claude-quota: can't read the usage ({err}); using the reading from "
              f"{hours(time.time() - snaps[-1]['t'])} ago", file=sys.stderr)
        return snaps[-1]
    scoped = {
        ((lim.get("scope") or {}).get("model") or {}).get("display_name", "?"): lim.get("percent")
        for lim in u.get("limits", [])
        if lim.get("kind") == "weekly_scoped"
    }
    s = {
        "t": time.time(),
        "kind": "snap",
        "five": u["five_hour"]["utilization"],
        "five_reset": epoch(u["five_hour"]["resets_at"]),
        "week": u["seven_day"]["utilization"],
        "week_reset": epoch(u["seven_day"]["resets_at"]),
        "scoped": scoped,
    }
    append(s)
    return s


def append(entry: dict) -> None:
    STATE.mkdir(parents=True, exist_ok=True)
    with LOG.open("a") as f:
        f.write(json.dumps(entry) + "\n")


def entries() -> list[dict]:
    if not LOG.exists():
        return []
    log = [json.loads(line) for line in LOG.read_text().splitlines() if line.strip()]
    return sorted(log, key=lambda e: e["t"])  # stable: a start and its snapshot keep their order


# --- parsing ------------------------------------------------------------------------------


def amount(s: str) -> float:
    s = s.strip().lower()
    mult = {"k": 1e3, "m": 1e6}.get(s[-1:], 1)
    return float(s[:-1] if mult != 1 else s) * mult


def model_of(s: str) -> str:
    m = s.strip().lower()
    if m not in MODELS:
        sys.exit(f"claude-quota: model must be one of {MODELS}, got {s!r}")
    return m


def hours(seconds: float) -> str:
    seconds = max(0, seconds)
    if seconds >= 86400:
        return f"{int(seconds // 86400)}d{int(seconds % 86400 // 3600)}h"
    if seconds >= 3600:
        return f"{int(seconds // 3600)}h{int(seconds % 3600 // 60)}m"
    return f"{int(seconds // 60)}m"


# --- learning -----------------------------------------------------------------------------


def agents(log: list[dict]) -> dict[str, dict]:
    """Every logged agent: its start entry, plus end time, tokens and outcome once it ended."""
    out: dict[str, dict] = {}
    for e in log:
        if e["kind"] == "start":
            out[e["id"]] = {**e, "end": None}
        elif e["kind"] == "end" and e["id"] in out:
            out[e["id"]].update(end=e["t"], tokens=e.get("tokens"), outcome=e.get("outcome"))
    return out


def lstsq(rows: list[list[float]], ys: list[float]) -> list[float] | None:
    """Ordinary least squares by the normal equations (a handful of unknowns)."""
    k = len(rows[0])
    a = [[sum(r[i] * r[j] for r in rows) for j in range(k)] for i in range(k)]
    b = [sum(r[i] * y for r, y in zip(rows, ys)) for i in range(k)]
    for c in range(k):
        p = max(range(c, k), key=lambda i: abs(a[i][c]))
        if abs(a[p][c]) < 1e-12:
            return None
        a[c], a[p], b[c], b[p] = a[p], a[c], b[p], b[c]
        for i in range(k):
            if i != c:
                f = a[i][c] / a[c][c]
                a[i] = [x - f * y for x, y in zip(a[i], a[c])]
                b[i] -= f * b[c]
    return [b[i] / a[i][i] for i in range(k)]


def nnls(rows: list[list[float]], ys: list[float]) -> list[float]:
    """Non-negative least squares by trying every subset of free columns (k is tiny)."""
    k = len(rows[0])
    best, best_err = [0.0] * k, sum(y * y for y in ys)
    for n in range(1, k + 1):
        for cols in itertools.combinations(range(k), n):
            sol = lstsq([[r[c] for c in cols] for r in rows], ys)
            if sol is None or min(sol) < 0:
                continue
            x = [0.0] * k
            for c, v in zip(cols, sol):
                x[c] = v
            err = sum((sum(r[i] * x[i] for i in range(k)) - y) ** 2 for r, y in zip(rows, ys))
            if err < best_err:
                best, best_err = x, err
    return best


def learn(log: list[dict]) -> dict:
    snaps = [e for e in log if e["kind"] == "snap"]
    ended = [a for a in agents(log).values() if a["end"] and a.get("tokens")]
    rates: dict[str, dict[str, float]] = {}
    baseline: dict[str, float] = {}
    moved = {"week": 0.0, "five": 0.0}  # points each window rose where neither reset
    for a, b in zip(snaps, snaps[1:]):
        if a["five_reset"] == b["five_reset"] and a["week_reset"] == b["week_reset"]:
            for w in moved:
                moved[w] += max(0.0, b[w] - a[w])
    for window in ("week", "five"):
        rows, ys = [], []
        for a, b in zip(snaps, snaps[1:]):
            if a[f"{window}_reset"] != b[f"{window}_reset"] or b["t"] <= a["t"]:
                continue  # the window reset in between: the delta means nothing
            row = [0.0] * (len(MODELS) + 1)
            for ag in ended:
                run = max(ag["end"] - ag["t"], 1.0)
                overlap = min(b["t"], ag["end"]) - max(a["t"], ag["t"])
                if overlap > 0:
                    row[MODELS.index(ag["model"])] += ag["tokens"] / 1e6 * overlap / run
            row[-1] = (b["t"] - a["t"]) / 3600
            rows.append(row)
            ys.append(b[window] - a[window])
        for i, m in enumerate(MODELS):  # the priors, as pseudo-observations
            row = [0.0] * (len(MODELS) + 1)
            row[i] = PRIOR_WEIGHT
            rows.append(row)
            ys.append(PRIOR[window][m] * PRIOR_WEIGHT)
        x = nnls(rows, ys)
        rates[window] = dict(zip(MODELS, x[: len(MODELS)]))
        baseline[window] = x[-1]
    derived = moved["five"] >= RATIO_MIN_FIVE and moved["week"] > 0
    if derived:  # the week from the finer-grained 5h rates
        ratio = moved["week"] / moved["five"]
        rates["week"] = {m: r * ratio for m, r in rates["five"].items()}
        baseline["week"] = baseline["five"] * ratio
    sizes: dict[tuple[str, str], float] = {}
    by_model: dict[str, list[float]] = {m: [] for m in MODELS}
    by_kind: dict[tuple[str, str], list[float]] = {}
    outcomes: dict[tuple[str, str], dict[str, int]] = {}
    for ag in ended:
        key = (ag["model"], ag.get("task") or "-")
        by_model[ag["model"]].append(ag["tokens"])
        by_kind.setdefault(key, []).append(ag["tokens"])
        if ag.get("outcome"):
            outcomes.setdefault(key, {o: 0 for o in OUTCOMES})[ag["outcome"]] += 1
    for key, toks in by_kind.items():
        sizes[key] = statistics.median(toks)
    model_size = {m: statistics.median(t) if t else PRIOR_SIZE[m] for m, t in by_model.items()}
    return {
        "rates": rates,
        "baseline": baseline,
        "derived": derived,
        "sizes": sizes,
        "model_size": model_size,
        "outcomes": outcomes,
        "agents": len(ended),
        "evidence": {m: sum(t) / 1e6 for m, t in by_model.items()},
    }


def size_of(m: dict, model: str, kind: str | None) -> float:
    return m["sizes"].get((model, kind or "-")) or m["model_size"][model]


# --- commands -----------------------------------------------------------------------------


def show_status(s: dict, target: float) -> None:
    now = s["t"]
    print(
        f"5h    {s['five']:.0f}%  resets in {hours(s['five_reset'] - now)}"
        f"   ({max(0, FIVE_CAP - s['five']):.0f} pts to the {FIVE_CAP:.0f}% cap)"
    )
    print(
        f"week  {s['week']:.0f}%  resets in {hours(s['week_reset'] - now)}"
        f"   ({max(0, target - s['week']):.0f} pts to the {target:.0f}% target)"
    )
    for name, pct in s.get("scoped", {}).items():
        if pct:
            print(f"      {name} weekly {pct:.0f}%")
    for a in agents(entries()).values():
        if not a["end"]:
            print(f"open  {a['id']}  {a['model']}:{a.get('task') or '-'}  {a['label']}  ({hours(now - a['t'])})")


def cmd_status(args) -> None:
    show_status(snapshot(), args.target)


def cmd_start(args) -> None:
    s = snapshot()
    aid = datetime.now(timezone.utc).strftime("a%m%d-%H%M%S-") + os.urandom(2).hex()  # unique within a second
    append({"t": time.time(), "kind": "start", "id": aid, "label": args.label, "model": model_of(args.model), "task": args.kind})
    print(aid)


def cmd_end(args) -> None:
    if args.outcome and args.outcome not in OUTCOMES:
        sys.exit(f"claude-quota: outcome must be one of {OUTCOMES}")
    s = snapshot()
    append({"t": time.time(), "kind": "end", "id": args.id, "tokens": amount(args.tokens), "outcome": args.outcome})
    a = agents(entries()).get(args.id)
    if a:
        print(f"{args.id} {a['label']}: {hours(s['t'] - a['t'])}, {amount(args.tokens) / 1e3:.0f}k tokens")


def cmd_learn(args) -> None:
    m = learn(entries())
    src = "from the 5h rates × the week/5h ratio" if m["derived"] else "fitted directly"
    print(f"{m['agents']} finished agents. % of each window per Mtok (week {src}; priors weigh {PRIOR_WEIGHT} Mtok):")
    for model in MODELS:
        print(
            f"  {model:7} week {m['rates']['week'][model]:5.2f}   5h {m['rates']['five'][model]:5.2f}"
            f"   typical agent {m['model_size'][model] / 1e3:4.0f}k   (logged {m['evidence'][model]:.1f} Mtok)"
        )
    print(f"  baseline (main session and other use): week {m['baseline']['week']:.2f} %/h, 5h {m['baseline']['five']:.2f} %/h")
    if m["sizes"]:
        print("by model and kind: median tokens, outcomes (merged/fixed/partial/discarded)")
        for (model, kind), size in sorted(m["sizes"].items()):
            o = m["outcomes"].get((model, kind))
            tally = "/".join(str(o[x]) for x in OUTCOMES) if o else "-"
            print(f"  {model}:{kind:12} {size / 1e3:5.0f}k   {tally}")


def cmd_plan(args) -> None:
    s = snapshot()
    m = learn(entries())
    want: list[tuple[str, str | None, float]] = []
    for item in args.agents:
        spec, _, n = item.partition("=")
        model, _, kind = spec.partition(":")
        want.append((model_of(model), kind or None, float(n or 1)))
    override = {model_of(k): amount(v) for k, _, v in (x.partition("=") for x in args.size or [])}
    cost = {"week": 0.0, "five": 0.0}
    count = 0.0
    for model, kind, n in want:
        size = override.get(model) or size_of(m, model, kind)
        for w in cost:
            cost[w] += n * size / 1e6 * m["rates"][w][model]
        count += n
    week_left = args.target - s["week"]
    five_left = FIVE_CAP - s["five"]
    now = s["t"]
    show_status(s, args.target)
    print()
    items = ", ".join(f"{mo}{':' + k if k else ''}×{n:g}" for mo, k, n in want)
    print(f"to run  {items}  ≈ {cost['week']:.1f} pts of the week, {cost['five']:.0f} pts of a 5h window")
    if cost["week"] > week_left:
        print(
            f"week    short by {cost['week'] - week_left:.1f} pts: move agents down a model "
            "(opus→sonnet→haiku) first, then drop the least valuable items."
        )
    else:
        spare = week_left - cost["week"]
        print(f"week    fits with {spare:.1f} pts to spare; the week doesn't limit this. Don't pad the work.")
        if s["week_reset"] - now < 24 * 3600 and spare > 15:
            print("        The reset is close and the spare is large: put it into quality (a stronger model, a review pass).")
    if cost["five"] <= five_left:
        print(f"5h      fits in this window ({five_left:.0f} pts left, resets in {hours(s['five_reset'] - now)}): run it in parallel.")
    else:
        each = cost["five"] / count if count else 0
        fit = int(five_left // each) if each else 0
        print(
            f"5h      about {fit} of {count:g} fit in this window (~{each:.1f} pts each, {five_left:.0f} left);"
            f" queue the rest for the reset in {hours(s['five_reset'] - now)}."
        )


def cmd_log(args) -> None:
    for e in entries()[-args.n :]:
        when = datetime.fromtimestamp(e["t"]).strftime("%m-%d %H:%M")
        if e["kind"] == "snap":
            print(f"{when}  snap   5h {e['five']:>3.0f}%  week {e['week']:>3.0f}%")
        elif e["kind"] == "start":
            print(f"{when}  start  {e['id']}  {e['model']}:{e.get('task') or '-'}  {e['label']}")
        else:
            print(f"{when}  end    {e['id']}  {e.get('tokens', 0) / 1e3:.0f}k  {e.get('outcome') or ''}")


def main() -> None:
    p = argparse.ArgumentParser(prog="claude-quota", description=__doc__.split("\n\n")[0])
    p.set_defaults(func=cmd_status, target=TARGET)
    sub = p.add_subparsers()
    st = sub.add_parser("status", help="snapshot and show the windows (the default)")
    st.add_argument("--target", type=float, default=TARGET)
    st.set_defaults(func=cmd_status)
    s = sub.add_parser("start", help="log an agent starting; prints its id")
    s.add_argument("label")
    s.add_argument("model", help=f"one of {', '.join(MODELS)}")
    s.add_argument("--kind", help="implement, review, research, design, mechanical…")
    s.set_defaults(func=cmd_start)
    e = sub.add_parser("end", help="log an agent finished, with the tokens it reported")
    e.add_argument("id")
    e.add_argument("tokens", help="e.g. 229k")
    e.add_argument("--outcome", help=f"one of {', '.join(OUTCOMES)}")
    e.set_defaults(func=cmd_end)
    pl = sub.add_parser("plan", help="price the work still to do against the budget left")
    pl.add_argument("agents", nargs="+", help="model[:kind]=count, e.g. sonnet:implement=3 opus=1")
    pl.add_argument("--size", nargs="*", help="model=tokens per agent, overriding the learned size")
    pl.add_argument("--target", type=float, default=TARGET)
    pl.set_defaults(func=cmd_plan)
    sub.add_parser("learn", help="show the fitted rates, sizes and outcomes").set_defaults(func=cmd_learn)
    lg = sub.add_parser("log", help="show the latest log entries")
    lg.add_argument("-n", type=int, default=20)
    lg.set_defaults(func=cmd_log)
    args = p.parse_args()
    args.func(args)


if __name__ == "__main__":
    main()
