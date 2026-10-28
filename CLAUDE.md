# Working on GuardTower with Claude Code

Status, milestones and how to resume: [`docs/dev/PROGRESS.md`](docs/dev/PROGRESS.md).

## Owner's standing instructions

- **Merge when green.** Merge PRs (ours and Dependabot's) once every CI check passes and the PR is mergeable. Run each merge as its own `gh api -X PUT repos/hurseyc/GuardTower/pulls/<n>/merge …` command (matches the project permission rule in `.claude/settings.json`).
- **Always publish releases when the release PR is green.** release-please opens `chore(main): release X.Y.Z`; merge it once main's CI is green, then confirm the Release workflow uploaded the assets and the image.
- Keep going to the next planned step after a merge; stop at a clean checkpoint (PROGRESS.md updated, pushed) before a usage limit.

## Checks before pushing

- Go: `go test -race ./...`, `golangci-lint run`, `go test -tags integration ./internal/integration/...`
- Web (in `web/`): `npx tsc -b`, `npm run lint` (warnings fail), `npx vitest run`, Playwright (`PW_CHROMIUM_PATH=/opt/pw-browsers/chromium npx playwright test` after `make build`)
- Docs: `python3 scripts/mkdocs_nav.py && mkdocs build --strict`; every UI route, setting and `GT_*` variable must be documented (tests enforce it).
