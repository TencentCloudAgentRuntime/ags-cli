# API Patch Verification Guide

Use this guide to map API patch changes to tests, produce local verification
reports, and review the evidence before merging. CI checks coverage mappings;
real API tests require a separate, reviewed local run.

`api.json` is canonical; a passing test does not turn `api.patch.json` into an
official contract. All changes target `main`. Choose verification from the PR's
actual effects; an unchanged, non-empty patch does not by itself require a new
API review or live E2E run.

## Required evidence

No live-E2E CI workflow is being added in this phase. CI automatically checks
coverage mappings without cloud credentials; it does not run real API tests or
verify that a report has been attached. Reviewers enforce the evidence requirement.

All PRs require the normal CI and maintainer review, with tests appropriate to
the changed behavior. State the verification scope in the PR description:

| Actual effect of the change | Required evidence |
| --- | --- |
| Local metadata, diagnostics, documentation or release notes, without changing cloud API behavior or live-test evidence | Relevant unit, integration or channel tests. No new patch E2E or API-contract acceptance solely because the repository contains a patch. |
| Effective patched API contract, or patch-dependent request/runtime behavior | Contract review for changed API semantics, applicable local tests, and a strict live report covering the affected behavior. |
| Live scenario setup, readiness, assertions, coverage selection, execution or cleanup behavior | Targeted regression tests and a strict live report when the plan has live obligations. Review changed assertions and failure paths, not just assertion IDs. |
| Implementation of verification evidence guarantees, including source/candidate binding, actual live execution, assertion meaning/completeness, result accounting or cleanup confirmation | The preceding tests and report, plus trusted-reviewer independent reproduction as described below. |

Classify effects, not filenames or commit types. Contract prose can change an
enum or constraint; a help/schema change can also affect request construction.
Promoting patched behavior into canonical `api.json` also requires checking the
affected behavior and channels, even if the patch is reduced or removed.
Formatting and comments that change none of these behaviors do not trigger live
testing. For behavior outside the patch plan, use the relevant tests or live
suite; a passing patch report cannot establish coverage of unrelated commands.

When a live report is required, attach it or link it before merging. Run the
strict command below against the real backend in an isolated test environment.
Include the exact command (without credentials), current head/base
SHAs, source tree, patch/plan digests, environment alias, timestamps, executed
scenarios/assertions, pass/fail/skip counts and confirmed cleanup results.
The archived worker and candidate CLI are both built with `-tags=preview`.
Reports include the actual `channel`, candidate `build_tags` and
`candidate_sha256` when a candidate is executed; a stable worker is rejected.
Mapping/help-only overlays and handwritten preview commands are covered by
channel tests; an empty API patch report is not evidence for those surfaces.
Mocks, coverage mappings and checked boxes are not substitutes for this report.

An empty or documentation-only plan has no live obligations. Record that limit
and validate affected tooling with appropriate regression tests; do not invent
live scenarios to obtain a passing report. If the implementation of verification
evidence guarantees changes, a trusted reviewer must independently reproduce those checks even when
there is no applicable live patch plan.

## Author workflow

1. Run `go run ./cmd/internal/apipatch delta` to derive all current changes in
   every API version, not just the lines changed in this PR. Member names, rather
   than array positions, identify obligations. Added/removed entities include an
   `@exists` obligation as well as their individual properties. Unknown metadata
   changes fail closed and require an explicit classifier change with tests.
   The existing patch engine rejects `move`/`copy`; this tool does not expand it.
2. Register a real CLI scenario in `internal/patchscenarios`, with stable scenario
   and assertion IDs. Use `patchtest.Session.CLI` to invoke the candidate binary.
   Test request values, response shape, readback where available, and meaningful
   boundary/error behavior. Do not replace these checks with `exit == 0` alone.
3. Map each entry in `api/ags/<version>/e2e-coverage.yaml`. Copy the part following
   `#` in the delta's ID. Each entry maps to one scenario and one or more assertion
   IDs; one scenario can satisfy many entries. Repeated, stale, missing and unknown
   mappings fail. To cover an entry with multiple behaviors, use distinct required
   assertion IDs in the same scenario.

   ```yaml
   version: 1
   coverage:
     - entry: /objects/ExampleRequest/members/Limit/required
       scenario: example.lifecycle
       assertions: [limit.readback, limit.boundary]
   ```

4. Documentation-only entries may instead have a nonempty `review` rationale.
   **Prose can encode enums or constraints.** The reviewer must review every such
   exemption; use scenario/assertion mapping instead when the prose changes a
   behavioral promise. `document`/`example` classification is not an automatic
   claim that live testing is unnecessary.
5. Run `make api-patch-coverage`. CI always runs `Patch Contract Coverage`, even
   for empty patches, and includes it in `CI Gate`. Empty entries mean there is
   no runtime obligation, not that any live behavior was tested.

## Strict execution and evidence

Only after reviewing the candidate code, use a disposable, isolated environment
and a dedicated least-privilege cloud test account. An isolated CLI home prevents
accidental config reuse; it is **not a security sandbox** for candidate Go code.
Do not run arbitrary PR code on a privileged maintainer laptop or inject release
credentials/repository-write tokens. The local runner does not authorize or
trigger live tests in GitHub Actions.

From a clean checkout of the exact PR head (fetch the exact base commit too):

```bash
# Set TENCENTCLOUD_SECRET_ID, TENCENTCLOUD_SECRET_KEY, AGR_REGION via your
# credential provider; set TENCENTCLOUD_TOKEN for temporary STS credentials.
# Optional AGR_CLOUD_ENDPOINT / AGR_DOMAIN must be reviewed for the target API.
make api-patch-e2e ARGS="--repository TencentCloudAgentRuntime/ags-cli --pr 123 --head <full-head-sha> --base <full-base-sha> --environment disposable-test" > /tmp/patch-e2e-report.json
```

