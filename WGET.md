# GitNexus Wiki Generation Instructions (Code Mastery)

Generate wiki documentation for engineers who need to modify this codebase safely, debug quickly, and make non-trivial refactors with confidence.

## Objective

Produce a wiki that enables **code mastery**, not only architecture awareness.

Code mastery means the reader can answer:

1. What happens at runtime, step by step, for each critical flow?
2. Which invariants must never be broken?
3. Where are edge cases handled and what fails if they are not?
4. How do errors propagate, get classified, and affect retry/user behavior?
5. What is safe vs risky to change, and what tests validate behavior?

## Source-of-truth rules

1. Prefer source code evidence over assumptions.
2. Every non-obvious claim must be anchored to concrete symbols and file paths.
3. If uncertain, label the statement as `Inference` and say why.
4. If docs conflict with code, trust code and explicitly call out the mismatch.
5. Avoid generic filler text and repeated explanations across pages.

## Required analysis workflow

For each major topic/module:

1. Identify relevant execution flows from GitNexus processes.
2. Trace caller -> callee chains for the critical paths.
3. Read implementation files directly for behavior details.
4. Map errors, retries, limits, and cancellation behavior.
5. Map tests that prove behavior (and list missing test coverage).
6. Document blast radius for likely future changes.

If index is stale, run `npx gitnexus analyze` first.

## Required outputs

Generate or update pages so the wiki contains:

1. `overview.md`: architecture, module boundaries, and system constraints.
2. `runtime-flows.md`: end-to-end traces for critical flows with ordered steps.
3. `module-<name>.md` pages (one per major module).
4. `contracts-and-invariants.md`: global invariants, pre/postconditions, state guarantees.
5. `error-semantics.md`: error taxonomy, retryability matrix, user-visible outcomes.
6. `concurrency-and-performance.md`: worker model, locking, hot paths, complexity, bottlenecks.
7. `testing-map.md`: behavior -> test mapping, coverage gaps, high-risk untested paths.
8. `change-playbook.md`: safest extension points, risky edits, recommended validation sequence.
9. `open-questions.md`: ambiguities, inferred behavior, and items requiring human confirmation.

## Per-module page template (mandatory sections)

Each module page must include these sections in this order:

1. **Responsibility and Boundaries**
2. **Entrypoints and Call Graph**
3. **Data Contracts and Key Types**
4. **Runtime Algorithm (step-by-step)**
5. **Invariants and Edge Cases**
6. **Error Paths and Recovery Logic**
7. **Concurrency Model and Cancellation**
8. **Performance Characteristics**
9. **Tests and Coverage Gaps**
10. **Safe Change Guide (what can break, what to run)**

## Precision requirements

1. Use exact function/type names as implemented.
2. Include file paths in each major section.
3. Distinguish clearly between:
   - `Verified`: directly supported by code
   - `Inference`: reasoned but not explicitly proven
4. Do not claim usage by a module unless a real call/import path exists.
5. Do not describe BFS as DFS (or similar algorithm mistakes); verify actual control structure.

## Runtime flow depth requirement

For each critical flow (single download, batch/concurrent download, mirror, background mode):

1. Show initialization sequence.
2. Show main loop / worker behavior.
3. Show retry/error branches.
4. Show completion/finalization sequence.
5. Show cancellation/shutdown behavior.
6. Include at least one "failure walkthrough" with concrete error propagation.

## Error and retry documentation standard

Document a table with:

1. Originating layer/module
2. Error type/code
3. Retry decision (yes/no/conditional)
4. Backoff behavior (if any)
5. User-visible impact (exit code, message, partial state)

## Testing documentation standard

For each key behavior, map:

1. Behavior statement
2. Test file and test name
3. Confidence level (`High` when directly tested, `Medium` when indirectly covered, `Low` when not tested)
4. Recommended missing tests

## Quality gate checklist (must pass before finishing)

1. No repeated high-level content across pages.
2. No unlabelled assumptions.
3. All critical flows documented end-to-end.
4. All major modules include invariants + failure modes + tests.
5. At least one risky change scenario documented with blast radius.
6. Docs enable a new maintainer to debug production failures without reading all source files first.

## Writing style

1. Assume experienced developers as audience.
2. Prefer precise technical language over marketing language.
3. Prefer depth over brevity, but keep each paragraph evidence-driven.
4. Explain both **what** the code does and **why** it likely does it that way.
