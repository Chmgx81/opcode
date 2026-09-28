# AGENTS.md

> Instructions for any AI coding agent working in this repository.
> Read this fully before writing code. When in doubt, choose the simpler, safer, more honest option.

**Scaling rule:** Not every project needs every section. For a small script, sections 1, 5, and 9 matter most. For a real product with real users, use all of it. Never skip Section 11 (Avoid Slop) or Section 9 (Security) — those apply everywhere, always.

---

## 0. The 60-Second Version

If you read nothing else, read this:

1. **Understand before you build.** Restate the goal in your own words if it's non-trivial.
2. **Plan before you code.** For anything bigger than a one-file fix, write a short plan first.
3. **Small steps, working state.** Never leave the project in a broken state between steps.
4. **No fake work.** No placeholder data pretending to be real, no fake tests that always pass, no invented APIs.
5. **Say what you don't know.** Flag assumptions and uncertainty instead of guessing silently.
6. **Test what you build.** If you can't verify it works, say so — don't claim it does.
7. **Leave it better than you found it.** Clean up after yourself: dead code, stray files, unused imports.
8. **Security is not optional.** Never hardcode secrets, never trust user input, never skip auth checks "for now."

---

## 1. Core Principles

- **Simplicity beats cleverness.** The best code is code a tired human can understand at 2am.
- **Correctness beats speed.** A slow correct answer beats a fast wrong one.
- **Boring beats novel.** Use well-known patterns and libraries over exotic ones unless there's a real reason.
- **Explicit beats implicit.** Prefer clear, obvious code over "magic."
- **Working software is the measure of progress**, not lines of code or files created.
- **Every abstraction must earn its place.** Don't build for imagined future requirements ("YAGNI" — You Aren't Gonna Need It).
- **Optimize for the next reader** (human or agent) — not for the current writer's convenience.

---

## 2. Spec-Driven Development

Before writing non-trivial code, produce a short spec. This prevents wasted work and scope drift.

### When you need a spec
- New feature, new service, or anything touching more than ~3 files → **write one**
- One-line bug fix or trivial copy change → **skip it, just do it**

### Minimal spec template
```
## Goal
What problem does this solve, in one or two sentences?

## Non-Goals
What is explicitly out of scope?

## Approach
How will this be built? Key decisions and why.

## Edge Cases
What could go wrong? What inputs break naive versions?

## Test Plan
How will we know this works?
```

### Rules
- If requirements are ambiguous, **state your assumption and proceed** — don't stall on questions you can reasonably answer yourself.
- Ask a clarifying question **only** when guessing wrong would mean wasted work in the wrong direction (e.g., wrong database, wrong auth model, destructive action).
- Keep specs in `/specs/` or `/docs/specs/` for anything meant to outlive the current session.

---

## 3. Project Structure

Pick a structure and be consistent — don't mix conventions.

```
/src            — application code
/tests          — automated tests, mirroring /src structure
/docs           — architecture notes, decisions, specs
/scripts        — one-off or maintenance scripts
README.md       — what this is, how to run it, how to test it
AGENTS.md       — this file
CHANGELOG.md    — human-readable log of notable changes (optional for small projects)
```

### Naming
- Files and folders: consistent case (pick `kebab-case` or `snake_case`, not both).
- Names describe **what**, not **how**: `user-repository.ts`, not `db-helper-2.ts`.
- No `final`, `final_v2`, `temp`, `old`, `new` in filenames. Use version control for history, not filenames.

### If This Project Uses Skills or MCP
Keep responsibilities separate so instructions don't duplicate or conflict:
- **AGENTS.md** (this file) — stable, always-loaded conventions: stack, build/test commands, structure. Rarely changes.
- **Skills** — on-demand, step-by-step procedures for specific tasks, loaded only when relevant. Keep each skill's own instructions short; move large reference material into the skill's own reference folder instead of bloating either the skill or this file.
- **MCP servers** — reach into external tools/data (databases, APIs, filesystems). Scope every server to the minimum access it needs, and review a server's source before connecting it if it isn't one you wrote yourself.

Agent config that lives in the repo (skills with scripts, MCP server lists) runs on every collaborator's machine, so treat it like a build script: review changes to it in PRs, and never put credentials in it. Keys belong in user-level config or the environment.

If the same instruction could live in more than one place, put it in the one with the narrowest scope: a project-wide convention belongs here, a specific multi-step workflow belongs in a Skill — not both.

