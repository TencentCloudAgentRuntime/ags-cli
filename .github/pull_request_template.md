## Summary

## Verification

Commands run, results, and checks not run:

## API Patch changes (complete when the candidate contains a non-empty patch)

- [ ] Targets `preview`; canonical `api.json` has not been replaced by a private source.
- [ ] Source basis and public-disclosure approval recorded (public link or auditable API-owner confirmation; no internal source text).
- [ ] API version, endpoint/namespace match, SDK/raw-call strategy reviewed.
- [ ] `apipatch check-all`, `apipatch coverage`, and `cobragen check` pass.
- [ ] Every obligation maps to real assertions; documentation exemptions reviewed for behavioral constraints.
- [ ] API owner and CLI maintainer identified and independently approved.
- [ ] Author strict report and trusted reviewer's independent reproduction match current head/base, source tree and digests.
- [ ] Cleanup confirmed; responsible owner, review date and upstream-absorption exit condition recorded.

See [Patch verification](https://github.com/TencentCloudAgentRuntime/ags-cli/blob/main/PATCH-VERIFICATION.md). Reports are manual
evidence, not an automated authorization gate. A checked box alone is not proof.
