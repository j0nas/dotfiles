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
line drawn through the clock. A limit reset (Settings > Usage, redeemed on the web or in the
desktop app) refills the week, the 5-hour window or both; logged with `reset add`, it counts as a
full week more to spend until it expires (a 5-hour-only reset as a 5-hour window more), and its
redemption is read off the snapshots: a window drops while its reset time stays put.

The 5-hour window is a hard limit: an agent running when it's reached is stopped mid-task. `plan`
counts what the agents still running will spend. `watch`, run in the background beside them,
exits once the window will reach its limit before it resets (at the rate it's filling, else the
fitted rates), so the session can have them commit their work and stop. `suspend` logs each one
paused, `queue` holds work not started yet, and `wait`, run in the background, sleeps until the
window resets and then hands both out again. A paused agent's tokens are spread over the time it
ran, not the time it waited.

  claude-quota [status]                       snapshot; the windows, open agents, queued work
  claude-quota start "label" sonnet [--kind implement]    -> prints an agent id
  claude-quota end <id> 229k [--outcome merged|fixed|discarded|partial]
  claude-quota plan sonnet:implement=3 opus:design=1 [--size sonnet=250k]
  claude-quota learn                          fitted rates, typical sizes, outcomes per kind
  claude-quota guard [--at 90] [--lead 20]    ok, hold or suspend: will the 5h window hit its limit first?
  claude-quota watch [--every 180]            poll beside running agents; exits when it's time to suspend
  claude-quota suspend <id> [--ref <agent>] [--note "…"]  an agent paused (or stopped by the limit)
  claude-quota queue add "label" opus:implement [--note "…"] | queue [list] | queue drop <id>
  claude-quota wait [--margin 2]              sleep until the 5h window resets, then hand out the work
  claude-quota resume [--dry-run]             hand out the suspended agents and queued work now
  claude-quota reset add both 2026-10-22 [--note "…"]     a limit reset (week|5h|both) to spend before then
  claude-quota reset [list] | reset used <id>
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
WINDOWS = {"week": "week", "5h": "five", "both": "both"}  # a reset's window, as typed → as logged
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
SUSPEND_AT = FIVE_CAP  # % of the 5h window where running agents are paused, if it'd fill before resetting
LEAD = 20.0  # minutes: pause sooner when the limit is this close, wherever the window stands
BURN_SPAN = 30 * 60  # seconds of readings the observed burn rate is taken over
WATCH_EVERY = 180  # seconds between watch's readings (the endpoint 429s a burst)
WAIT_MARGIN = 2.0  # minutes past the reset before wait reads the new window


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


def started(s: dict) -> dict:
    """A window nothing has been used in yet has no reset time: it ends a full window from the
    reading."""
    return {
        **s,
        "five_reset": s["five_reset"] or s["t"] + 5 * 3600,
        "week_reset": s["week_reset"] or s["t"] + 7 * 86400,
    }


def snapshot(max_age: float = FRESH) -> dict:
    """A fresh reading, logged; the last one if it's under max_age old (the endpoint is
    rate-limited: a burst of calls gets 429s) or if the endpoint can't be reached."""
    snaps = [e for e in entries() if e["kind"] == "snap"]
    if snaps and time.time() - snaps[-1]["t"] < max_age:
        return started(snaps[-1])
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
        return started(snaps[-1])
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
    return started(s)


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
    """Every logged agent: its start entry, plus end time, tokens and outcome once it ended, and
    its pauses ([suspended, resumed or None]) with the session's handle on it and where it was."""
    out: dict[str, dict] = {}
    for e in log:
        if e["kind"] == "start":
            out[e["id"]] = {**e, "end": None, "paused": [], "ref": None, "note": None}
            continue
        a = out.get(e.get("id", ""))
        if a is None:
            continue
        if e["kind"] == "end":
            a.update(end=e["t"], tokens=e.get("tokens"), outcome=e.get("outcome"))
        elif e["kind"] == "suspend":
            a["paused"].append([e["t"], None])
            a["ref"] = e.get("ref") or a["ref"]
            a["note"] = e.get("note")
        elif e["kind"] == "resume" and a["paused"] and a["paused"][-1][1] is None:
            a["paused"][-1][1] = e["t"]
    return out