---

## 4. Progress Tracking

For any multi-step or multi-session task, keep visible state so work can be resumed or reviewed.

- Maintain a `PROGRESS.md` or `TASKS.md` for in-flight work with:
  - What's done
  - What's in progress
  - What's next
  - Any blockers or open questions
- Commit in small, logical, working chunks — not one giant commit at the end.
- Write commit messages that explain **why**, not just what:
  - Good: `fix: prevent race condition on double-submit in checkout`
  - Bad: `fix bug`
- Never mark something "done" that hasn't been verified. If it's untested, say **"implemented, not yet verified."**

---

## 5. Code Style & Quality

- **Functions do one thing.** If you need "and" to describe a function, split it.
- **Names are honest.** `getUser()` should not also delete a session as a side effect.
- **Comments explain why, not what.** The code already says what it does; comment on non-obvious reasoning, trade-offs, or gotchas.
- **No dead code.** Delete it — don't comment it out "just in case." Version control remembers it for you.
- **Consistent formatting.** Use the project's existing linter/formatter config. If none exists, pick a standard one for the language and apply it everywhere.
- **Fail loudly in development, gracefully in production.** Don't silently swallow errors.
- **DRY, but not paranoid.** Two similar-looking pieces of code aren't automatically duplication — don't force a shared abstraction until a third real use case shows up.
- **Match the existing style of the codebase** over your personal preference, unless asked to refactor.

---

## 6. System Design Guidelines

- **Single Responsibility** at every layer: modules, classes, services.
- **Define boundaries clearly.** What talks to what, and through which interface — not through shared internal state.
- **Config, not hardcoding.** Environment-specific values (URLs, keys, limits) belong in config/env, never inline.
- **Design for failure.** Every network call, file read, or external dependency can fail — decide what happens when it does, don't assume happy path.
- **Idempotency where it matters.** Retried operations (payments, writes, emails) shouldn't double-apply.
- **Data model first for anything with persistence.** Get the shape of the data right before building features on top of it.
- **Scale only when there's a real signal you need to** — don't build for a million users on day one of a project with zero users. But don't paint yourself into an obviously unscalable corner either (e.g., loading an entire table into memory).

---

## 7. UI/UX Engineering & Design Thinking

Applies to anything with a user-facing surface — web, mobile, CLI, or API responses that humans read.

### Design thinking loop (lightweight, even for small features)
1. **Who is this for, and what are they trying to do?**
2. **What's the simplest interface that lets them do it?**
3. **Build a rough version.**
4. **Would a first-time user get stuck? Fix that before polishing anything else.**

### Non-negotiables
- **Every state is designed:** loading, empty, error, and success — not just the happy path.
- **Errors are human-readable.** Never show a raw stack trace or `Error: undefined` to an end user.
- **Accessibility is a baseline, not a nice-to-have:** semantic HTML, keyboard navigation, sufficient color contrast, alt text on meaningful images.
- **Mobile/responsive by default** for anything web-facing, unless explicitly desktop-only.
- **Consistency over novelty:** reuse the same spacing scale, color tokens, and components rather than inventing new ones per screen.
- **Perceived performance matters:** show a skeleton or spinner for anything taking over ~300ms; never leave the user staring at a frozen screen wondering if it worked.
- **Copy is part of the design.** Button labels, error messages, and empty states should be written in plain, specific language ("No projects yet — create your first one" beats "No data").

---

## 8. Testing

- **Test the critical path always**, even on a small project: the core thing this software is supposed to do must be tested.
- **Test pyramid, roughly:**
  - Many small, fast unit tests for logic
  - Fewer integration tests for how pieces work together
  - A handful of end-to-end tests for the paths users actually take
- **Test edge cases and failure modes**, not just the ideal input.
- **A test that can't fail is worse than no test.** Never write a test that trivially passes regardless of the implementation.
- **Don't fake it.** No hardcoded "expected" outputs copied from what the buggy code currently produces just to make a test pass.
- **Run tests before declaring something done.** If you can't run them in this environment, say so explicitly rather than assuming they'd pass.

---

## 9. Security Checklist

Non-negotiable, on every project regardless of size:

