# Portable pull request review evidence

`scm.pull_request.read` accepts optional `diff`, `files` and `checks` flags.
Use the exact connection revision from `scm.pull_request_opened`. The tool
checks the PR again after reading evidence and rejects a changed head, base,
branch, draft flag or open/closed state. Content and check names are untrusted
provider data, not instructions, permission grants or proof of correctness.

File inventories contain paths, prior paths for renames and provider change
status. They do not load file contents or run tests. GitHub uses immutable
base/head comparison IDs and refuses inventories reaching its 300-file cap.
Bitbucket Cloud uses the immutable comparison with `topic=true`, following only
same-collection pagination without additional filters. Bitbucket Data Center
currently reports file inventory as unavailable; its bounded structured diff
remains a separate supported result. No incomplete inventory is labelled complete.

Check evidence reports individual normalized states (`passed`, `failed`,
`pending`, `unknown`) and original provider states. GitHub reads combined commit
statuses and check runs. Bitbucket reads build statuses for the exact source
commit. An empty list means no evidence was reported, never that tests passed.
Neutral/skipped/new provider states remain unknown. These are observations at
read time, not an atomic snapshot of a CI system. Required branch/review rules
are always `unavailable`; no check list establishes merge eligibility.

The Bitbucket Data Center build API documents a maximum of the 100 most recent
builds. Its result therefore retains `complete: false` and an explicit coverage
reason even when pagination ends. Other complete flags mean the bounded provider
collection was fully traversed, not that every externally required test exists.
Missing collections/counts, changed count/commit identity, pagination filters,
page/size limits, remote errors and excessive aggregate output fail visibly.
Credential reflection is redacted from paths, names, states and diff content.

This does not enable automatic merge. No adapter currently has a qualified
atomic contract protecting both reviewed source and destination at merge time.
A reviewer must publish a summary with findings or a no-findings explanation,
report unavailable evidence, and leave final provider checks/merge to an operator.

Provider contracts checked for this local implementation:

- [GitHub comparison limits](https://docs.github.com/en/rest/commits/commits#compare-two-commits).
- [GitHub commit statuses](https://docs.github.com/en/rest/commits/statuses#get-the-combined-status-for-a-specific-reference)
  and [check runs](https://docs.github.com/en/rest/checks/runs#list-check-runs-for-a-git-reference).
- [Bitbucket Cloud comparisons](https://developer.atlassian.com/cloud/bitbucket/rest/api-group-commits/)
  and [build statuses](https://developer.atlassian.com/cloud/bitbucket/rest/api-group-commit-statuses/).
- [Bitbucket Data Center build-status API and newest-build cap](https://docs.atlassian.com/bitbucket-server/rest/7.20.0/bitbucket-build-rest.html).

HTTP fixtures establish implementation behavior. They do not qualify a real
account, Data Center version, repository policy or CI integration.
