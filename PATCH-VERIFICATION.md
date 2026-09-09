# API Patch Verification Guide

Use this guide to map API patch changes to tests, produce local verification
reports, and review the evidence before merging. CI checks coverage mappings;
real API tests require a separate, reviewed local run.

`api.json` is canonical; a passing test does not turn `api.patch.json` into an
official contract. All changes target `main`; patch evidence requirements apply
regardless of whether the PR edits the patch file.

## Required evidence

No live-E2E CI workflow is being added in this phase. CI automatically checks
coverage mappings without cloud credentials; it does not run real API tests or
verify that a report has been attached. Reviewers enforce the evidence requirement.

Every PR whose candidate contains a non-empty `api.patch.json` must attach a
real E2E report, or link to it from the PR description, before merging. Run the
strict command below locally against the real backend in an isolated test
environment. Include the exact command (without credentials), current head/base
SHAs, source tree, patch/plan digests, environment alias, timestamps, executed
scenarios/assertions, pass/fail/skip counts and confirmed cleanup results.
Mocks, coverage mappings and checked boxes are not substitutes for this report.

PRs with empty patches, including changes to verification tooling itself, do not
require a real patch-E2E report. State that no live patch behavior was tested.

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
its report is not cryptographically trusted and must be independently reproduced.
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
including partial-failure IDs. Cleanup callbacks must **confirm absence**, not
merely accept an asynchronous delete response. All registered callbacks run in
reverse order after success/failure/panic, each with a fresh two-minute timeout.
A read-only case explicitly calls `NoResources()`; missing cleanup declarations
fail. Reviewer must audit that declarations cover every resource. Hard kill,
machine loss and hung scenario code still require external TTL/janitor cleanup;
an interrupted/missing report is never usable passing evidence.

## Reviewer and ownership checklist

All PRs follow the repository's normal required-review and CI rules on `main`.
The retired `preview` branch's two-approval requirement no longer applies.

For a candidate containing a non-empty Patch, the author must nominate an API
reviewer other than themselves and link that reviewer's explicit acceptance of the
API contract. The API reviewer may be any GitHub user; repository write access is
not required. A lead maintainer must verify the reviewer's relevant expertise,
identity and conclusions before approving. External API review is supporting
evidence, not a substitute for required maintainer approval.

GitHub enforces the approval count, not API expertise or evidence quality. The
lead maintainer must check API review, source/disclosure verification, the real
E2E report and independent reproduction before approving and merging.

Author report is supporting evidence. A trusted reviewer must independently rerun
the strict command on the exact source in an isolated environment, then compare
identity, plan, assertion records and cleanup. Scenario/entry set equality only
proves bookkeeping; review the assertions themselves and show a targeted wrong
behavior causes failure. Without independent reproduction, do not merge.

New head commits invalidate approvals/reports. Base changes require comparing the
new candidate merge result and rerunning when it changes. The local runner records
identity but does not automatically query GitHub or publish a trusted check.
No live-E2E status check is required in this phase. Any future automation needs
a separate design and security review; it is not a prerequisite for this workflow.

Record public source/disclosure approval, API version and endpoint/namespace
match, SDK or raw-call strategy, API owner, cleanup owner, expiry/review date and
official-absorption exit condition in each Patch PR. Do not paste internal source
text, credentials, tenant IDs, private endpoints or raw sensitive responses into
public GitHub. Use an auditable API-owner confirmation for nonpublic sources.

Real patch scenarios belong with the patch-dependent capabilities they verify.
Do not add fabricated passing scenarios to populate an otherwise empty registry.
