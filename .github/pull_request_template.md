## Summary

<!-- What changed and why? Link related issues when applicable. -->

## Verification

<!-- List commands run and their results. Note checks not run and why. -->

<!-- Complete the section below only if the candidate contains a non-empty
api/ags/**/api.patch.json, even if this PR does not edit that file.
Otherwise leave it collapsed or remove it; no patch checklist/report is needed. -->

<details>
<summary>API patch verification — only for non-empty patches</summary>

Complete before merging when the candidate contains a non-empty `api.patch.json`.

No live-E2E CI workflow is being added in this phase. Attach a real local E2E
report or link to it here before merging; CI coverage success is not a substitute.

Report attachment/link:

- [ ] Targets `preview`; canonical `api.json` has not been replaced by a private source.
- [ ] Source basis and public-disclosure approval recorded (public link or auditable API-owner confirmation; no internal source text).
- [ ] API version, endpoint/namespace match, SDK/raw-call strategy reviewed.
- [ ] `apipatch check-all`, `apipatch coverage`, and `cobragen check` pass.
- [ ] Every obligation maps to real assertions; documentation exemptions reviewed for behavioral constraints.
- [ ] Required maintainer approval obtained; actual reviewers and roles recorded.
- [ ] Real E2E report attached or linked: command without credentials, head/base, source tree, patch/plan digests, environment alias, timestamps, scenarios/assertions, pass/fail/skip counts and cleanup results.
- [ ] Trusted reviewer's independent reproduction matches the report's current source identity and digests.
- [ ] Cleanup confirmed; responsible owner, review date and upstream-absorption exit condition recorded.

Separate API-owner and CLI-maintainer approvals are optional; independent test
reproduction and source/disclosure verification remain required.

See [Patch verification](https://github.com/TencentCloudAgentRuntime/ags-cli/blob/main/PATCH-VERIFICATION.md). Reports are manual
evidence, not an automated authorization gate. A checked box alone is not proof.

</details>
