## Summary

<!-- What changed and why. One paragraph max. Link issues with "Closes #". -->

Closes #

## Scope

<!-- Check ALL areas this PR touches. Reviewers and CI use this to gauge blast radius. -->

- [ ] `client.go` — Core client / surface API
- [ ] Resource files (filings, statements, entity, ownership, etc.)
- [ ] `examples/` — Example programs
- [ ] `go.mod` / `go.sum` — Dependencies
- [ ] `README.md` / docs
- [ ] `VERSION` — Release version
- [ ] `.github/` — CI/CD workflows
- [ ] Tests (`*_test.go`)

## Changes

<!-- Bullet points grouped by area. Be specific — diffs are for code, this is for intent. -->

-
-

## Verification

<!-- What you ran locally. Paste actual commands and their outcomes. -->

```bash
go build ./...   # ✅ / ❌
go test ./...    # ✅ / ❌
go vet ./...     # ✅ / ❌
gofmt -l .       # ✅ / ❌  (no diff)
```

<details>
<summary>Additional verification (expand if applicable)</summary>

```bash
# Run an example end-to-end
SECAPI_API_KEY=... go run ./examples/basic

# Linting
golangci-lint run

# Race detector
go test -race ./...
```

</details>

## Deployment Impact

<!-- Skip this section entirely for code-only changes with no release impact. -->

- [ ] New version tag required (bump `VERSION`)
- [ ] Breaking API change (semver major)
- [ ] Docs (README / examples) updated to match
- [ ] Companion docs PR in `secapi-ai/.github` or org docs site

## Completion Attestation

<!-- You MUST select one. This is a binding statement of delivery status. -->

- [ ] **100% complete, 100% functional.** All code is written, tested, and works end-to-end against live SEC API. No outstanding work remains.
- [ ] **Not fully complete or functional.** Deltas listed below.

### Deltas (only if attesting incomplete)

<!-- Short bullets. Items intentionally deferred from this PR's stated scope. -->

-

## Screenshots / Demo

<!-- Terminal output, CLI snippets, or API response examples. Delete section if not applicable. -->

---

<details>
<summary>Agent Context</summary>

<!-- This section is for AI coding agents that may continue or review this work.
     Fill in what's relevant; delete what isn't. -->

**Key files to read first:**
<!-- List the 3-5 most important files for understanding this PR's changes. -->
- `client.go`
-

**Decisions made:**
<!-- Non-obvious choices and why. Agents should not re-litigate these. -->
-

**Relevant docs:**
- `README.md`
- https://docs.secapi.ai

**Conventions applied:**
<!-- Idiomatic-Go conventions, error handling, naming, response metadata fields. -->
-

</details>
