# Graywolf — Claude Code Instructions

## Release workflow

When the user asks to release, cut a release, bump, tag, or any equivalent phrasing:

0. **Draft the release note with the user.** Before running any bump target, ask the user what user-facing changes this release contains and whether any configuration action is required. Translate each change into plain English, with no reference to internal packages, APIs, or architecture — operators only. Keep the wording human and in the user's voice — quote his phrasing when he gives you an example. **Plain ASCII only in the note body and title: no emojis, no em dashes (use `--` or rewrite the sentence), no smart quotes, no non-ASCII punctuation.** Choose style: `info` for new features and improvements, `cta` when the operator must do something (verify a setting, reconfigure a channel, etc.). Prepend a new entry to `graywolf/pkg/releasenotes/notes.yaml` using the computed NEW version (one patch/minor above current VERSION), today's date, and a hash-route link to the relevant settings tab when applicable. The bump targets will refuse to run if no entry exists for the new version.
1. Run `make bump-point` for a patch release or `make bump-minor` for a minor release — pick based on the user's wording (default to patch if ambiguous). These targets handle VERSION, Cargo manifests, AUR files, regenerated docs, commit, tag, and push.
2. After the bump target completes, watch the GitHub Actions run at https://github.com/chrissnell/graywolf/actions (use `gh run list` / `gh run watch`) until every workflow finishes.
3. If any workflow fails:
   - Diagnose the failure (`gh run view <id> --log-failed`).
   - Fix the underlying issue in code.
   - Delete and re-tag the same version rather than bumping again (see memory `feedback_release_retag`): `git tag -d vX.Y.Z && git push origin :refs/tags/vX.Y.Z`, commit the fix, then re-tag and push.
   - Resume watching until all workflows pass.

   **Retag contract.** If a CI workflow fails after the tag is pushed and you delete+re-tag the same version per the retag flow above, **do not rewrite the release note** during the retag. Whatever text was in `graywolf/pkg/releasenotes/notes.yaml` at the final successful tag is what ships to operators on upgrade — silently rewording the user-facing changelog between retag attempts is a trust hazard. Only fix the underlying issue that caused CI to fail; leave the note alone.
4. Only report the release as complete once every workflow is green.