def suspended(a: dict) -> bool:
    return not a["end"] and bool(a["paused"]) and a["paused"][-1][1] is None


def running(log: list[dict]) -> list[dict]:
    """The agents started and neither ended nor paused."""
    return [a for a in agents(log).values() if not a["end"] and not suspended(a)]


def active(a: dict, lo: float, hi: float) -> float:
    """Seconds of [lo, hi] the agent ran: from its start to its end (or hi), less its pauses."""
    end = a["end"] or hi
    span = max(0.0, min(hi, end) - max(lo, a["t"]))
    for p0, p1 in a["paused"]:
        span -= max(0.0, min(hi, end, p1 if p1 is not None else end) - max(lo, a["t"], p0))
    return max(0.0, span)


def queued(log: list[dict]) -> dict[str, dict]:
    """Work queued for a later window and not yet handed out or dropped."""
    out: dict[str, dict] = {}
    for e in log:
        if e["kind"] == "queue":
            out[e["id"]] = e
        elif e["kind"] in ("resume", "drop"):
            out.pop(e.get("id"), None)
    return out


def refilled(a: dict, b: dict, window: str) -> bool:
    """The window dropped while its reset time stayed put: a limit reset was redeemed (or
    Anthropic reset everyone's limits). Within a window the utilisation otherwise only rises."""
    return a[f"{window}_reset"] == b[f"{window}_reset"] and b[window] < a[window]


def resets(log: list[dict]) -> dict[str, dict]:
    """Every logged limit reset, with `used` set to when it was redeemed: logged with `reset
    used`, else read off the first refill of its window after it was logged and before it
    expired."""
    out: dict[str, dict] = {}
    for e in log:
        if e["kind"] == "reset":
            out[e["id"]] = {**e, "used": None}
        elif e["kind"] == "redeem" and e["id"] in out:
            out[e["id"]]["used"] = e["t"]
    snaps = [e for e in log if e["kind"] == "snap"]
    for a, b in zip(snaps, snaps[1:]):
        for w in ("week", "five"):
            if not refilled(a, b, w):
                continue
            open_ = [r for r in out.values()
                     if r["window"] in (w, "both") and not r["used"] and r["t"] <= b["t"] and r["expires"] >= a["t"]]
            if open_:
                min(open_, key=lambda r: r["expires"])["used"] = b["t"]
    return out


def live_resets(now: float) -> list[dict]:
    """Resets still there to spend: not redeemed, not expired."""
    return sorted((r for r in resets(entries()).values() if not r["used"] and r["expires"] > now),
                  key=lambda r: r["expires"])


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
        if (a["five_reset"] == b["five_reset"] and a["week_reset"] == b["week_reset"]
                and not refilled(a, b, "five") and not refilled(a, b, "week")):
            for w in moved:
                moved[w] += max(0.0, b[w] - a[w])
    for window in ("week", "five"):
        rows, ys = [], []
        for a, b in zip(snaps, snaps[1:]):
            if a[f"{window}_reset"] != b[f"{window}_reset"] or b["t"] <= a["t"] or refilled(a, b, window):
                continue  # the window reset (or was refilled) in between: the delta means nothing
            row = [0.0] * (len(MODELS) + 1)
            for ag in ended:
                run = max(active(ag, ag["t"], ag["end"]), 1.0)
                overlap = active(ag, a["t"], b["t"])
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
    runs = [active(ag, ag["t"], ag["end"]) / 3600 for ag in ended]
    run_hours = statistics.median(runs) if runs else 0.5
    return {
        "rates": rates,
        "baseline": baseline,
        "derived": derived,
        "sizes": sizes,
        "model_size": model_size,
        "outcomes": outcomes,
        "run_hours": run_hours,
        "agents": len(ended),
        "evidence": {m: sum(t) / 1e6 for m, t in by_model.items()},
    }


