# Codex Project Instructions

## Critical Product Rules

* Prioritize product correctness, maintainability, and realistic user behavior over minimal diffs.
* Do not introduce silent behavior changes.
* Any user-visible, API, database, strategy, timing, sorting, filtering, or data-format change must be called out before implementation.
* Calling out an intended change is a progress update, not a request for approval. Continue within the user's authorized scope after explaining the change; ask only when essential information is missing or an action requires additional authorization.
* Never expose, log, commit, or hard-code secrets.
* Treat production data, market data, Redis, queues, caches, migrations, and external APIs as high-risk areas.
* Read-only inspection may proceed within the authorized task, while protecting secrets and avoiding expensive queries or excessive external API traffic. Before writes, deletions, migrations, service restarts, or production operations, assess scope, reversibility, and downstream effects. Request confirmation for destructive or out-of-scope actions unless the user has already explicitly authorized them; always respect tool approval requirements.

## Before Coding

For non-trivial behavior changes or bug fixes:

* State the intended behavior and acceptance criteria.
* Identify affected modules, services, data paths, or UI surfaces.
* Document the finalized approach in the relevant design/spec doc when behavior, architecture, data contracts, or strategy semantics change.
* For small mechanical changes, documentation is not required unless behavior changes.

## Testing / Regression Protection

For core behavior changes, add or update meaningful tests covering:

* normal behavior
* edge cases
* invalid or missing inputs
* error handling
* regression cases related to the change

For bug fixes:

* identify the root cause
* add or update a test that would fail before the fix
* implement the fix
* run the relevant tests
* check for similar logic elsewhere

Do not chase 100% coverage mechanically. Prefer meaningful assertions around changed core behavior and public integration boundaries.

## Data / Reliability Checks

When touching database, Redis, queues, caches, external APIs, replay data, or market-data pipelines, consider:

* backward compatibility
* duplicate writes
* partial failures
* stale cache
* race conditions
* timezone handling
* null, zero, missing, or malformed values
* performance on large datasets
* noisy or missing diagnostics

## Done Means

A task is done only when:

* the implementation matches the stated intent
* relevant tests or checks were run
* no unrelated code was changed
* risky behavior changes were documented
* errors are handled explicitly
* logs or diagnostics are sufficient for debugging
* the final summary explains what changed and how it was verified