Use a **new report path per run** outside the checkout. Staged, unstaged and
untracked files make the strict command fail. Do not hide changes with Git
assume-unchanged/skip-worktree. The launcher rebuilds the test runner (including
its scenario registry) from a temporary `git archive` of the recorded commit.
That worker rejects a different head and builds the candidate CLI from the same
commit. Both builds disable workspace mode, ambient Go flags and automatic VCS
stamping (`-buildvcs=false`), since the archives have no `.git` metadata. Source
identity is checked again after testing. The runner itself is candidate code:
its report is not cryptographically trusted. Review its implementation and apply
the independent-reproduction requirements below.
Launcher cancellation forwards an interrupt to the worker for cleanup, with a
three-minute grace period before forced termination.

Report fields include repository/PR, full head/base and tested commit/tree,
path-and-content Patch digest across all versions, plan digest, entrypoint version,
environment alias, timestamps, results, assertion IDs, CLI call counts and per-case
cleanup status. Digests use SHA-256 over sorted length-prefixed path/content pairs.
Compare plan digests with `apipatch coverage` on the same source; the public runtime
report omits raw contract values and raw CLI output. Review aliases, IDs and review
rationales for disclosure before posting. Repository/PR/base and environment alias
are supplied context: reviewers must check them against GitHub and the real account.

Build diagnostics are written to local stderr, separately from the JSON report.
They may contain source excerpts or local paths: do not combine stderr into the
public report or publish build logs without a disclosure review.

Missing explicit credentials (for selected live scenarios), unexpected command failures or failed assertions,
missing assertion execution, skip, panic, cancellation and cleanup failure cannot
produce a passing run. Scenarios must propagate relevant CLI errors. Even if an
assertion's returned error is ignored, a failed assertion poisons the scenario.
Empty/documentation-only plans return `not_applicable`, never live `pass`.

Each scenario must register cleanup immediately for every created resource,
including partial-failure IDs. Cleanup callbacks must confirm the API's completed
cleanup state, not merely accept an asynchronous request: deleted resources must
be absent; APIs that stop instances may retain history, so `STOPPED` or absence
is sufficient. Remove run-specific test data and report cleanup failures; identify
shared fixtures or history deliberately retained. Retained history is not a
requirement to delete resources beyond the API's lifecycle contract.
All registered callbacks run in
reverse order after success/failure/panic, each with a fresh two-minute timeout.
A read-only case explicitly calls `NoResources()`; missing cleanup declarations
fail. Reviewer must audit that declarations cover every resource. Hard kill,
machine loss and hung scenario code still require external TTL/janitor cleanup;
an interrupted/missing report is never usable passing evidence.

## Reviewer and ownership checklist

All PRs follow the repository's normal required-review and CI rules on `main`.
The retired `preview` branch's two-approval requirement no longer applies.

When a PR introduces or changes an effective patched API contract, link acceptance
of the changed semantics by a reviewer other than the author who understands the
affected API. That reviewer may be the approving maintainer; this does not require
an additional person or approval count. A separate API reviewer's identity,
relevant expertise and conclusions must be verified by the maintainer. Repository
write access is not required for that supporting review.

Record the source/disclosure basis, API version and endpoint/namespace match,
SDK or raw-call strategy, API owner and official-absorption exit condition for
changed contracts. Link existing confirmed evidence for unchanged parts and check
that it still applies; do not reopen the entire contract review for unrelated
changes. Public source links or auditable API-owner confirmation may establish
the source basis. An API reviewer and an API owner are not necessarily the same
person. Record the cleanup owner and any review/expiry date needed for retained
test resources with the live evidence.

Recheck source/disclosure evidence when its basis or the target service changes,
even if the request shape is unchanged.

The maintainer reviews the applicable evidence and its limits before approving.
Ordinary scenario setup or readiness changes with unchanged assertions, real CLI
execution, error/timeout propagation and cleanup guarantees do not automatically
require a second operator to repeat the live run.

Independent reproduction is required when the change affects the implementation
of guarantees that make a report trustworthy: source/candidate binding, real
backend execution,
assertion predicates or expected values, required-assertion selection, failure or
skip accounting, or cleanup confirmation. Keeping assertion IDs unchanged does
not waive this requirement. A trusted reviewer reruns the strict command on the
exact source in an isolated environment (or the applicable tooling regressions
when the plan has no live obligations), then compares identity, plan, assertion
records and cleanup. Also demonstrate that targeted wrong behavior fails.
For other changes, a maintainer may require independent reproduction for a stated
risk or evidence gap; record the reason and result in the review. Once required,
it must be completed before merging. Do not claim reviewer reproduction from an
author's report or from static coverage checks.

When a current-head live report is required, a new head needs a new report.
Earlier reports may be cited as historical evidence, with their original identity,
but not relabeled as current-head runs. GitHub controls approval invalidation.
Base changes require comparing the new candidate merge result and rerunning when
they affect the tested behavior or verification evidence. The local runner records
identity but does not automatically query GitHub or publish a trusted check.
No live-E2E status check is required in this phase. Any future automation needs
a separate design and security review; it is not a prerequisite for this workflow.

Changes to this verification policy need normal maintainer review, including an
explicit assessment of the policy PR's own verification scope. They do not add a
release gate or make earlier incomplete evidence complete.

Do not paste internal source text, credentials, tenant IDs, private endpoints or
raw sensitive responses into public GitHub. Use an auditable API-owner
confirmation for nonpublic sources.

Real patch scenarios belong with the patch-dependent capabilities they verify.
Do not add fabricated passing scenarios to populate an otherwise empty registry.