def size_of(m: dict, model: str, kind: str | None) -> float:
    return m["sizes"].get((model, kind or "-")) or m["model_size"][model]


def in_flight(m: dict, now: float) -> tuple[dict[str, float], int]:
    """What the agents running now will still spend, in points of each window: a typical agent
    of its model and kind, less the share of a typical run it has had. One past its typical run is
    still going, so it's counted at a quarter at least."""
    cost = {"week": 0.0, "five": 0.0}
    going = running(entries())
    for a in going:
        had = active(a, a["t"], now) / 3600 / max(m["run_hours"], 0.1)
        left = size_of(m, a["model"], a.get("task")) * max(0.25, 1 - had)
        for w in cost:
            cost[w] += left / 1e6 * m["rates"][w][a["model"]]
    return cost, len(going)


def burn(s: dict, m: dict) -> tuple[float, str]:
    """How fast the 5-hour window is filling, in points an hour: the faster of what the last half
    hour of readings shows and what the running agents' fitted rates say (a batch that just
    started hasn't shown in the readings yet)."""
    recent = [started(e) for e in entries()
              if e["kind"] == "snap" and 0 <= s["t"] - e["t"] <= BURN_SPAN]
    recent = [e for e in recent if abs(e["five_reset"] - s["five_reset"]) < 300]  # this window only
    seen = None
    if recent and s["t"] - recent[0]["t"] >= 300:
        seen = max(0.0, s["five"] - recent[0]["five"]) / ((s["t"] - recent[0]["t"]) / 3600)
    modelled = m["baseline"]["five"]
    for a in running(entries()):
        per_hour = size_of(m, a["model"], a.get("task")) / max(m["run_hours"], 0.1)
        modelled += per_hour / 1e6 * m["rates"]["five"][a["model"]]
    if seen is not None and seen >= modelled:
        return seen, f"seen over the last {hours(s['t'] - recent[0]['t'])}"
    return modelled, "from the fitted rates of what's running"


def verdict(s: dict, m: dict, at: float = SUSPEND_AT, lead: float = LEAD) -> tuple[str, str]:
    """ok, hold (start nothing more), suspend (pause the running agents) or limited (the window
    is spent): whether the 5-hour window will reach its limit before it resets."""
    now = s["t"]
    to_reset = max(0.0, s["five_reset"] - now)
    if s["five"] >= 100:
        return "limited", f"the 5h window is spent until it resets in {hours(to_reset)}"
    rate, how = burn(s, m)
    eta = (100 - s["five"]) / rate * 3600 if rate > 0 else float("inf")
    pace = f"at {s['five']:.0f}%, filling ~{rate:.0f} pts/h ({how})"
    if eta >= to_reset:
        return "ok", f"{pace}: it resets in {hours(to_reset)}, before its limit"
    if s["five"] >= at or eta <= lead * 60:
        state = "suspend" if running(entries()) else "hold"
        return state, f"{pace}: its limit in ~{hours(eta)}, before it resets in {hours(to_reset)}"
    return "ok", f"{pace}: its limit in ~{hours(eta)}; suspend at {at:.0f}% or {lead:.0f} min before it"


def at_clock(t: float) -> str:
    return datetime.fromtimestamp(t).strftime("%H:%M")


