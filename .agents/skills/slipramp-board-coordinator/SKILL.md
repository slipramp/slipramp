---
name: slipramp-board-coordinator
description: Reconcile the Slipramp Trello board with Codex worktree chats and GitHub pull requests. Use for manual or scheduled coordination of cards explicitly placed in Ready for Agent, review follow-up, completion tracking, and conservative Backlog proposals. Do not use for ordinary Slipramp implementation requests or other Trello boards.
---

# Slipramp Board Coordinator

Coordinate authorized Slipramp work without implementing product changes in the coordinator chat. Trello is the human-governed work queue, GitHub pull requests are the review boundary, and one Codex worktree chat owns each pull request.

## Fixed context

- Repository: `/Users/ianunruh/Workspace/slipramp`
- Base branch: `main`
- Trello board: `https://trello.com/b/mHLLfpzN/slipramp`
- Workflow lists, in order: `Backlog`, `Ready for Agent`, `In Progress`, `In Review`, `Complete`
- Maximum concurrent implementation workers: 2

The user has authorized this workflow to create Codex worktree chats for cards in `Ready for Agent` and to send those chats scoped follow-up instructions needed to finish their pull requests or respond to explicit review feedback. Routine preflight may fast-forward the local `main` ref to its fetched `origin/main` descendant. This authorization does not include merging or closing pull requests, force-pushing, resetting or otherwise rewriting `main`, deleting branches, or executing cards from any other list.

## Authority boundaries

- Treat moving a card into `Ready for Agent` as the only authorization to start implementation.
- Never move a Backlog proposal into `Ready for Agent`.
- Never merge or close a pull request. Approval is not permission to merge.
- Never execute a card from `Backlog`, `In Progress`, `In Review`, or `Complete` unless reconciling work already recorded on that card.
- Treat a user move out of the expected list as an override. Stop automatic progress for that card and report it; do not undo the move.
- Do not add, rename, archive, or reorder board lists or labels.
- Keep secrets and credential values out of Trello comments, worker prompts, branches, commits, and pull requests.

## Required preflight

Before changing Trello or dispatching work:

1. Fetch `origin` and compare local `main` with `origin/main`.
   - If they already identify the same commit, continue.
   - If local `main` is an ancestor of `origin/main`, fast-forward local `main` to `origin/main` in the same cycle. A stale, fast-forwardable local ref is routine maintenance, not a blocker and not a reportable event.
   - If `main` is checked out in another worktree, update it only when that worktree is clean and the operation remains fast-forward-only. Otherwise stop rather than disturbing user changes.
   - If local `main` is ahead of or has diverged from `origin/main`, do not reset, force-move, or rewrite it. Stop before external mutations and report the divergence.
2. Adopt the resulting exact `origin/main` commit as the coordinator's understood base and require all three identities to match.
3. Read `AGENTS.md`, `docs/DESIGN.md`, and `docs/ROADMAP.md` from that exact base commit.
4. Confirm that Trello can be read and written.
5. Confirm that GitHub pull requests and their changed files, reviews, checks, and merge state can be read, and that a worker can push a branch and open a pull request without interactive approval.
6. List existing Slipramp worker chats and open pull requests before creating anything.

If any preflight fails, make no board mutation, start no worker, and report the single actionable blocker. Do not repeat an unchanged blocker on every scheduled run.

## Reconciliation cycle

Each invocation performs one bounded cycle and then exits. Do not wait indefinitely.

Serialize cycles for this coordinator chat. Before dispatching, confirm that no earlier coordinator turn is still active. If serialization cannot be established, perform read-only reconciliation but do not claim cards or create workers.

### 1. Reconcile existing work first

Read every card in `In Progress` and `In Review`. Use its coordination comment, worker-chat status, branch, pull request, review state, and checks to derive the current state.

- A worker still running remains `In Progress`.
- An `In Progress` card with no coordination record is an interrupted claim. Search deterministic worker titles, branches, and pull requests for its card ID before acting. If none exists, move the card back to `Ready for Agent` and report the recovery. If any evidence exists but cannot be reconciled safely, leave the card in place and ask the user.
- A worker that needs a material product decision remains in its current list; add or update one concise coordination comment and notify the user.
- When every pull request for a card is open and ready for human review, move the card to `In Review`.
- Explicit change requests may be sent to the same worker chat. Do not interpret ordinary discussion, CI noise, or an approval as a change request.
- Keep an approved but unmerged pull request in `In Review`.
- Move a card to `Complete` only after every recorded pull request is merged.
- If a pull request is closed without merge, or its branch disappears, leave the card in `In Review` and ask the user what should happen.

Prefer updating the existing coordination comment over posting repeated status comments. The comment should record the work-unit plan, worker chat identity, base commit, branch, pull request URL, and current concise state. Never rely on coordinator chat memory as the only record.

### 2. Compute capacity and conflicts

Count active implementation workers and do not exceed two. Inspect actual changed files for every open Slipramp pull request before selecting new work.

