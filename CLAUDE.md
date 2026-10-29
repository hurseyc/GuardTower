# Working on GuardTower with Claude Code

Status, milestones and how to resume: [`docs/dev/PROGRESS.md`](docs/dev/PROGRESS.md).

## Owner's standing instructions

- **Merge when green.** Merge PRs (ours and Dependabot's) once every CI check passes and the PR is mergeable. Run each merge as its own `gh api -X PUT repos/hurseyc/GuardTower/pulls/<n>/merge …` command (matches the project permission rule in `.claude/settings.json`).
- **Always publish releases when the release PR is green.** release-please opens `chore(main): release X.Y.Z`; merge it once main's CI is green, then confirm the Release workflow uploaded the assets and the image.
- Keep going to the next planned step after a merge; stop at a clean checkpoint (PROGRESS.md updated, pushed) before a usage limit.

## Lean git workflow

- **One PR per feature.** Planning notes, PROGRESS.md and other docs updates ride along in the feature's PR; don't open a PR just for them.
- **Auto-merge.** Right after opening a PR, turn on auto-merge (`gh api -X PUT repos/hurseyc/GuardTower/pulls/<n>/ccr/auto_merge -f merge_method=squash`); GitHub merges it when the required checks pass. Only watch the PR (subscribe) to fix red CI; no scheduled check-ins for green-path waiting. If auto-merge can't be enabled, fall back to merging when green as above.
- **Docs-only changes** (`docs/`, `*.md`, `mkdocs.yml`) run only the `changes` and `docs` CI jobs; the docs job also runs the Go and web doc gates.
- **Open PRs with the REST API** (`gh api repos/hurseyc/GuardTower/pulls -f base=main -f head=<branch> …`); `gh pr create` uses GraphQL, which cloud sessions can't reach.
- **New branches in a cloud session:** the clone only tracks the branches it was created with, so run `git config --replace-all remote.origin.fetch '+refs/heads/*:refs/remotes/origin/*'` once, then `git push -u origin <branch>`.

## Checks before pushing

- Go: `go test -race ./...`, `golangci-lint run`, `go test -tags integration ./internal/integration/...`
- Web (in `web/`): `npx tsc -b`, `npm run lint` (warnings fail), `npx vitest run`, Playwright (`PW_CHROMIUM_PATH=/opt/pw-browsers/chromium npx playwright test` after `make build`)
- Docs: `python3 scripts/mkdocs_nav.py && mkdocs build --strict`; every UI route, setting and `GT_*` variable must be documented (tests enforce it).