def advise(state: str, s: dict) -> str:
    wake = at_clock(s["five_reset"] + WAIT_MARGIN * 60)
    then = (f"then run `claude-quota wait` in the background: it exits at the reset (~{wake}) with"
            " what to resume, which wakes the session.")
    if state == "suspend":
        return ("Tell each running agent to commit its work in progress and stop at the next safe point,"
                " saying where it was. Log each with `claude-quota suspend <id> --ref <agent> --note \"…\"`,"
                f" queue anything not started (`claude-quota queue add`), {then}")
    if state == "hold":
        return f"Start nothing more in this window: queue it (`claude-quota queue add`), {then}"
    if state == "limited":
        return ("Agents still running are stopped by the limit: log each with `claude-quota suspend <id>"
                f" --ref <agent> --note \"…\"`, {then}")
    return ""


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
    for r in live_resets(now):
        name = {"week": "week", "five": "5h", "both": "week + 5h"}[r["window"]]
        left = r["expires"] - now
        need = 100 - s["five" if r["window"] == "five" else "week"]
        when = datetime.fromtimestamp(r["expires"]).strftime("%b %d %H:%M")
        print(f"reset {r['id']}  a {name} reset, expires in {hours(left)} ({when}), not redeemed"
              + (f": {r['note']}" if r.get("note") else ""))
        if r["window"] != "five":
            per = need / max(left / (5 * 3600), 1)
            print(f"      worth up to {need:.0f} pts if the week reaches its limit first: ≈ {per:.0f} week pts per"
                  " 5-hour window until then. Redeem it in Settings > Usage (web or desktop app).")
        if left < 24 * 3600:
            print("      It lapses within a day: redeem it before then, near the limit or not.")
    for a in agents(entries()).values():
        if not a["end"]:
            ran = hours(active(a, a["t"], now))
            paused = f", suspended {hours(now - a['paused'][-1][0])} ago" if suspended(a) else ""
            print(f"open  {a['id']}  {a['model']}:{a.get('task') or '-'}  {a['label']}  (ran {ran}{paused})")
    for q in queued(entries()).values():
        print(f"queued {q['id']}  {q['model']}:{q.get('task') or '-'}  {q['label']}")


