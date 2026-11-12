# RUN-007 verification evidence

The maintainer approved D-01 through D-09 on 2026-11-12. Implementation
verification is pending. The matrix records the fixture-based coverage
required for each criterion.

## Pre-implementation review on 2026-11-12

The current GitHub MCP trigger adapter can search, replace the full label
list and post comments. D-03 requires evidence of who added `weft:todo`
and whether that human is a maintainer. A label snapshot cannot establish
either fact. D-07 also requires preserving unrelated concurrent label
changes. The GitHub REST API exposes issue timeline, repository permission
and individual label operations. The maintainer needs to select how the
trigger gateway will obtain these operations under reviewed consent.
[Current adapter](../../../server/internal/trigger/issue.go),
[GitHub MCP issue tools](https://github.com/github/github-mcp-server/blob/main/pkg/github/issues.go),
[Timeline events](https://docs.github.com/en/rest/issues/timeline),
[Repository permissions](https://docs.github.com/en/rest/collaborators/collaborators#get-repository-permissions-for-a-user),
[Issue label operations](https://docs.github.com/en/rest/issues/labels).

The RUN-006 closure rule also requires a decision when the issue source
and PR target are different repositories. See the
[RUN-006 pre-implementation review](../issue-references-and-polling-triggers/evidence.md#pre-implementation-review-on-2026-11-12).

| Verification | Result on the specifications-only revision |
| --- | --- |
| Specification and matrix inventory | All six RUN-007 criteria, four RUN-006 criteria and nine maintainer decisions are represented. |
| `go test -count=1 ./...` and `go test -count=1 -tags e2e ./cmd/weft-e2e` | Passed in the cached Linux Go image. |
| `./scripts/go.sh vet ./...`, `gofmt -l .` | Vet passed and gofmt returned no files. |
| `go build ./...` with `GOOS=linux` and `GOOS=darwin` | Both arm64 builds passed. |
| `pnpm -C web test`, `pnpm -C web exec tsc --noEmit`, `pnpm -C web exec tsc --project tsconfig.e2e.json` | 444 Vitest tests and both type checks passed. |
| `pnpm -C web exec playwright test e2e/activity.spec.ts e2e/triggers.spec.ts`, `pnpm -C web exec playwright test --config playwright.activity.config.ts` | Six Activity scenarios and one trigger scenario passed. |

The checks establish the pre-implementation baseline. Qualification of the
new behavior remains pending.

The maintainer subsequently approved the specific tracker API gateway
operations with reviewed consent and fixture verification, and qualified
GitHub closure references for cross-repository delivery.
