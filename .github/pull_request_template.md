## Summary

<!-- What changed and why? Link related issues when applicable. -->

## Verification

<!-- List commands run and their results. Note checks not run and why. -->

<!-- Complete the section below only if this PR changes the patch contract
(api/ags/**/*.patch.json, api/ags/**/e2e-coverage.yaml or internal/patchscenarios/)
and the candidate contains a non-empty api/ags/**/api.patch.json.
Otherwise remove it and state under Verification that the patch contract is
unchanged and no live patch behavior was tested. -->

<details>
<summary>API patch verification — only for PRs that change a non-empty patch contract</summary>

Complete before merging when this PR changes the patch contract and the candidate
contains a non-empty `api.patch.json`. Items marked "API patch changes" apply only
when this PR changes an `api.patch.json`.

No live-E2E CI workflow is being added in this phase. Attach a real local E2E
report or link to it here before merging; CI coverage success is not a substitute.

Report attachment/link:

- [ ] Targets `main`; canonical `api.json` has not been replaced by a private source.
- [ ] API patch changes: source basis and public-disclosure approval recorded (public link or auditable API-owner confirmation; no internal source text).
- [ ] API patch changes: API version, endpoint/namespace match, SDK/raw-call strategy reviewed.
- [ ] `apipatch check-all`, `apipatch coverage`, and `cobragen check` pass.
- [ ] Every obligation maps to real assertions; documentation exemptions reviewed for behavioral constraints.
- [ ] API patch changes: author-nominated API reviewer (not the author) explicitly accepted the API contract; identity and review link recorded, with expertise and conclusions verified by the lead maintainer.
- [ ] Real E2E report attached or linked: command without credentials, head/base, source tree, patch/plan digests, environment alias, timestamps, scenarios/assertions, pass/fail/skip counts and cleanup results.
- [ ] Trusted reviewer's independent reproduction matches the report's current source identity and digests.
- [ ] Cleanup confirmed. API patch changes: responsible owner, review date and upstream-absorption exit condition recorded.

The API reviewer need not have repository write access. Their review is supporting
evidence, not a substitute for the repository's required maintainer approval.
The lead maintainer verifies API review, independent test reproduction and
source/disclosure evidence; no fixed reviewer teams are required.

See [Patch verification](https://github.com/TencentCloudAgentRuntime/ags-cli/blob/main/PATCH-VERIFICATION.md). Reports are manual
evidence, not an automated authorization gate. A checked box alone is not proof.

</details>