def cmd_status(args) -> None:
    s = snapshot()
    show_status(s, args.target)
    if running(entries()):
        state, why = verdict(s, learn(entries()))
        print(f"5h    {state}: {why}")
        if state != "ok":
            print(f"      {advise(state, s)}")


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
    print(f"  an agent's median run: {m['run_hours'] * 60:.0f} min")
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
    # The main session (and anything else) keeps spending while the agents run: a parallel batch
    # lasts about one agent's run, a serial one the sum.
    hours_running = m["run_hours"] * (count if args.serial else 1)
    for w in cost:
        cost[w] += m["baseline"][w] * hours_running
    now = s["t"]
    # The agents already running keep spending too: what's left is what they leave.
    going, n_going = in_flight(m, now)
    # Each unredeemed reset is a full window more. One that refills both counts for the week only:
    # it's worth most redeemed once the week is spent, not to widen the 5-hour window at hand.
    extra = {"week": 0.0, "five": 0.0}
    for r in live_resets(now):
        extra["five" if r["window"] == "five" else "week"] += 100
    week_left = args.target - s["week"] + extra["week"] - going["week"]
    five_left = max(0.0, FIVE_CAP - s["five"] + extra["five"] - going["five"])
    show_status(s, args.target)
    print()
    if n_going:
        print(f"running {n_going} agent{'s' if n_going > 1 else ''} still going ≈ {going['five']:.0f} pts more"
              f" of this 5h window, {going['week']:.1f} of the week: counted as spent below")
    items = ", ".join(f"{mo}{':' + k if k else ''}×{n:g}" for mo, k, n in want)
    print(
        f"to run  {items}  ≈ {cost['week']:.1f} pts of the week, {cost['five']:.0f} pts of a 5h window"
        f" (incl. ~{hours_running:.1f} h of the main session alongside)"
    )
    if cost["week"] > week_left:
        print(
            f"week    short by {cost['week'] - week_left:.1f} pts: move agents down a model "
            "(opus→sonnet→haiku) first, then drop the least valuable items."
        )
    else:
        spare = week_left - cost["week"]
        print(f"week    fits with {spare:.1f} pts to spare; the week doesn't limit this. Don't pad the work.")
        if extra["week"]:
            print("        That counts the unredeemed limit reset: spend the week toward its limit before it"
                  " expires, then redeem it.")
        if s["week_reset"] - now < 24 * 3600 and spare > 15:
            print("        The reset is close and the spare is large: put it into quality (a stronger model, a review pass).")
    if cost["five"] <= five_left:
        print(f"5h      fits in this window ({five_left:.0f} pts left, resets in {hours(s['five_reset'] - now)}): run it in parallel.")
    else:
        each = cost["five"] / count if count else 0
        fit = int(five_left // each) if each else 0
        print(
            f"5h      about {fit} of {count:g} fit in this window (~{each:.1f} pts each, {five_left:.0f} left);"
            f" queue the rest for the reset in {hours(s['five_reset'] - now)}"
            " (`claude-quota queue add`, then `claude-quota wait` in the background)."
        )
    if count:
        print("        While agents run, keep `claude-quota watch` going in the background: it exits in time to"
              " pause them before the window's limit stops them.")


def cmd_guard(args) -> None:
    s = snapshot()
    state, why = verdict(s, learn(entries()), args.at, args.lead)
    print(f"{state}: {why}")
    if state != "ok":
        print(advise(state, s))


def cmd_watch(args) -> None:
    """Reads the usage every few minutes while agents run, and exits (waking the session that
    started it in the background) once they should be paused, or once none are running."""
    while True:
        s = snapshot()
        state, why = verdict(s, learn(entries()), args.at, args.lead)
        if state != "ok":
            print(f"{state} ({at_clock(s['t'])}): {why}\n{advise(state, s)}")
            return
        if not running(entries()):
            print(f"no agents running ({at_clock(s['t'])}): watch ends. {why}")
            return
        time.sleep(args.every)


def cmd_suspend(args) -> None:
    a = agents(entries()).get(args.id)
    if not a or a["end"]:
        sys.exit(f"claude-quota: no open agent {args.id!r}; `claude-quota status` lists them")
    if suspended(a):
        sys.exit(f"claude-quota: {args.id} is already suspended")
    append({"t": time.time(), "kind": "suspend", "id": args.id, "ref": args.ref, "note": args.note})


def cmd_queue_add(args) -> None:
    model, _, kind = args.spec.partition(":")
    qid = datetime.now(timezone.utc).strftime("q%m%d-%H%M%S-") + os.urandom(2).hex()
    append({"t": time.time(), "kind": "queue", "id": qid, "label": args.label,
            "model": model_of(model), "task": kind or None, "note": args.note})
    print(qid)


def cmd_queue_drop(args) -> None:
    if args.id not in queued(entries()):
        sys.exit(f"claude-quota: nothing queued as {args.id!r}; `claude-quota queue` lists it")
    append({"t": time.time(), "kind": "drop", "id": args.id})


def cmd_queue_list(args) -> None:
    for q in queued(entries()).values():
        print(f"{q['id']}  {q['model']}:{q.get('task') or '-'}  {q['label']}"
              + (f"  — {q['note']}" if q.get("note") else ""))


def hand_out(dry: bool) -> None:
    """Lists the suspended agents and the queued work, and (unless dry) logs them handed out: a
    suspended agent runs again from now, a queued item is started with `start` as any agent is."""
    log = entries()
    paused = [a for a in agents(log).values() if suspended(a)]
    waiting = list(queued(log).values())
    if not paused and not waiting:
        print("nothing suspended or queued")
        return
    for a in paused:
        to = f" → resume it (SendMessage {a['ref']})" if a["ref"] else " → resume it"
        print(f"resume {a['id']}  {a['model']}:{a.get('task') or '-'}  {a['label']}{to}"
              + (f": {a['note']}" if a.get("note") else ""))
    for q in waiting:
        print(f"start  {q['id']}  {q['model']}:{q.get('task') or '-'}  {q['label']} → `claude-quota start` it"
              + (f": {q['note']}" if q.get("note") else ""))
    if waiting:
        print("Price the queued work with `claude-quota plan` first, leaving room for the session itself,"
              " and keep `claude-quota watch` going in the background while it runs.")
    if not dry:
        for x in [*paused, *waiting]:
            append({"t": time.time(), "kind": "resume", "id": x["id"]})


def cmd_resume(args) -> None:
    hand_out(args.dry_run)


def cmd_wait(args) -> None:
    """Sleeps until the 5-hour window resets, confirms it with a fresh reading, then hands out
    what's suspended and queued. Run in the background, its exit wakes the session at the reset."""
    s = snapshot()
    target = s["five_reset"] + args.margin * 60
    print(f"waiting for the 5h window to reset (~{at_clock(target)}, in {hours(target - time.time())})", flush=True)
    time.sleep(max(0.0, target - time.time()))
    n = s
    for _ in range(15):  # the reading may lag the reset by a minute or two
        n = snapshot(max_age=0)
        if n["five_reset"] > s["five_reset"] + 60 or n["five"] < s["five"]:
            break
        time.sleep(120)
    show_status(n, TARGET)
    print()
    hand_out(dry=False)


def deadline(s: str) -> float:
    """An expiry as Settings > Usage shows it, in local time. A bare date means its start, so a
    reset is never planned past the moment it lapses."""
    try:
        return datetime.fromisoformat(s).timestamp()
    except ValueError:
        sys.exit(f"claude-quota: expiry must be a date or date and time, e.g. 2026-10-22 or 2026-10-22T18:00, got {s!r}")


def cmd_reset_add(args) -> None:
    rid = datetime.now(timezone.utc).strftime("r%m%d-%H%M%S-") + os.urandom(2).hex()
    append({"t": time.time(), "kind": "reset", "id": rid, "window": WINDOWS[args.window],
            "expires": deadline(args.expires), "note": args.note})
    print(rid)


def cmd_reset_used(args) -> None:
    if args.id not in resets(entries()):
        sys.exit(f"claude-quota: no reset {args.id!r}; `claude-quota reset` lists them")
    append({"t": time.time(), "kind": "redeem", "id": args.id})


def cmd_reset_list(args) -> None:
    now = time.time()
    for r in resets(entries()).values():
        name = {"week": "week", "five": "5h", "both": "both"}[r["window"]]
        when = datetime.fromtimestamp(r["expires"]).strftime("%Y-%m-%d %H:%M")
        if r["used"]:
            state = "redeemed " + datetime.fromtimestamp(r["used"]).strftime("%m-%d %H:%M")
        elif r["expires"] <= now:
            state = "lapsed"
        else:
            state = f"open, {hours(r['expires'] - now)} left"
        print(f"{r['id']}  {name:4}  expires {when}  {state}" + (f"  {r['note']}" if r.get("note") else ""))


def cmd_log(args) -> None:
    for e in entries()[-args.n :]:
        when = datetime.fromtimestamp(e["t"]).strftime("%m-%d %H:%M")
        if e["kind"] == "snap":
            print(f"{when}  snap   5h {e['five']:>3.0f}%  week {e['week']:>3.0f}%")
        elif e["kind"] == "start":
            print(f"{when}  start  {e['id']}  {e['model']}:{e.get('task') or '-'}  {e['label']}")
        elif e["kind"] == "reset":
            print(f"{when}  reset  {e['id']}  {e['window']} until {datetime.fromtimestamp(e['expires']):%m-%d %H:%M}")
        elif e["kind"] == "redeem":
            print(f"{when}  redeem {e['id']}")
        elif e["kind"] == "suspend":
            print(f"{when}  pause  {e['id']}" + (f"  {e['note']}" if e.get("note") else ""))
        elif e["kind"] == "queue":
            print(f"{when}  queue  {e['id']}  {e['model']}:{e.get('task') or '-'}  {e['label']}")
        elif e["kind"] in ("resume", "drop"):
            print(f"{when}  {e['kind']:6} {e['id']}")
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
    pl.add_argument("--serial", action="store_true", help="the agents run one after another")
    pl.set_defaults(func=cmd_plan)
    sub.add_parser("learn", help="show the fitted rates, sizes and outcomes").set_defaults(func=cmd_learn)
    for name, func, text in (
        ("guard", cmd_guard, "ok, hold or suspend: will the 5h window reach its limit before it resets?"),
        ("watch", cmd_watch, "poll while agents run; exit when they should be suspended, or none run"),
    ):
        g = sub.add_parser(name, help=text)
        g.add_argument("--at", type=float, default=SUSPEND_AT, help="%% of the 5h window to suspend at")
        g.add_argument("--lead", type=float, default=LEAD, help="minutes before the limit to suspend at")
        if name == "watch":
            g.add_argument("--every", type=float, default=WATCH_EVERY, help="seconds between readings")
        g.set_defaults(func=func)
    su = sub.add_parser("suspend", help="log an agent paused (or stopped by the limit), to resume later")
    su.add_argument("id")
    su.add_argument("--ref", help="the session's handle on it, e.g. its agent id for SendMessage")
    su.add_argument("--note", help="where it was, for whoever resumes it")
    su.set_defaults(func=cmd_suspend)
    qu = sub.add_parser("queue", help="work waiting for a later window; list it")
    qu.set_defaults(func=cmd_queue_list)
    qsub = qu.add_subparsers()
    qa = qsub.add_parser("add", help="queue an agent not started yet; prints its id")
    qa.add_argument("label")
    qa.add_argument("spec", help="model[:kind], e.g. opus:implement")
    qa.add_argument("--note")
    qa.set_defaults(func=cmd_queue_add)
    qd = qsub.add_parser("drop", help="drop a queued item")
    qd.add_argument("id")
    qd.set_defaults(func=cmd_queue_drop)
    qsub.add_parser("list", help="the queued work").set_defaults(func=cmd_queue_list)
    wt = sub.add_parser("wait", help="sleep until the 5h window resets, then hand out the work")
    wt.add_argument("--margin", type=float, default=WAIT_MARGIN, help="minutes past the reset")
    wt.set_defaults(func=cmd_wait)
    rm = sub.add_parser("resume", help="hand out the suspended agents and queued work now")
    rm.add_argument("--dry-run", action="store_true", help="list them without marking them handed out")
    rm.set_defaults(func=cmd_resume)
    rs = sub.add_parser("reset", help="log a limit reset to spend before it expires; list them")
    rs.set_defaults(func=cmd_reset_list)
    rsub = rs.add_subparsers()
    ra = rsub.add_parser("add", help="a reset from Settings > Usage; prints its id")
    ra.add_argument("window", choices=tuple(WINDOWS), help="what it refills")
    ra.add_argument("expires", help="local date or date and time, e.g. 2026-10-22 or 2026-10-22T18:00")
    ra.add_argument("--note")
    ra.set_defaults(func=cmd_reset_add)
    ru = rsub.add_parser("used", help="log a reset as redeemed (else read off the snapshots)")
    ru.add_argument("id")
    ru.set_defaults(func=cmd_reset_used)
    rsub.add_parser("list", help="every logged reset and what became of it").set_defaults(func=cmd_reset_list)
    lg = sub.add_parser("log", help="show the latest log entries")
    lg.add_argument("-n", type=int, default=20)
    lg.set_defaults(func=cmd_log)
    args = p.parse_args()
    args.func(args)


if __name__ == "__main__":
    main()