Estimate a Ready card's likely files and subsystems from its description and a read-only codebase inspection. Treat uncertainty conservatively. A candidate conflicts when it overlaps an open pull request or depends on behavior that pull request changes.

Always treat these as cross-cutting conflict surfaces even without exact file overlap:

- OpenAPI sources, generated clients, or generated server interfaces
- database migrations, sqlc queries, or shared persistence models
- shared authorization, identity, or request middleware
- shared domain interfaces or lifecycle state machines
- dependency manifests, lock files, build tooling, or CI
- `docs/DESIGN.md` decisions governing both changes

Skip only the conflicting card when independent work is safe. If open pull requests conflict with one another, their combined base is unclear, or two or more Ready cards depend on unresolved cross-cutting work, dispatch nothing until the user reviews the outstanding pull requests.

### 3. Plan pull-request units

The delegation unit is one pull request, not necessarily one card.

- Prefer one pull request when the card is cohesive.
- Split a complex card only into independently reviewable outcomes with clear acceptance criteria.
- Record the complete work-unit plan on the card before starting the first worker.
- Give every unit in a multi-pull-request card a stable ordinal and total, such as `1 of 2`.
- Dispatch independent units concurrently only when they do not overlap.
- Dispatch dependent units sequentially. Record the prerequisite pull request and intended base explicitly.
- Never create speculative cleanup or refactor units that are not required by the card.

### 4. Claim and dispatch

For each selected card, up to available capacity:

1. Re-read the card and verify it is still in `Ready for Agent`.
2. Move it to `In Progress` as the claim.
3. Re-read the card, confirm the move succeeded, and confirm no coordination record or deterministic worker title already exists for the same card ID and work unit.
4. Add one coordination comment with the work-unit plan and the exact `origin/main` base commit.
5. Create one separate Codex task per immediately eligible pull-request unit in a managed worktree based on that exact `origin/main` commit. Give it a deterministic title containing the Trello card object ID and work-unit name.
6. If task creation fails before any unit starts, move the card back to `Ready for Agent`, update the coordination comment with the failure, and do not retry again in the same cycle. If another unit already started, keep the card in `In Progress`, preserve every successful unit, and mark only the failed unit eligible for a later retry.

Use a branch named `codex/trello-<card-number>-<short-slug>`; add a short unit suffix when the card has multiple pull requests.

The worker prompt must include:

- the Trello card URL, exact requested outcome, and its pull-request unit
- the base commit, target branch `main`, and required branch name
- the instruction to read and follow `AGENTS.md`, `docs/DESIGN.md`, and `docs/ROADMAP.md`
- the instruction to update OpenAPI sources and regenerate instead of editing generated files
- the requirement to implement, format, test, review its diff, commit, push, and open a pull request
- the requirement to keep the pull request limited to its assigned unit and explain verification
- for a multi-pull-request card, the unit ordinal and total plus a requirement that the pull request body clearly state:
  - the current unit name and `X of N` position
  - prerequisite or already completed units with pull request links
  - every remaining planned unit and whether it is blocked, sequential, or independently eligible
- the requirement to attach the created pull request to the worker task
- the instruction not to merge, close unrelated pull requests, rewrite shared branches, or start additional work
- the card's coordination details so the pull request can be traced back to Trello

After dispatch, record the worker chat identity and branch on the card. When the worker opens a pull request, verify that any required multi-unit progress summary is present, record its URL, and attach it to the worker task. A missing progress summary is incomplete coordination metadata; send the same worker a scoped follow-up to add it before treating the pull request as ready for human review.

## Backlog proposals

Proposal generation is subordinate to execution and must not become a feedback loop.

- Never propose work in a cycle that claims or dispatches a Ready card.
- Propose only a concrete defect, missing roadmap capability, or narrowly evidenced follow-up discovered from current repository state.
- Search all open and archived board cards first and do not duplicate or restate existing work.
- Add at most one proposal in any seven-day period.
- Create proposals only in `Backlog` with a problem statement, evidence, intended outcome, likely scope, dependencies, and why it is worth considering.
- Never move, schedule, decompose, or implement a proposal. Wait for the user to edit it and move it to `Ready for Agent`.

## Idempotency and failure handling

- Use the Trello card ID plus work-unit name as the durable coordination key.
- Before any external write, read current state and skip the write when the intended result already exists.
- After an ambiguous write result, read back before retrying.
- Do not create a second worker, branch, pull request, or coordination comment for an existing coordination key.
- Retry no external mutation more than once in a cycle.
- Do not retry infrastructure commands merely because orchestration or reporting failed.
- Leave recoverable evidence on the card and report failures that need user action.

## Reporting

Stay quiet when nothing changed and no decision is required. Report only:

- cards claimed or dispatched
- pull requests opened, updated for explicit review feedback, merged, or unexpectedly closed
- cards moved to `In Review` or `Complete`
- a newly detected conflict or actionable blocker
- a newly created Backlog proposal

Keep reports concise and link the relevant Trello cards, worker chats, and pull requests.
