# Graywolf — Claude Code Instructions

## Release workflow

When the user asks to release, cut a release, bump, tag, or any equivalent phrasing:

1. Run `make bump-point` for a patch release or `make bump-minor` for a minor release — pick based on the user's wording (default to patch if ambiguous). These targets handle VERSION, Cargo manifests, AUR files, regenerated docs, commit, tag, and push.
2. After the bump target completes, watch the GitHub Actions run at https://github.com/chrissnell/graywolf/actions (use `gh run list` / `gh run watch`) until every workflow finishes.
3. If any workflow fails:
   - Diagnose the failure (`gh run view <id> --log-failed`).
   - Fix the underlying issue in code.
   - Delete and re-tag the same version rather than bumping again (see memory `feedback_release_retag`): `git tag -d vX.Y.Z && git push origin :refs/tags/vX.Y.Z`, commit the fix, then re-tag and push.
   - Resume watching until all workflows pass.
4. Only report the release as complete once every workflow is green.
