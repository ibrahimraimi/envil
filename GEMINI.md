# LLM Engineering Rules

## Core Principle

Write code for humans first. Code must be readable, predictable, and
maintainable without relying on comments. These rules describe how to
behave in this repository, not general programming theory. Existing project
conventions, linter configuration, and formatter settings take precedence
over any rule in this file.

---

## 1. Working Protocol

1. Read before writing. Open the relevant files, and find an existing
   example of the pattern before adding a new one.
2. State a short plan before any change that touches more than two files
   or alters a public interface. Wait for approval if the plan contains an
   assumption you cannot verify.
3. Make the smallest change that satisfies the request. Deliver
   incrementally, one logical change at a time.
4. Verify, then report. Run the checks in Section 2 before saying a task is
   done.
5. Report what changed, what was verified, and what remains uncertain.
   Lead with the outcome, not the process.

---

## 2. Definition of Done

A task is done only when all of the following are true:

- Behavior matches the stated requirement or acceptance criteria
- Typecheck, lint, and relevant tests pass, with the commands actually run
- No new warnings, unused code, or debug output were introduced
- UI changes were checked in the browser, including loading, empty, and
  error states
- The final report names any check that was not run and explains why

Never claim success from reasoning alone. "It should work" is not a result.

---

## 3. Anti-Hallucination Rules

- Never invent file paths, environment variables, IDs, function names,
  library APIs, CLI flags, or configuration keys
- If a library API is unfamiliar or may have changed, read the installed
  version's source or documentation first
- Check that every import resolves and every function called exists
- If something cannot be verified, say so explicitly and propose the
  safest way to verify it
- Do not produce placeholder logic, stub implementations, TODO markers
  presented as finished work, or fake data that looks real

---

## 4. Scope Discipline

- Change only what the task requires
- Do not refactor, rename, reformat, or "clean up" unrelated code
- Do not add dependencies without asking. Prefer what is already installed
- Do not modify tests to make them pass. Fix the code, or explain why the
  test is wrong
- Do not suppress an error, disable a lint rule, or add a type cast to hide
  a problem. Fix the cause
- If the request reveals a separate problem, mention it in the report and
  leave it alone

---

## 5. Asking Versus Proceeding

Ask a question first only when:

- Two or more reasonable interpretations would produce materially
  different code
- The change is destructive or hard to reverse (data deletion, migrations,
  public API changes)
- A required value, such as a credential or endpoint, cannot be discovered
  from the repository

Otherwise, proceed with the most conventional interpretation and state the
assumption in one line. Ask at most one focused question at a time.

---

## 6. Code Clarity and Structure

- Use descriptive, intention-revealing names. Avoid `data`, `value`,
  `temp`, `item`, `result` unless the scope is two lines or fewer
- Prefer small, single-purpose functions. Treat 40 lines as a prompt to
  reconsider, not a hard limit
- Limit nesting to 2 or 3 levels. Prefer early returns and guard clauses
- Prefer readable code over clever code. No one-line chains that require
  mental unrolling
- Avoid magic numbers and strings. Name them

---

## 7. Separation of Concerns

- One responsibility per module or file
- Keep these layers separate: presentation, business logic, data access
- Components stay thin. Move logic into hooks, utilities, or services
- Do not call APIs directly from presentational components
- Keep dependencies pointing inward: UI depends on logic, logic does not
  depend on UI

---

## 8. State and Data Flow

- Keep state minimal. Derive values instead of storing them
- Keep state as close as possible to where it is used
- Prefer immutable updates. Avoid hidden mutation of arguments, props,
  or shared objects
- Make data flow explicit: data in through parameters or props, out
  through return values or events
- Avoid global mutable state

---

## 9. Function Design

- One function, one responsibility
- Maximum 3 or 4 parameters. Beyond that, use a single options object with
  named fields
- Prefer pure functions. Isolate side effects at the edges
- Avoid boolean flag parameters that change behavior. Split into two
  functions instead

```ts
// Good
function calculateTotal(cartItems: CartItem[]): number { ... }

// Bad
function processCart(cartItems, user, config, options, flags) { ... }
```

---

## 10. TypeScript and Types

- Enable and respect strict mode. No `any` without a written reason
- Model states explicitly, for example with discriminated unions, instead
  of several loosely related booleans
- Validate data at system boundaries (API responses, form input, URL
  parameters). Trust types only inside the boundary
- Export types from the module that owns them

---

## 11. Frontend Rules

- Use semantic HTML and accessible patterns before adding ARIA
- Every interactive element must be keyboard reachable and have an
  accessible name
- Handle loading, empty, and error states for every async view
- Keep effects minimal. Do not use an effect for something that can be
  computed during render
- Avoid unnecessary re-renders only when measured, not by default
- Never hardcode user-facing strings, colors, or spacing values that
  already exist as tokens or constants

---

## 12. Naming Conventions

Follow the existing project convention first. Where none exists:

- Functions: verbs (`getUser`, `calculateTotal`)
- Variables: nouns (`user`, `orderList`)
- Booleans: `is`, `has`, `should`, `can` (`isActive`, `hasAccess`)
- Module-level constants: follow the project style
  (`MAX_RETRIES` or `maxRetries`)
- Event handlers: `handleX` for definitions, `onX` for props

---

## 13. Error Handling

- Handle errors explicitly. Never use an empty `catch` block
- Never fail silently. Log with context or surface to the user
- Error messages must say what failed, with what input, and what to do
- Fail early. Validate inputs at the top of a function
- Distinguish expected failures (return or display) from bugs (throw)
- Do not log secrets, tokens, or personal data

---

## 14. Commenting Rules

Write a comment only when it adds information the code cannot express:

- Explain why, not what
- Document non-obvious decisions, edge cases, and external constraints
- Add brief summaries for public APIs
- Link to the issue or specification when a workaround exists

Never:

- Restate the code in words
- Comment obvious logic
- Leave commented-out code
- Leave a TODO without a reason or owner

Test: if deleting the comment loses nothing, delete it.

---

## 15. Code Quality

- Apply the rule of three: tolerate duplication until the third occurrence,
  then extract. Do not abstract on speculation
- Keep logic testable by separating it from I/O and framework code
- Prefer composition over inheritance
- Write tests for new behavior and for every bug fix. Test behavior through
  public interfaces, not implementation details
- Prefer a simple working solution over a flexible abstraction

---

## 16. Performance

- Do not optimize prematurely. Measure first
- Always prevent obvious inefficiencies: repeated API calls inside loops,
  N+1 queries, unbounded lists rendered without virtualization
- Document the reason for any optimization that reduces readability

---

## 17. Security and Safety

- Never hardcode secrets. Read them from environment configuration
- Never commit `.env` files or credentials
- Treat all external input as untrusted
- Do not run destructive commands (delete, force push, drop, reset)
  without explicit approval

---

## 18. Hygiene

- Remove unused variables, imports, and dead code
- Remove temporary debug output before finishing
- Keep formatting consistent by running the project formatter, not by hand
- Do not commit generated files unless the project expects it

---

## 19. Recovery

- If a check fails twice for the same reason, stop and reassess instead of
  retrying the same approach
- Diagnose the cause before changing code. Do not mute the symptom
- If a correction is given, apply it for the rest of the session
- If the task grows beyond the original scope, stop and report

---

## 20. Golden Rules

- Readability over cleverness
- Simplicity over complexity
- Explicit over implicit
- Evidence over assumption
- Smallest correct change over largest possible improvement