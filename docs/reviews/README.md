# Sprint reviews

Post-hoc reviews of what each sprint actually shipped, written against the sprint's own document
rather than against a general standard. They exist to answer one question: **would somebody joining
this codebase find what the sprint doc promised?**

Reviews cover both repositories. `blindpulse-fe` mirrors the requirements register but not these
documents; they live here because the product-level sprint plans do.

| Review | Sprint | Kind | Verdict |
|---|---|---|---|
| [sprint-01-identity-accounts.md](sprint-01-identity-accounts.md) | 01 — Identity, accounts, reset trees | Code review | Ships what it claims; the unguarded front door is **fixed** |
| [sprint-02-market-data.md](sprint-02-market-data.md) | 02 — Market data and blinded feeds | Code review | Blinding is sound; both ingest defects **fixed** |
| [sprint-03-replay-engine.md](sprint-03-replay-engine.md) | 03 — Replay engine and terminal | Code review | The strongest sprint; the account lockout is **fixed**, one architectural debt remains |
| [sprint-04-execution-risk.md](sprint-04-execution-risk.md) | 04 — Execution and the risk gate | **Plan review — not yet built** | Plan is sound; all five decisions now **settled** and written into the plan |

## Status

Findings are kept after they are fixed rather than deleted, with the row struck through and marked
FIXED. A review that quietly loses its findings as they are addressed stops being a record of what
was wrong and why, which is the part worth keeping.

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