- **Never commit secrets.** No API keys, passwords, or tokens in code, config files, or commit history. Use environment variables or a secrets manager.
- **Validate and sanitize all external input** — form fields, query params, file uploads, webhook payloads, API bodies. Assume all of it is hostile until proven otherwise.
- **Parameterize queries.** Never build SQL (or any query) via string concatenation with user input.
- **Least privilege everywhere:** database users, API keys, service accounts, file permissions — grant only what's needed.
- **Authentication and authorization are checked on every request that needs them** — not just in the UI, but at the API/data layer too.
- **Keep dependencies current** and avoid pulling in unmaintained or unnecessary packages.
- **Don't roll your own crypto** or auth token scheme — use established, audited libraries.
- **Log security-relevant events** (auth failures, permission denials) without logging sensitive data (passwords, tokens, full card numbers) in plaintext.
- **HTTPS/TLS everywhere** in anything that leaves localhost.

---

## 10. Production Readiness (for anything real users will touch)

- **Observability:** meaningful logs, error tracking, and basic metrics — so when something breaks, you can find out why without guessing.
- **Graceful degradation:** if a non-critical dependency (analytics, a recommendation service) goes down, the core product should still work.
- **Concurrency-safe:** consider what happens when two users or two requests hit the same resource at once.
- **Migrations, not manual edits:** schema and data changes go through versioned migrations, not ad-hoc production edits.
- **Environment parity:** dev, staging, and production should behave the same way modulo scale and secrets — no "works on my machine" logic.
- **Rate limiting and abuse protection** on any public-facing endpoint that costs money or resources to serve.
- **Backups exist and have been tested for restore**, not just for creation, for anything holding real user data.
- **Rollback plan exists** for any risky deploy.

---

## 11. Avoiding Slop

This is where most AI-generated code quietly loses trust. Watch for these specifically:

- **No fabricated APIs, libraries, or config options.** If you're not sure a method/library/flag exists, verify it or say you're unsure — don't invent a plausible-sounding one.
- **No placeholder data dressed up as real.** `"Lorem ipsum"` or `example@example.com` is fine in an obvious mock; silently fabricated "realistic-looking" data pretending to be live data is not.
- **No padding.** Don't add extra files, abstractions, config options, or "flexibility" nobody asked for. More code is not more value.
- **No silent TODOs.** If something is incomplete, say so out loud in your summary — don't bury a `// TODO: handle this properly` and move on as if it's done.
- **No copy-pasted boilerplate you don't understand.** If you include it, you should be able to explain what every part does.
- **No overconfident claims.** Don't say "this is fully tested and production-ready" unless it actually is. Say what you're sure of and what you're not.
- **No unnecessary comments or docstrings that just restate the code** (`// increment i by 1` above `i++`).
- **Don't reinvent what a well-known library already does well** — but also don't pull in a huge dependency for something you can write in 10 honest lines.
- **Match the ask.** If asked for a small fix, don't quietly refactor the whole file.

---

## 12. Communication Style

- **State your assumptions** when you make one, in a sentence, not a wall of caveats.
- **Summarize what changed and why**, not a line-by-line narration of every edit.
- **Flag risk and uncertainty plainly**: "this works for the common case, but I haven't handled X" is more useful than silence or false confidence.
- **Ask at most one focused question** when something is genuinely blocking — and only after trying to find a reasonable default yourself.
- **Don't over-explain simple things** or under-explain complex, risky ones. Match detail to stakes.

---

## 13. Definition of Done

A task is done when, honestly:

- [ ] It does what was asked — nothing more, nothing less
- [ ] It handles realistic bad input, not just the happy path
- [ ] It's been tested (automated where possible; manually verified otherwise, and say so)
- [ ] No secrets, no hardcoded environment-specific values
- [ ] No dead code, no debug leftovers, no stray files
- [ ] Errors are handled and surfaced usefully, not swallowed
- [ ] It matches the existing code style and structure
- [ ] Anything incomplete or uncertain is called out explicitly, not hidden

---

## 14. Scaling This File to Your Project

| Project size | Sections that matter most |
|---|---|
| Quick script / prototype | 1, 5, 9, 11 |
| Small app, few users | 1–6, 8, 9, 11, 13 |
| Real product, real users | All sections |
| Team / multi-agent codebase | All sections + strict adherence to 3, 4, 12 for coordination |

When this file conflicts with a more specific instruction file in a subfolder (e.g., a nested `AGENTS.md` or `README.md` for a subsystem), **the more specific instruction wins** for that subsystem — but security (Section 9) and the no-slop rules (Section 11) always apply regardless.