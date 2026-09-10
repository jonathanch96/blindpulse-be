# Sprint reviews

Post-hoc reviews of what each sprint actually shipped, written against the sprint's own document
rather than against a general standard. They exist to answer one question: **would somebody joining
this codebase find what the sprint doc promised?**

Reviews cover both repositories. `blindpulse-fe` mirrors the requirements register but not these
documents; they live here because the product-level sprint plans do.

| Review | Sprint | Kind | Verdict |
|---|---|---|---|
| [sprint-01-identity-accounts.md](sprint-01-identity-accounts.md) | 01 — Identity, accounts, reset trees | Code review | Ships what it claims, with one unguarded front door |
| [sprint-02-market-data.md](sprint-02-market-data.md) | 02 — Market data and blinded feeds | Code review | Blinding is sound; ingest is weaker than its own spec |
| [sprint-03-replay-engine.md](sprint-03-replay-engine.md) | 03 — Replay engine and terminal | Code review | The strongest sprint; one architectural debt, one dead requirement |
| [sprint-04-execution-risk.md](sprint-04-execution-risk.md) | 04 — Execution and the risk gate | **Plan review — not yet built** | Plan is sound; four decisions need making before it starts |

## Severity

| | Meaning |
|---|---|
| **High** | A user can hit it, or it silently corrupts data. Fix before the next sprint. |
| **Medium** | Real, bounded, and will get worse with scale or use. Schedule it. |
| **Low** | Correct but untidy, or a documentation claim that outruns the code. |

## Method and its limits

Findings come from reading the code against the sprint document and the requirements register, and
from running the suites. **They are not from a security audit or a load test.** Where a claim could
not be checked from this environment — the network policy denies every non-GitHub host — the review
says so rather than assuming.

Every finding names a file. Where a finding says a guard is vacuous, that was verified by
introducing the defect and watching the guard stay silent.
