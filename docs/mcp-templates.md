# MCP template candidates

Research date: 2026-10-17. This is a candidate survey, not a list of certified
integrations. Five initial v1 presets now ship in the catalogue (DeepWiki, GitHub
read-only, Context7, Hugging Face and W&B); see [template usage](mcp.md#versioned-templates-and-browser-links).
Live qualification and the remaining candidates are still planned. The four Release 1 integrations remain GitHub, Bitbucket Cloud,
Jira Cloud and Confluence Cloud; see [their contracts](mcp-integrations.md).

A template should prefill reviewed configuration and explain prerequisites. It
must not install a server, create credentials or grant an agent access implicitly.
Compatibility below is a planning assessment against [the current client](mcp.md),
not a live certification. DeepWiki has previously been exercised in the installed
Weft browser/gateway flow; no other candidate is certified by this survey.

## Software engineering

| Candidate / official source | Useful work | Connection and authentication | Weft delivery |
| --- | --- | --- | --- |
| [DeepWiki](https://docs.devin.ai/work-with-devin/deepwiki-mcp) | Understand public repositories and their architecture | `https://mcp.deepwiki.com/mcp`; remote Streamable HTTP, no credential | First template and repeatable public smoke test. Discover actual tool names rather than copying examples. |
| [Context7](https://context7.com/docs/resources/all-clients) | Library documentation and code examples | `https://mcp.context7.com/mcp`; HTTP with Bearer API key; separate `/mcp/oauth` OAuth endpoint | First wave, using encrypted Bearer credentials; live compatibility test pending. |
| [Sentry](https://mcp.sentry.dev/) | Investigate errors and performance | `https://mcp.sentry.dev/mcp`; remote OAuth; optional organization/project path | Sentry v1 supplies OAuth defaults and guidance. Live consent, per-tool scope and scoped-project qualification remain pending. |
| [Playwright](https://github.com/microsoft/playwright-mcp) | Browser interaction and UI investigation | Official server launched locally or in a container; no hosted public endpoint selected | Isolated managed-server track. Browser lifecycle, inbound access and resource limits must be designed before offering an executable template. |

## Data engineering

| Candidate / official source | Useful work | Connection and authentication | Weft delivery |
| --- | --- | --- | --- |
| [dbt](https://docs.getdbt.com/docs/dbt-ai/setup-remote-mcp) | Model metadata, lineage and semantic-layer context | `https://<dbt-host>/api/ai/v1/mcp/`; remote OAuth or token (`Authorization: Token` **or** `Bearer`); environment headers such as `x-dbt-prod-environment-id` | Typed additional headers, project/environment selection and per-tool credential requirements. Remote service does not replace local dbt CLI execution. |
| [Databricks](https://docs.databricks.com/aws/en/generative-ai/mcp/custom-mcp-usage) | Governed workspace data and managed services | `https://<workspace-host>/api/2.0/mcp/<service>/<path>`; OAuth user/service-principal credentials; privileges depend on selected service | Workspace/service v1 covers the current `/ai-gateway/mcp-services/<catalog>.<schema>.<service>` path, PAT testing and user OAuth guidance. Legacy managed paths, service-principal renewal and live service acceptance remain separate. |
| [BigQuery](https://docs.cloud.google.com/bigquery/docs/use-bigquery-mcp) | Dataset/table exploration and SQL | `https://bigquery.googleapis.com/mcp`; Google identity/IAM | Protocol qualification first: current documentation describes stateless 2026-07-28 without initialize/session lifecycle, unlike Weft 2025-06-18. Add IAM guidance and query-cost controls before SQL grants. |
| [Snowflake](https://docs.snowflake.com/en/user-guide/snowflake-cortex/cortex-agents-mcp) | Governed Cortex/data tools | Account-scoped `/api/v2/databases/<db>/schemas/<schema>/mcp-servers/<server>` endpoint; PAT/OAuth and role privileges | Qualify lifecycle/version differences documented by Snowflake before compatibility claims; then account/server/role setup and least-privilege profiles. |

## Machine learning and AI engineering

| Candidate / official source | Useful work | Connection and authentication | Weft delivery |
| --- | --- | --- | --- |
| [Hugging Face Hub](https://github.com/huggingface/hf-mcp-server) | Discover models, datasets, papers and Spaces | `https://huggingface.co/mcp`; remote HTTP, optional Bearer HF token | First wave with discovery/search profile; separately review extra Space tools, data transfer and compute capabilities. |
| [W&B Models / Weave](https://github.com/wandb/wandb-mcp-server) | Compare experiments, inspect metrics, traces and evaluations | `https://mcp.withwandb.com/mcp`; Bearer W&B API key | First wave, subject to protocol tests; start with read tools and project-scoped credentials. |
| [MLflow](https://mlflow.org/docs/latest/genai/mcp/) | Analyze GenAI traces and feedback | Documented local `mlflow mcp run`; tracking-server configuration; no hosted MCP URL selected | Managed isolated-server track with separate upstream authentication. Do not confuse the tracking URL with an MCP endpoint. |
| [LangSmith](https://github.com/langchain-ai/langsmith-mcp-server) | Inspect traces, datasets, prompts and evaluations | `https://langsmith-mcp-server.onrender.com/mcp`; `LANGSMITH-API-KEY`, optional workspace/endpoint headers | Encrypted custom-header support and workspace selection. Confirm each tool's actual capability; documentation tools do not imply remote execution of evaluations. |
| [Qdrant](https://qdrant.tech/documentation/qdrant-mcp-server/) | Semantic retrieval and agent memory | [Official server](https://github.com/qdrant/mcp-server-qdrant), stdio or Streamable HTTP; upstream Qdrant URL/API key | Managed isolated-server track. Database credentials do not authenticate inbound MCP clients; provision TLS/auth separately. Retrieval first, storage writes separately granted. |

## Proposed delivery order

1. Versioned template schema and UI, then DeepWiki, Context7, Hugging Face and W&B.
   These exercise no-auth/Bearer configurations without requiring new credential
   mechanisms. Only DeepWiki has existing installed-flow evidence.
2. Complete the four priority vendor contracts and guided authentication, then
   Sentry, dbt, LangSmith and Databricks as their dependencies become available.
3. Qualify BigQuery and Snowflake protocol differences; implement explicit
   compatibility profiles only after tests establish their contracts.
4. Design isolated MCP hosting for Playwright, MLflow and Qdrant. Keep arbitrary
   startup commands out of the Weft main process; private networking remains
   a separate explicit policy, not a template exception.

Each template needs an owner/source URL, review date, version, endpoint parameters,
transport/protocol profile, credential fields, scope guidance, data boundary,
read/write/cost classification and validation status. New discoveries never
silently expand existing agent grants. Template updates must show a configuration
diff before changing an existing connection.

Acceptance for each implemented template: unit tests for validation/defaults and
invalid inputs; integration tests with TLS, storage and gateway authorization;
browser E2E for setup, discovery, exact grants, invocation and revocation; an
opt-in live smoke test with authorized test resources. Cover expired/insufficient
credentials, timeouts, pagination, schema drift and redaction. A fixture success
is not proof of live vendor compatibility. Live write tests need explicit approved
test targets; no automatic production mutations or paid jobs.

## Opt-in public protocol qualification

On 2026-10-18 the production transport successfully initialized and discovered
three tools from DeepWiki and four anonymous tools from Hugging Face. A separate
DeepWiki check read the public `golang/go` documentation structure through the
real connection manager and tool gateway; the same call was denied without its
exact grant. Tests use temporary storage and synthetic encryption material, never
installed connections or credentials, and make no repository/account mutations.
Returned documentation is treated as data and is not written to the test log.

With a local Go toolchain and public HTTPS access, repeat explicitly from `server`:

```sh
WEFT_TEST_PUBLIC_MCP=1 go test ./internal/mcpconfig -run 'TestLive(PublicTemplateDiscovery|DeepWikiReadThroughGateway|Context7ReadThroughGateway)' -v -count=1
```

Normal offline tests skip these network checks. Availability and catalogue contents
may change. This evidence does not qualify authenticated Hugging Face/Space tools,
Context7 or W&B credentials, paid computation, private repositories or every remote
tool. Those acceptance cases remain pending with appropriate authorized accounts.
An anonymous Context7 `resolve-library-id` lookup for React also passed through
the production gateway on 2026-10-18, including denial without an exact grant.
No key was supplied; documentation content was omitted from logs. This qualifies
that lookup only, not `query-docs`, authenticated modes or private libraries.
[Context7 tools and anonymous limits](https://context7.com/docs/resources/all-clients)
describe the reviewed contract.

The [DeepWiki contract](https://docs.devin.ai/work-with-devin/deepwiki-mcp) and
[Hugging Face server](https://github.com/huggingface/hf-mcp-server) describe the
public endpoints used by these tests.

## Warehouse protocol qualification (2026-10-18)

These are candidates, not supported connection templates. The default transport
uses MCP 2025-06-18 with initialization, initialized notification and session
cleanup. Custom connections now have an explicit bounded stateless 2026-07-28
option (see [MCP configuration](mcp.md#explicit-stateless-transport)); production
transport discovery passed against the nine-tool public BigQuery catalogue.
This does not qualify account authentication, query execution or spending controls. Do not bypass a failed handshake automatically or infer compatibility
from a successful OAuth login.

### BigQuery

The [official MCP guide](https://docs.cloud.google.com/bigquery/docs/use-bigquery-mcp)
describes stateless 2026-07-28 requests, no initialization/session handshake,
request metadata/headers and possible multi-round-trip requests. IAM remains
independent of OAuth consent: review MCP Tool User, BigQuery Job User and narrowly
scoped Data Viewer permissions for the chosen identity and billing project.

A credential-free `tools/list` probe to `https://bigquery.googleapis.com/mcp` on
2026-10-18 returned HTTP 200 and nine tools using the advertised protocol and
`Mcp-Method: tools/list`. No tool was invoked or job submitted. The live
`execute_sql_readonly` schema requires `projectId` and `query`; it exposes
`dryRun`, `jobTimeoutMs`, `timeoutMs` and `labels`, but no maximum-billed-bytes
argument. This is catalogue evidence, not authenticated execution qualification.

Before delivery:

- The bounded stateless profile is implemented with metadata, response parsing
  and explicit unsupported multi-round-trip errors, preserving DNS/TLS, redirect,
  credential-redaction and no-replay guarantees. Extend only with qualified
  custom-header/input-request contracts.
- Revalidate tool schemas at discovery; never forward arbitrary schema-specified
  HTTP headers or accept agent-controlled billing projects without policy.
- Separate metadata, read-query, write-query and cancellation grants. Read-only
  SQL can still consume paid resources. A dry run estimates work; it is not a
  reservation or enforceable spending ceiling. A client timeout does not prove
  that a remote job stopped.
- Require explicit billing-project/data boundaries and provider-enforced cost
  controls before query execution; do not invent a maximum-bytes parameter.
- Test denial, expiry, schema drift, asynchronous completion, cancellation and
  uncertain outcomes with fixtures, then authorized disposable cloud resources.

### Snowflake

The [managed MCP guide](https://docs.snowflake.com/en/user-guide/snowflake-cortex/cortex-agents-mcp)
excludes MCP lifecycle phases, notifications and version negotiation. It supports
up to 50 tools and may truncate SQL/generic results at 250 KB. OAuth can use
Snowflake or an explicitly bound external identity provider; discover the chosen
resource's issuer rather than assuming the account URL. Access also requires
appropriate server/object privileges, a default role and default warehouse with
warehouse usage. No account, credentials or live lifecycle were tested here.

Before delivery:

- Capture the chosen account's request/response contract in fixtures before
  adding a lifecycle-free profile; do not reuse BigQuery semantics by analogy.
- Validate account/database/schema/server parameters and exact resource/issuer
  binding. Keep private networking subject to installation policy.
- Document role and warehouse prerequisites, underlying object permissions,
  credential renewal and revocation for the chosen authentication method.
- Classify actual discovered tools and preserve exact agent grants. SQL/custom
  tools need independent data and spending controls; truncation is visible,
  never evidence that all rows were returned.
- Test missing privileges, warehouse failures, truncation, expired credentials
  and uncertain execution outcomes before an authorized account acceptance run.

Neither provider currently has a Weft query-cost guarantee. A template,
fixture or public catalogue must not mark these authenticated journeys complete.

### Context7 anonymous discovery

The [official server guide](https://github.com/upstash/context7/blob/master/packages/mcp/README.md)
also documents basic remote access without an API key. On 2026-10-18 the opt-in
public discovery test passed against the existing Context7 endpoint with two
tools and no credential. The immutable v1 preset still offers Bearer-key setup;
this evidence does not change saved connections or claim private-repository,
authenticated, OAuth or tool-execution acceptance. Rate limits still apply.
