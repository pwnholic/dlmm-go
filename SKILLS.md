# Digest Skill Go (samber/cc-skills-golang)

Artefak turunan, bukan pengganti. Sumber: `~/.agents/skills/golang-*` @ `19a0626`.
Dibuat untuk membuat seluruh korpus 46 skill + 223 file reference dapat dirujuk tanpa memuat 1,7 MB mentah ke context.

**Cakupan:** 46 skill · 177 file reference · 507258 byte SKILL.md ditransformasi.

Cara pakai: telusuri aturan di sini dulu; `read` file SKILL.md/reference aslinya saat aturan itu benar-benar mengikat keputusan.

---

## Code Quality

### `golang-code-style`
> Golang code style conventions — line length and breaking, variable declarations, control flow clarity, when comments help vs hurt. Use when writing or reviewing Go code, asking about style or clarity, or establishing project coding standards. Not for naming conventions (→ See `samber/cc-skills-golang@golang-naming` skill), linter configuration (→ See `samber/cc-skills-golang@golang-lint` skill), o…

- No rigid line limit, but lines beyond ~120 characters MUST be broken. Break at semantic boundaries, not arbitrary column counts. Function calls with 4+ arguments MUST use one argument per line — even when the prompt asks for single-line code:
- SHOULD use `:=` for non-zero values, `var` for zero-value initialization. The form signals intent: `var` means "this starts at zero."
- Slices and maps MUST be initialized explicitly, never nil. Nil maps panic on write; nil slices serialize to `null` in JSON (vs `[]` for empty slices), surprising API consumers.
- users := []User{}                       // always initialized
- m := map[string]int{}                   // always initialized
- Do not preallocate speculatively — `make([]T, 0, 1000)` wastes memory when the common case is 10 items.
- Composite literals MUST use field names — positional fields break when the type adds or reorders fields:
- Errors and edge cases MUST be handled first (early return). Keep the happy path at minimal indentation:
- When the `if` body ends with `return`/`break`/`continue`, the `else` MUST be dropped. Use default-then-override for simple assignments — assign a default, then override with independent conditions or a `switch`:
- When an `if` condition has 3+ operands, MUST extract into named booleans — a wall of `||` is unreadable and hides business logic. Keep expensive checks inline for short-circuit benefit. [Details](./references/details.md)
- When comparing the same variable multiple times, prefer `switch`:
- Functions SHOULD be short and focused — one function, one job.
- Functions SHOULD have ≤4 parameters. Beyond that, use an options struct (see `samber/cc-skills-golang@golang-design-patterns` skill).
- Naked returns help in very short functions (1-3 lines) where return values are obvious, but become confusing when readers must scroll to find what's returned — name returns explicitly in longer functions.
- SHOULD use `range` over index-based loops. Use `range n` (Go 1.22+) for simple counting.
- Dot imports pollute the namespace and make it impossible to tell where a name comes from — never use in library code
- Unexport aggressively — you can always export later; unexporting is a breaking change. → See `samber/cc-skills-golang@golang-gopls` skill to unexport safely — its rename updates every call site atomically and refuses the change when lowercasing a method would break interface satisfaction, a breakage grep/sed silently ships.
- Prefer explicit, narrow conversions. Use generics over `any` when a concrete type will do:
- "Reflection is never clear" — avoid `reflect` unless necessary
- Don't abstract prematurely — extract when the pattern is stable

Sections: Line Length & Breaking · Variable Declarations · Control Flow · Function Design · Value vs Pointer Arguments · Code Organization Within Files · String Handling · Type Conversions · Philosophy · Parallelizing Code Style Reviews · Enforce with Linters · Cross-References

References (1): `references/details.md`

### `golang-documentation`
> Comprehensive documentation guide for Golang projects, covering godoc comments, README, CONTRIBUTING, CHANGELOG, Go Playground, Example tests, API docs, and llms.txt. Use when writing or reviewing doc comments, documentation, adding code examples, setting up doc sites, or discussing documentation best practices. Triggers for both libraries and applications/CLIs.

- Persona: You are a Go technical writer and API designer. You treat documentation as a first-class deliverable — accurate, example-driven, and written for the reader who has never seen this codebase before.
- Concision — write the shortest version that carries the idea. Remove ornament and hollow transitions. Never drop facts, warnings, or user-requested depth.
- Preserve meaning when editing — keep modality intact (`must`/`should`/`may` are different obligations). Preserve conditions, warnings, required actions. A cleaner sentence that changes obligations is wrong.
- Every exported function and method MUST have a doc comment. Document complex internal functions too. Skip test functions.
- The comment starts with the function name and a verb phrase. Focus on why and when, not restating what the code already shows. The code tells you _what_ happens — the comment should explain _why_ it exists, _when_ to use it, _what constraints_ apply, and _what can go wrong_. Include parameters, return values, error cases, and a usage example:
- //   - basePrice: The original price before any discounts (must be non-negative)
- //   - quantity: The number of units ordered (must be positive)
- README SHOULD follow this exact section order. Copy the template from [templates/README.md](./assets/templates/README.md):
- Title
- Badges
- Summary
- Demo
- Getting Started
- Features / Specification
- Contributing
- Contributors
- License
- Prefer auto-generation from code annotations when possible. See [Application Documentation](./references/application.md#api-documentation) for details.

Sections: Cross-References · Writing Principles · Step 1: Detect Project Type · Step 2: Documentation Checklist · Parallelizing Documentation Work · Step 3: Function & Method Doc Comments · Step 4: README Structure · Step 5: CONTRIBUTING & Changelog · Step 6: Library-Specific Documentation · Step 7: Application-Specific Documentation · Step 8: API Documentation · Step 9: AI-Friendly Documentation

References (7): `assets/templates/CHANGELOG.md`, `assets/templates/CONTRIBUTING.md`, `assets/templates/README.md`, `references/application.md`, `references/code-comments.md`, `references/library.md`, `references/project-docs.md`

### `golang-error-handling`
> Idiomatic Golang error handling — creation, wrapping with %w, errors.Is/As, errors.Join, custom error types, sentinel errors, panic/recover, the single handling rule, structured logging with slog, HTTP request logging middleware, and samber/oops for production errors. Built to make logs usable at scale with log aggregation 3rd-party tools. Apply when creating, wrapping, inspecting, or logging erro…

- Persona: You are a Go reliability engineer. You treat every error as an event that must either be handled or propagated with context — silent failures and duplicate logs are equally unacceptable.
- Returned errors MUST always be checked
- Errors MUST be wrapped with context
- Error strings MUST be lowercase
- Use `%w` internally, `%v` at system boundaries
- MUST use `errors.Is` for sentinel matching and `errors.As`/`errors.AsType` for typed chain inspection
- SHOULD use `errors.Join`
- Errors MUST be either logged OR returned
- Use sentinel errors
- NEVER use `panic` for expected error conditions
- SHOULD use `slog`
- Use `samber/oops`
- Log HTTP requests
- Use log levels
- Never expose technical errors to users
- Keep log grouping low-cardinality
- [Error Creation](./references/error-creation.md) — How to create errors that tell the story: error messages should be lowercase, no punctuation, and describe what happened without prescribing action. Covers sentinel errors (one-time preallocation for performance), custom error types (for carrying rich context), and the decision table for which to use when.
- [Error Handling Patterns and Logging](./references/error-handling.md) — The single handling rule: errors are either logged OR returned, NEVER both (prevents duplicate logs cluttering aggregators). Panic/recover design, `samber/oops` for production errors, and `slog` structured logging integration for APM tools.

Sections: Best Practices Summary · Detailed Reference · Parallelizing Error Handling Audits · Cross-References · References

References (3): `references/error-creation.md`, `references/error-handling.md`, `references/error-wrapping.md`

### `golang-lint`
> Linting best practices and golangci-lint configuration for Golang projects — running linters, configuring .golangci.yml, suppressing warnings with nolint directives, interpreting lint output, and selecting linters. Use when configuring golangci-lint, asking about lint warnings or nolint suppressions, setting up code quality tooling, or choosing linters. Also use when the user mentions golangci-lin…

- `golangci-lint` is the standard Go linting tool. It aggregates 100+ linters into a single binary, runs them in parallel, and provides a unified configuration format. Run it frequently during development and always in CI.
- Every Go project MUST have a `.golangci.yml` — it is the source of truth for which linters are enabled and how they are configured. See the [recommended configuration](./assets/.golangci.yml) for a production-ready setup with 48 linters enabled.
- //nolint directives MUST specify the linter name
- //nolint directives MUST include a justification comment
- The `nolintlint` linter enforces both rules above
- NEVER suppress security linters
- Linters SHOULD be run after every significant change
- Auto-fix what you can
- Format before committing
- Incremental adoption on legacy code

Sections: Overview · Quick Reference · Configuration · Suppressing Lint Warnings · Development Workflow · Interpreting Output · Common Issues · Parallelizing Legacy Codebase Cleanup · Cross-References

References (2): `references/linter-reference.md`, `references/nolint-directives.md`

### `golang-naming`
> Go (Golang) naming conventions — covers packages, constructors, structs, interfaces, constants, enums, errors, booleans, receivers, getters/setters, functional options, acronyms, test functions, and subtest names. Use this skill when writing new Go code, reviewing or refactoring, choosing between naming alternatives (New vs NewTypeName, isConnected vs connected, ErrNotFound vs NotFoundError, Statu…

- Go favors short, readable names. Capitalization controls visibility — uppercase is exported, lowercase is unexported. All identifiers MUST use MixedCaps, NEVER underscores.
- All Go identifiers MUST use `MixedCaps` (or `mixedCaps`). NEVER use underscores in identifiers — the only exceptions are test function subcases (`TestFoo_InvalidInput`), generated code, and OS/cgo interop. This is load-bearing, not cosmetic — Go's export mechanism relies on capitalization, and tooling assumes MixedCaps throughout.
- Go call sites always include the package name, so repeating it in the identifier wastes the reader's time — `http.HTTPClient` forces parsing "HTTP" twice. A name MUST NOT repeat information already present in the package name, type name, or surrounding context.
- Boolean struct fields: Unexported boolean fields MUST use `is`/`has`/`can` prefix — `isConnected`, `hasPermission`, not bare `connected` or `permission`. The exported getter keeps the prefix: `IsConnected() bool`. This reads naturally as a question and distinguishes booleans from other types.
- Error strings are fully lowercase — including acronyms. Write `"invalid message id"` not `"invalid message ID"`, because error strings are often concatenated with other context (`fmt.Errorf("parsing token: %w", err)`) and mixed case looks wrong mid-sentence. Sentinel errors should include the package name as prefix: `errors.New("apiclient: not found")`.
- Enum zero values: Always place an explicit `Unknown`/`Invalid` sentinel at iota position 0. A `var s Status` silently becomes 0 — if that maps to a real state like `StatusReady`, code can behave as if a status was deliberately chosen when it wasn't.
- Subtest names: Table-driven test case names in `t.Run()` should be fully lowercase descriptive phrases: `"valid id"`, `"empty input"` — not `"valid ID"` or `"Valid Input"`.
- [Packages, Files & Import Aliasing](./references/packages-files.md) — Package naming (single word, lowercase, no plurals), file naming conventions, import alias patterns (only use on collision to avoid cognitive load), and directory structure.

Sections: Quick Reference · MixedCaps · Avoid Stuttering · Frequently Missed Conventions · Detailed Categories · Common Mistakes · Enforce with Linters · Cross-References

References (5): `references/functions-methods.md`, `references/identifiers.md`, `references/packages-files.md`, `references/testing.md`, `references/types-errors.md`

### `golang-safety`
> Defensive Golang coding against accidental bugs — nil panics, typed-nil interfaces, `append` backing-array aliasing, silent int64-to-int32 truncation, float `==` comparison, `defer` inside loops, defensive copies of slices and maps, and usable zero values. Use when a Go program panics on a nil map write or nil pointer dereference, when reviewing code for nil-safety, numeric conversion overflow, or…

- Prefer generics over `any`
- Always use safe type assertions
- Typed nil pointer in an interface is not `== nil`
- Writing to a nil map panics
- `append` may reuse the backing array
- Return defensive copies
- `defer` runs at function exit, not loop iteration
- Integer conversions truncate silently
- Float arithmetic is not exact
- Design useful zero values
- Use `sync.Once` for lazy init
- Maps MUST NOT be accessed concurrently — → see `samber/cc-skills-golang@golang-concurrency` for sync primitives.
- Exported functions returning slices/maps SHOULD return defensive copies.
- → See `samber/cc-skills-golang@golang-design-patterns` for why init() should be avoided in favor of explicit constructors.
- For reflection code, prefer `reflect.TypeAssert[T]` over `value.Interface().(T)`.

Sections: Best Practices Summary · Nil Safety · Slice & Map Safety · Numeric Safety · Resource Safety · Immutability & Defensive Copying · Initialization Safety · Enforce with Linters · Common Mistakes · Cross-References

References (2): `references/nil-safety.md`, `references/slice-map-safety.md`

### `golang-security`
> Security best practices and vulnerability prevention for Golang — injection (SQL, command, XSS), cryptography, path traversal, SSRF and HTTP security headers, cookies, secrets management, memory safety, PII in logs, STRIDE/DREAD threat modeling, plus `gosec` SAST, race detection, and fuzz testing. Apply when writing, reviewing, or auditing Go code for security, or when touching crypto, file or net…

- What are the trust boundaries?
- What can an attacker control?
- What is the blast radius?
- Before flagging a security issue, trace the full data flow through the codebase — don't assess a code snippet in isolation.
- Trace the data origin
- Check for upstream validation
- Examine the trust boundary
- Read the surrounding code, not just the diff

Sections: Overview · Security Thinking Model · Severity Levels · Research Before Reporting · Threat Modeling (STRIDE) · Quick Reference · Detailed Categories · Code Review Checklist · Tooling & Verification · Common Mistakes · Security Anti-Patterns · Cross-References

References (12): `references/architecture.md`, `references/checklist.md`, `references/cookies.md`, `references/cryptography.md`, `references/filesystem.md`, `references/injection.md`, `references/logging.md`, `references/memory-safety.md`, `references/network.md`, `references/secrets.md`, `references/third-party.md`, `references/threat-modeling.md`

### `golang-structs-interfaces`
> Golang struct and interface design patterns — composition, embedding, type assertions, type switches, interface segregation, dependency injection via interfaces, struct field tags, and pointer vs value receivers. Use this skill when designing Go types, defining or implementing interfaces, embedding structs or interfaces, writing type assertions or type switches, adding struct field tags for JSON/Y…

- Interfaces SHOULD have 1-3 methods. Small interfaces are easier to implement, mock, and compose. If you need a larger contract, compose it from small interfaces:
- Interfaces MUST be defined where consumed, not where implemented. This keeps the consumer in control of the contract and avoids importing a package just for its interface.
- Functions SHOULD accept interface parameters for flexibility and return concrete types for clarity. Callers get full access to the returned type's fields and methods; consumers upstream can still assign the result to an interface variable if needed.
- Since Go 1.18+, MUST prefer generics over `any` for type-safe operations. Use `any` only at true boundaries where the type is genuinely unknown (e.g., JSON decoding, reflection):
- Canonical method signatures MUST be honored — if your type has a `String()` method, it must match `fmt.Stringer`. Don't invent `ToString()` or `ReadData()`.
- Type assertions MUST use the comma-ok form (`s, ok := val.(string)`) — the single-value form panics on a type mismatch instead of branching. Use a type switch to dispatch on the dynamic type, and an assertion to a small optional interface (`if f, ok := w.(Flusher); ok`) to exploit richer implementations without widening the declared parameter type.
- Exported fields in serialized structs MUST have field tags — without one, the encoder falls back to the Go field name, so renaming a field silently changes the wire format:
- Receiver type MUST be consistent across all methods of a type — if one method uses a pointer receiver, all methods should.

Sections: Interface Design Principles · Make the Zero Value Useful · Avoid `any` / `interface{}` When a Specific Type Will Do · Key Standard Library Interfaces · Compile-Time Interface Check · Type Assertions & Type Switches · Struct & Interface Embedding · Dependency Injection via Interfaces · Struct Field Tags · Pointer vs Value Receivers · Preventing Struct Copies with `noCopy` · Cross-References

References (2): `references/struct-fields.md`, `references/type-assertions.md`

## Architecture & Design

### `golang-concurrency`
> Golang concurrency design — goroutine lifecycle and leak prevention, channels and `select`, channel ownership and direction, `sync.Mutex`/`RWMutex`/`sync.Map`/`sync.Once`/atomics, `errgroup`, `singleflight`, worker pools, and fan-out/fan-in pipelines. Use when writing or reviewing concurrent Go code, when choosing between channels and mutexes, when protecting a shared map or counter, or when a gor…

- Go's concurrency model is built on goroutines and channels. Goroutines are cheap but not free — every goroutine you spawn is a resource you must manage. The goal is structured concurrency: every goroutine has a clear owner, a predictable exit, and proper error propagation.
- Every goroutine must have a clear exit
- Share memory by communicating
- Send copies, not pointers
- Only the sender closes a channel
- Specify channel direction
- Default to unbuffered channels
- Always include `ctx.Done()` in select
- Avoid repeated `time.After` in hot loops
- Track goroutine leaks in tests
- [ ] Should this be synchronous instead? — don't add concurrency without measured need

Sections: Core Principles · Channel vs Mutex vs Atomic · WaitGroup vs errgroup · Sync Primitives Quick Reference · Concurrency Checklist · Pipelines and Worker Pools · Parallelizing Concurrency Audits · Common Mistakes · Cross-References · References

References (3): `references/channels-and-select.md`, `references/pipelines.md`, `references/sync-primitives.md`

### `golang-context`
> Idiomatic context.Context usage in Golang — propagation through API boundaries, cancellation, timeouts and deadlines, request-scoped values, context.WithoutCancel for background work outliving requests. Apply when designing context propagation across layers, debugging leaked or unexpired contexts, choosing between context.Background/TODO/WithoutCancel, or storing values in context. Not for code th…

- 9. Carry only request-scoped metadata in context values, never function parameters — values retrieved through `Value()` lose compile-time typing and disappear from the function signature.
- 10. Use `context.WithoutCancel` (Go 1.21+) when spawning background work that must outlive the parent request — otherwise the handler returning cancels the audit log or cleanup just started.

Sections: Best Practices Summary · Creating Contexts · Context Propagation: The Core Principle · Deep Dives · Cross-References · Enforce with Linters

References (3): `references/cancellation.md`, `references/http-services.md`, `references/values-tracing.md`

### `golang-data-structures`
> Golang data structures — slices (internals, capacity growth, preallocation, slices package), maps (internals, hash buckets, maps package), arrays, container/list/heap/ring, strings.Builder vs bytes.Buffer, generic collections, pointers (unsafe.Pointer, weak.Pointer), and copy semantics. Use when choosing or optimizing Go data structures, implementing generic containers, using container/ packages, …

- Preallocate slices and maps
- Arrays
- NEVER rely on slice capacity growth timing
- Use `container/heap`
- `strings.Builder`
- 6. Generic data structures SHOULD use the tightest constraint possible — `comparable` for keys, custom interfaces for ordering
- `unsafe.Pointer`
- `weak.Pointer[T]`
- [Map Internals Deep Dive](./references/map-internals.md) — How Go maps store and hash data, bucket overflow chains, why maps never shrink (and what to do about it), comparing map performance to alternatives.
- Prefer slices for everything else — arrays cannot grow and pass by value (expensive for large sizes).
- [Pointer Types Deep Dive](./references/pointers.md) — Normal pointers, `unsafe.Pointer` (the 6 valid spec patterns), and `weak.Pointer[T]` for GC-safe caches that don't prevent cleanup.
- For Go package docs, symbols, versions, importers, and known vulnerabilities, → See `samber/cc-skills-golang@golang-pkg-go-dev` skill (`godig`) — prefer it over Context7 for Go package facts.

Sections: Best Practices Summary · Slice Internals · Map Internals · Arrays · container/ Standard Library · strings.Builder vs bytes.Buffer · Generic Collections (Go 1.18+) · Pointer Types · Copy Semantics Quick Reference · Third-Party Libraries · Cross-References · Common Mistakes

References (5): `references/containers.md`, `references/generics.md`, `references/map-internals.md`, `references/pointers.md`, `references/slice-internals.md`

### `golang-database`
> Comprehensive guide for Go database access — parameterized queries, struct scanning, NULLable columns, transactions, isolation levels, SELECT FOR UPDATE, connection pool, batch processing, context propagation, and migration tooling. Use when writing, reviewing, or debugging Golang code that interacts with PostgreSQL, MariaDB, MySQL, or SQLite; for database testing; or for questions about database/…

- Go's `database/sql` provides a solid foundation for database access. Use `sqlx` or `pgx` on top of it for ergonomics — never an ORM.
- Use sqlx or pgx, not ORMs
- 2. Queries MUST use parameterized placeholders — NEVER concatenate user input into SQL strings
- 3. Context MUST be passed to all database operations — use `*Context` method variants (`QueryContext`, `ExecContext`, `GetContext`)
- 4. `sql.ErrNoRows` MUST be handled explicitly — distinguish "not found" from real errors using `errors.Is`
- 5. Rows MUST be closed after iteration — `defer rows.Close()` immediately after `QueryContext` calls
- 6. NEVER use `db.Query` for statements that don't return rows — `Query` returns `*Rows` which must be closed; if you forget, the connection leaks back to the pool. Use `db.Exec` instead
- Use transactions for multi-statement operations
- Use `SELECT ... FOR UPDATE`
- Set custom isolation levels
- Handle NULLable columns
- 11. Connection pool MUST be configured — `SetMaxOpenConns`, `SetMaxIdleConns`, `SetConnMaxLifetime`, `SetConnMaxIdleTime`
- Use external tools for migrations
- Batch operations in reasonable sizes
- Never create or modify database schemas
- Avoid hidden SQL features
- Never interpolate column names from user input. Use an allowlist:
- if err := rows.Err(); err != nil { // always check after iteration
- Always use the `*Context` method variants to propagate deadlines and cancellation:
- Migration SQL should be written and reviewed by humans, versioned in source control, and applied through CI/CD pipelines.
- Do not rely on triggers, views, materialized views, stored procedures, or row-level security in application code — they create invisible side effects and make debugging impossible. Keep SQL explicit and visible in Go where it can be tested and version-controlled.

Sections: Best Practices Summary · Library Choice · Parameterized Queries · Struct Scanning and NULLable Columns · Error Handling · Context Propagation · Transactions, Isolation Levels, and Locking · Connection Pool · Migrations · Avoid Hidden SQL Features · Schema Creation · Deep Dives

References (4): `references/performance.md`, `references/scanning.md`, `references/testing.md`, `references/transactions.md`

### `golang-dependency-injection`
> Comprehensive guide for dependency injection (DI) in Golang. Covers why DI matters (testability, loose coupling, separation of concerns, lifecycle management), manual constructor injection, and DI library comparison (google/wire, uber-go/dig, uber-go/fx, samber/do). Use this skill when designing service architecture, setting up dependency injection, refactoring tightly coupled code, managing singl…

- Persona: You are a Go software architect. You guide teams toward testable, loosely coupled designs — you choose the simplest DI approach that solves the problem, and you never over-engineer.
- Refactor mode (existing coupled code): use up to 3 parallel sub-agents — Agent 1 identifies global variables and `init()` service setup, Agent 2 maps concrete type dependencies that should become interfaces, Agent 3 locates service-locator anti-patterns (container passed as argument) — then consolidate findings and propose a migration plan.
- 1. Dependencies MUST be injected via constructors — NEVER use global variables or `init()` for service setup
- 2. Small projects (< 10 services) SHOULD use manual constructor injection — no library needed
- 3. Interfaces MUST be defined where consumed, not where implemented — accept interfaces, return structs
- 4. NEVER use global registries or package-level service locators
- 5. The DI container MUST only exist at the composition root (`main()` or app startup) — NEVER pass the container as a dependency
- Prefer lazy initialization
- Use singletons for stateful services
- Mock at the interface boundary
- Keep the dependency graph shallow
- Choose the right DI library
- DI shines in applications with many interconnected services — HTTP servers, microservices, CLI tools with plugins. For a small script with 2-3 functions, manual wiring is fine. Don't over-engineer.
- api := do.MustInvoke[*API](i)
- svc := do.MustInvoke[*UserService](testInjector)

Sections: Best Practices Summary · Why Dependency Injection? · Manual Constructor Injection (No Library) · DI Library Comparison · Testing with DI · When to Adopt a DI Library · Common Mistakes · Cross-References · References

References (4): `references/google-wire.md`, `references/manual-di.md`, `references/samber-do.md`, `references/uber-dig-fx.md`

### `golang-design-patterns`
> Idiomatic Golang design patterns — functional options, constructor APIs, `init()` and global-state avoidance, enums, panic vs error decisions, resource management and lifecycle, graceful shutdown, timeouts and retries, streaming and iterators, and architecture styles (clean, hexagonal, DDD, flat). Apply when choosing between architectural patterns, implementing functional options, designing constr…

- 1. Constructors SHOULD use functional options — they scale better as APIs evolve (one function per option, no breaking changes)
- 2. Functional options MUST return an error if validation can fail — catch bad config at construction, not at runtime
- Avoid `init()`
- 4. Enums SHOULD start at 1 (or Unknown sentinel at 0) — Go's zero value silently passes as the first enum member
- 5. Error cases MUST be handled first with early return — keep happy path flat
- Panic is for bugs, not expected errors
- `defer Close()` immediately after opening
- `runtime.AddCleanup`
- 9. Every external call SHOULD have a timeout — a slow upstream hangs your goroutine indefinitely
- Limit everything
- 11. Retry logic MUST check context cancellation between attempts
- Use `strings.Builder`
- 14. Iterators (Go 1.23+): use for lazy evaluation — avoid loading everything into memory
- Stream large transfers
- Use `crypto/rand`
- 18. Regexp MUST be compiled once at package level — compilation is O(n) and allocates
- A little recode > a big dependency
- Design for testability
- Constructors SHOULD use functional options — they scale better with API evolution and require less code. Use builder pattern only if you need complex validation between configuration steps.
- Cannot return errors — failures must panic or `log.Fatal`
- Zero values should represent invalid/unset state:
- var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
- _8 aturan lain di SKILL.md_

Sections: Best Practices Summary · Constructor Patterns: Functional Options vs Builder · Constructors & Initialization · Error Flow Patterns · Data Handling · Resource Management · Resilience & Limits · Database Patterns · Architecture · Detailed Guides · Code Philosophy · Cross-References

References (6): `references/architecture.md`, `references/clean-architecture.md`, `references/data-handling.md`, `references/ddd.md`, `references/hexagonal-architecture.md`, `references/resource-management.md`

### `golang-modernize`
> Modernize Golang code to use recent language features, standard library improvements, and idiomatic patterns. Use when reviewing Go code with old-style patterns, when encountering a deprecation warning, or when the user asks for modernization, a Go version upgrade (e.g. to Go 1.27), or a CI/tooling refresh. Not for structural refactors, extracting functions, or moving code between packages (→ See …

- Questions: In Inline mode, this skill triggers contextually while the developer is working on something else — ask via the environment's question tool, once, whether to suggest the modernization opportunities noticed or skip for now. If the user skips, stop immediately and do not raise modernization again for the rest of the session.
- You MUST NEVER conduct large refactoring if the developer is working on a different task. But TRY TO CONVINCE your human it would improve the code quality.
- Check the project's `go.mod` or `go.work`
- Check the latest Go version
- Read `.modernize`
- Scan the codebase
- Run `golangci-lint`
- Suggest improvements contextually
- If the developer is actively coding, only suggest improvements related to the code they are currently working on. Do not refactor unrelated files. Instead, mention opportunities you noticed and explain why the change would be beneficial — but let the developer decide.
- For large codebases
- Before suggesting a dependency update
- If the developer explicitly ignores a suggestion

Sections: Workflow · Go Version Changelogs · Using the modernize linter · Version-specific modernizations · Tooling modernization · Deprecated Packages Migration · Go 1.27+ version-bump risk checklist · Migration Priority Guide · Related Skills

References (2): `references/tooling.md`, `references/versions.md`

### `golang-refactoring`
> Golang refactoring — safe, at-scale restructuring of existing Go code: a coverage-adaptive safety net, behavior-preserving transforms (gopls Rename/Extract, `gofmt -r`, `gopatch`), the Fowler catalog mapped to Go, breaking import cycles, and small stacked PRs. Apply when a function or type has grown too large, a code smell blocks a feature, or the user asks to refactor Go code — also for renaming …

- Persona: You are a Go refactoring engineer. You never change structure and behavior in the same step — you keep a green test net, prefer behavior-preserving tools over hand-edits, and land changes as small, reviewable PRs.
- Understand
- Safety net
- Small tool-driven step
- Verify
- Atomic single-category commit
- Never mix structural and behavioral changes in one commit or PR.
- Prefer gopls Rename/Inline over LLM hand-edits.
- Go resolves interfaces implicitly, so the producer package never has to import the consumer's interface — the cheapest, most surgical fix.
- Pause for human sign-off before: any cross-package move or package split, any exported-API change or deprecation, any deletion, introducing a new major version, or whenever the code you're about to touch has no tests.
- Load `samber/cc-skills-golang@golang-security` (and `golang-safety` for internal-correctness risk) whenever a step changes code logic, not just its shape.
- It's critical production code with no tests. Don't refactor it directly.

Sections: The Core Loop · Hard Rules · When Not to Refactor · Risk Stratification · Workflow: Plan → Stage → Land · Detailed References · Cross-References

References (5): `references/catalog.md`, `references/go-tooling.md`, `references/safety-net.md`, `references/structural.md`, `references/workflow.md`

## QA & Performance

### `golang-benchmark`
> Golang benchmarking, profiling, and performance measurement. Use when writing, running, or comparing Go benchmarks, profiling hot paths with pprof, interpreting CPU/memory/trace profiles, analyzing results with benchstat, setting up CI benchmark regression detection, or investigating production performance with Prometheus runtime metrics. Also use when the developer needs deep analysis on a specif…

- Persona: You are a Go performance measurement engineer. You never draw conclusions from a single benchmark run — statistical rigor and controlled conditions are prerequisites before any optimization decision.
- Order `Benchmark*` functions inside `parser_bench_test.go` to mirror the order of the functions/methods they measure in `parser.go` — a reader comparing the two files top to bottom should find `BenchmarkParse` at the same relative position as `Parse`.
- For Go 1.24+, prefer `b.Loop()` for new benchmarks. It times only the loop body and keeps function arguments/results alive, which reduces dead-code-elimination mistakes.
- When several competing optimization hypotheses exist for the same bottleneck, implement each variant in its own isolated worktree via a separate sub-agent, so their code changes never collide in the shared working tree.
- Never paste results with `~` (no statistical significance) — the improvement cannot be claimed
- [Diagnostic Tools](./references/tools.md) — Quick reference for ancillary tools: fieldalignment (struct padding waste), GODEBUG (runtime logging flags), fgprof (frame graph profiles), race detector (concurrency bugs), and others. Use this when you have a specific symptom and need a focused diagnostic — don't reach for pprof if a simpler tool already answers your question.
- → See `samber/cc-skills-golang@golang-observability` skill for everyday always-on monitoring, continuous profiling (Pyroscope), distributed tracing (OpenTelemetry)

Sections: Writing Benchmarks · Running Benchmarks · Comparing Optimization Variants in Parallel · Documenting Results in Commits · Profiling from Benchmarks · Reference Files · Cross-References

References (8): `references/benchstat.md`, `references/ci-regression.md`, `references/compiler-analysis.md`, `references/investigation-session.md`, `references/pprof.md`, `references/prometheus-go-metrics.md`, `references/tools.md`, `references/trace.md`

### `golang-observability`
> Golang everyday observability — the always-on signals in production. Covers structured logging with slog, Prometheus metrics, OpenTelemetry distributed tracing, continuous profiling with pprof/Pyroscope, server-side RUM event tracking, alerting, and Grafana dashboards. Apply when instrumenting Go services for production monitoring, setting up metrics or alerting, adding OpenTelemetry tracing, corr…

- Persona: You are a Go observability engineer. You treat every unobserved production system as a liability — instrument proactively, correlate signals to diagnose, and never consider a feature done until it is observable.
- Use structured logging
- Choose the right log level
- Log with context
- Prefer Histogram over Summary
- Keep label cardinality low
- Track percentiles
- Set up OpenTelemetry tracing on new projects
- Add spans to every meaningful operation
- Propagate context everywhere
- Enable profiling via environment variables
- Correlate signals
- A feature is not done until it is observable
- [awesome-prometheus-alerts](https://samber.github.io/awesome-prometheus-alerts/) provides ~500 ready-to-use alerting rules
- For simple fan-out to multiple slog handlers, prefer stdlib `slog.NewMultiHandler` before adding third-party handler-composition dependencies.
- [Profiling](references/profiling.md) — On-demand profiling with pprof (CPU, heap, goroutine, mutex, block profiles) — how to enable it in production, secure it with auth, and toggle via environment variables without redeploying. Continuous profiling with Pyroscope for always-on performance visibility. Cost implications of each profiling type and mitigation strategies.
- [ ] Logging is proper — structured key-value pairs with `slog`, context variants used (`slog.InfoContext`), no PII in logs, errors MUST be either logged OR returned (NEVER both).

Sections: Best Practices Summary · Cross-References · The Five Signals · Detailed Guides · Correlating Signals · Migrating Legacy Loggers · Definition of Done for Observability · Common Mistakes

References (7): `references/alerting.md`, `references/dashboards.md`, `references/logging.md`, `references/metrics.md`, `references/profiling.md`, `references/rum.md`, `references/tracing.md`

### `golang-performance`
> Golang performance optimization patterns and methodology - if X bottleneck, then apply Y. Covers allocation reduction, CPU efficiency, memory layout, GC tuning, pooling, caching, and hot-path optimization. Use when profiling or benchmarks have identified a bottleneck and you need the right optimization pattern to fix it. Also use when performing performance code review to suggest improvements or b…

- Persona: You are a Go performance engineer. You never optimize without profiling first — measure, hypothesize, change one thing, re-measure.
- Profile before optimizing
- Allocation reduction yields the biggest ROI
- Document optimizations
- Define your metric
- Write an atomic benchmark
- Measure baseline
- Diagnose
- Improve
- Compare
- Commit
- Repeat

Sections: Core Philosophy · Rule Out External Bottlenecks First · Iterative Optimization Methodology · Decision Tree: Where Is Time Spent? · Common Mistakes · Deep Dives · CI Regression Detection · Cross-References

References (6): `references/caching.md`, `references/cpu.md`, `references/io-networking.md`, `references/memory.md`, `references/observability.md`, `references/runtime.md`

### `golang-testing`
> Production-ready Golang tests — table-driven tests, testify suites and mocks, parallel tests, fuzzing, fixtures, goroutine leak detection with goleak, snapshot testing, code coverage, integration tests, idiomatic test naming. Use when writing or reviewing Go tests, choosing a testing approach, setting up Go test CI, or debugging flaky/slow tests. For testify-specific APIs see `samber/cc-skills-gol…

- 1. Table-driven tests MUST use named subtests -- every test case needs a `name` field passed to `t.Run`
- 2. Integration tests MUST use build tags (`//go:build integration`) to separate from unit tests
- 3. Tests MUST NOT depend on execution order -- each test MUST be independently runnable
- 4. Independent tests SHOULD use `t.Parallel()` when possible
- 5. Tests MUST assert observable behavior and public API contracts, not implementation details -- a test coupled to internals turns every refactor into a test rewrite while proving nothing about the contract
- 6. Packages with goroutines SHOULD use `goleak.VerifyTestMain` in `TestMain` to detect goroutine leaks
- 12. Test files MUST be named after the source file under test, not after the function or method being tested
- 13. Test functions SHOULD appear in the same order as the functions/methods they test in the source file
- helloworld.go       -> abcd_test.go         // wrong: should be helloworld_test.go
- Table-driven tests are the idiomatic Go way to test multiple scenarios. Always name each test case.
- Unit tests should be fast (< 1ms), isolated (no external dependencies), and deterministic.
- Use `synctest.Test` in Go 1.25+ and later. Do not use the old Go 1.24 experimental `synctest.Run` API in Go 1.25+ code. If a module explicitly targets Go 1.24 and opts into `GOEXPERIMENT=synctest`, use the old API only as a compatibility fallback.

Sections: Best Practices Summary · Test Structure and Organization · Table-Driven Tests · Common Pitfall: Assert Scope Leaking into Subtests · Unit Tests · Testing HTTP Handlers · Goroutine Leak Detection with goleak · testing/synctest for Deterministic Goroutine Testing · Test Timeouts · Benchmarks · Go 1.26+: test artifacts · Parallel Tests

References (7): `references/benchmarks.md`, `references/coverage.md`, `references/examples.md`, `references/helpers.md`, `references/http-testing.md`, `references/integration-testing.md`, `references/mocking.md`

### `golang-troubleshooting`
> Troubleshoot Golang programs systematically - find and fix the root cause. Use when encountering bugs, crashes, deadlocks, races, or unexpected behavior in Go code. Covers debugging methodology, common Go pitfalls, test-driven debugging, pprof setup and capture, Delve, race detection, GODEBUG tracing, and production debugging. Start here for any 'something is wrong' situation. Not for interpreting…

- Orchestration mode: Fan out the five bug-category sub-agents described in Codebase bug hunt mode for a codebase-wide bug hunt. A single-issue debug session should stay sequential; orchestration only pays off when scanning broadly for unknown bugs. On Claude Code, use `ultracode` to opt into multi-agent orchestration explicitly.
- Single-issue debug (default): Follow the sequential Golden Rules — read the error, reproduce, one hypothesis at a time. Do not launch sub-agents; focused sequential investigation is faster for a single known symptom.
- Start with the Decision Tree
- Follow the Golden Rules
- Work through the General Debugging Methodology
- Watch for Red Flags
- Escalate tools incrementally.
- Never propose a fix you cannot explain.
- NEVER debug by guessing — reproduce first. Always:
- Never rely on intuition for performance or concurrency bugs:
- You MUST understand why the bug happens before writing a fix. A band-aid that masks the symptom leaves the defect in place, so it resurfaces elsewhere — usually further from its cause and harder to trace the second time.
- When you don't understand the issue:
- Trace callers
- Check upstream validation
- Read the surrounding code
- Sometimes `fmt.Println` IS the right tool for local debugging. Escalate tools only when simpler approaches fail. NEVER use `fmt.Println` for production debugging — use `slog`.
- Blaming the framework/stdlib/compiler — It's almost never a Go bug. Verify your code first.
- [General Debugging Methodology](./references/methodology.md) — The systematic 10-step process: define symptoms, isolate reproduction, form one hypothesis, test it, verify the root cause, and defend against regressions. Escalation guide: when to escalate from `fmt.Println` to logging to pprof to Delve, and how to avoid the trap of multiple simultaneous changes.

Sections: Quick Decision Tree · The Golden Rules · Red Flags: You're Debugging Wrong · Reference Files · Cross-References

References (10): `references/code-review-flags.md`, `references/common-go-bugs.md`, `references/compilation.md`, `references/concurrency-debug.md`, `references/diagnostic-tools.md`, `references/methodology.md`, `references/performance-debug.md`, `references/pprof.md`, `references/production-debug.md`, `references/testing-debug.md`

## Project Setup

### `golang-cli`
> Golang CLI application development. Use when building, modifying, or reviewing a Go CLI tool — especially for command structure, flag handling, configuration layering, version embedding, exit codes, I/O patterns, signal handling, shell completion, argument validation, and CLI unit testing. Also triggers when code uses cobra, viper, or urfave/cli. For cobra-specific APIs → See `samber/cc-skills-gol…

- `main.go` should be minimal — see [assets/examples/main.go](assets/examples/main.go).
- `SilenceUsage: true` MUST be set — prevents printing the full usage text on every error
- `SilenceErrors: true` MUST be set — lets you control error output format yourself
- `PersistentPreRunE` runs before every subcommand, so config is always initialized
- CLI flags
- Environment variables
- Config file
- Defaults
- Version SHOULD be embedded at compile time using `ldflags`. See [assets/examples/version.go](assets/examples/version.go) for the version command and build instructions.
- Exit codes MUST follow Unix conventions:
- stdout vs stderr: NEVER write diagnostic output to stdout — stdout is for program output (pipeable), stderr for logs/errors/diagnostics
- Signal handling MUST use `signal.NotifyContext` to propagate cancellation through context. See [assets/examples/signal.go](assets/examples/signal.go) for graceful HTTP server shutdown.

Sections: Quick Reference · Project Structure · Root Command Setup · Subcommands · Flags · Argument Validation · Configuration with Viper · Version and Build Info · Exit Codes · I/O Patterns · Signal Handling · Shell Completions

References (0): —

### `golang-continuous-integration`
> GitHub Actions CI/CD pipeline configuration for Golang projects — workflow files for test, lint, SAST, coverage and vulnerability-scan jobs, Dependabot and Renovate config files, GoReleaser release pipelines, Docker build/push, repository security settings, and AI-driven PR review. Use when setting up or improving Go project CI, writing or fixing `.github/workflows/*.yml`, adding a linter or secur…

- Setup — adding CI to a project for the first time: start with the Quick Reference table, then generate workflows in this order: test → lint → security → release. Prefer the latest stable major version for each GitHub Action.
- `-race`: CI MUST run tests with the `-race` flag (catches data races — undefined behavior in Go)
- CI SHOULD enforce code coverage thresholds. Configure thresholds in `codecov.yml` at the repo root — see [codecov.yml](./assets/codecov.yml)
- `golangci-lint` MUST be run in CI on every PR. `.github/workflows/lint.yml` — see [lint.yml](./assets/lint.yml)
- CI MUST run `govulncheck` — it only reports vulnerabilities in code paths your project actually calls, unlike generic CVE scanners.
- Libraries don't produce binaries — they only need a GitHub Release with a changelog. Use a minimal config that skips the build.
- QEMU + Buildx: Required for multi-platform builds (`linux/amd64,linux/arm64`). Remove platforms you don't need.
- `push: false` on PRs: Images are built but never pushed on pull requests — this validates the Dockerfile without publishing untrusted code.

Sections: Action Versions · Quick Reference · Testing · Integration Tests · Linting · Security & SAST · Dependency Management · Release Automation · Repository Security Settings · AI-Driven Code Review · Common Mistakes · Related Skills

References (2): `assets/copilot-review-instructions.md`, `references/repo-security.md`

### `golang-dependency-management`
> Dependency management for Golang projects — go.mod and go.sum, `go get` install and upgrade flows, Minimal Version Selection, conflict resolution with replace/exclude/retract, `govulncheck` scanning of the module tree, outdated dependency and binary size auditing, vendoring, `tool` directives, and go.work workspaces. Use when adding, removing, or upgrading Go dependencies, deciding whether to take…

- Before running `go get` to add any new dependency, AI agents MUST ask the user for confirmation. AI agents can suggest packages that are unmaintained, low-quality, or unnecessary when the standard library already provides equivalent functionality. Using `go get -u` to upgrade an existing dependency is safe.
- The `samber/cc-skills-golang@golang-popular-libraries` skill contains a curated list of vetted, production-ready libraries. Prefer recommending packages from that list. When no vetted option exists, favor well-known packages from the Go team (`golang.org/x/...`) or established organizations over obscure alternatives.
- `go.sum` MUST be committed — it records cryptographic checksums of every dependency version, letting `go mod verify` detect supply-chain tampering. Without it, a compromised proxy could silently substitute malicious code
- Prefer `go get -u=patch` for routine updates. Patch and minor updates are usually lower risk than major upgrades, but still require review. For dependency updates, run:
- For Go 1.24+ modules, pin executable tools in `go.mod` with `tool` directives. Do not create a new `tools.go` blank-import file unless the module must support Go <1.24.
- `go.mod` shape for a module targeting Go 1.27 or newer. This is an example target, not a cap; keep the project's actual `go` directive and do not change it just to add tools.
- For future Go versions, use the project's intended target version. Do not use APIs newer than the module's `go` directive until the project explicitly agrees to upgrade it.
- [Dependency Conflicts & Resolution](./references/conflicts.md) — Diagnosing version conflicts (what `go get` does when you request incompatible versions), resolution strategies (`replace` directives for local development, `exclude` for broken versions, `retract` for published versions that should be skipped), and workflows for conflicts across your dependency tree.

Sections: AI Agent Rule: Ask Before Adding Dependencies · Key Rules · go.mod & go.sum · Installing & Upgrading Dependencies · Deep Dives · Cross-References · Quick Reference

References (6): `references/auditing.md`, `references/automated-updates.md`, `references/conflicts.md`, `references/versioning.md`, `references/visualization.md`, `references/workspaces.md`

### `golang-gopls`
> Golang semantic code intelligence via `gopls`, the official Go language server — go-to-definition, find references, call/implementation hierarchy, workspace symbol search, package API discovery, diagnostics, safe rename, refactors (extract/inline/fill/rewrite code actions), formatting, and generated tests. Reaches an agent via gopls's own MCP server (`go_*` tools), Claude Code's native `LSP` tool,…

- Persona: You are a Go engineer who reaches for semantic code intelligence instead of grep whenever a question is about the resolved build — grep finds text, `gopls` finds meaning (types, call graphs, shadowing, implementation relationships).
- Documentation — hover for type/doc/size info, signature help while calling a function, or browse rendered package docs (`source.doc`, including internal packages pkg.go.dev never sees).
- Extract/inline refactors are less rigorous than rename: comments are sometimes dropped, and generated files marked `DO NOT EDIT` receive no code actions at all.

Sections: Three ways to reach gopls · Capability → CLI → MCP → native LSP · Use cases · Efficient workflows · gopls vs godig vs Context7 vs govulncheck

References (5): `references/cli.md`, `references/features.md`, `references/matrix.md`, `references/mcp.md`, `references/settings.md`

### `golang-pkg-go-dev`
> Golang package and module lookup via `godig`, a pkg.go.dev API client (CLI + MCP server). Use for any Go/Golang library's documentation, API signatures, symbols, usage examples, which versions exist, licenses, whether a dependency has CVEs, or who imports a package — prefer this over Context7 for any Go package or module. Read-only, no auth. Not for upgrading dependencies (→ See `samber/cc-skills-…

- The CLI and the MCP server expose the same operations under matching names. Prefer the CLI when `godig` is installed; the hosted instance is a fallback when it is not.
- Always pass `-o md` so results render as Markdown (tables, or raw doc/README) in the chat. Other formats exist (`table` default, `json`, `raw`) but prefer `md` here.
- Prefer `symbol doc`/`symbol examples` over the package-wide `package doc`/`package examples` when you only need one symbol — far fewer tokens.
- Always request Markdown output (`-o md`):

Sections: When to use this skill · Choosing between `godig`, gopls, Context7, and govulncheck · Setup · Commands

References (1): `references/sample-output.md`

### `golang-popular-libraries`
> Golang library and framework selection — vetted production-ready options by category (web, database, testing, logging, messaging), new and experimental stdlib packages, standard-library-first tradeoffs, and maturity signals (maintenance, license, importer counts). Apply when the user asks for library suggestions, wants to compare alternatives, needs to choose a library for a specific task, or when…

- Production-readiness
- Simplicity
- Performance
- Standard Library First
- When exploring a candidate library, → See `samber/cc-skills-golang@golang-pkg-go-dev` skill (`godig`) for docs, symbols, versions, importers, and known vulnerabilities — prefer it over Context7 for Go package facts.
- Assess requirements first
- Check standard library
- Prioritize maturity
- Consider complexity
- Think about dependencies

Sections: Core Philosophy · Reference Catalogs · General Guidelines · Anti-Patterns to Avoid · Cross-References

References (3): `references/libraries.md`, `references/stdlib.md`, `references/tools.md`

### `golang-project-layout`
> Golang project layout and workspace setup — cmd/internal/pkg directory conventions, module and package naming, go.work workspaces, and essential configuration files. Use when starting a new Go project, organizing an existing codebase, setting up a monorepo with multiple packages, creating CLI tools with multiple main packages, or discussing package restructuring, package splits, or module splits. …

- Questions: Ask the user through the environment's question tool — never as plain-text prose. Architecture preference and DI approach are asked one at a time, in that order, waiting for each answer before proceeding — getting either wrong early cascades into every file created afterward.
- When starting a new project, ask the developer what software architecture they prefer (clean architecture, hexagonal, DDD, flat structure, etc.). Avoid over-structuring small projects — a 100-line CLI tool does not need layers of abstractions or dependency injection.
- Your module path in `go.mod` should:
- MUST match your repository URL: `github.com/username/project-name`
- Be semantic: Name should clearly express purpose
- Packages MUST be lowercase, singular, and match their directory name. → See `samber/cc-skills-golang@golang-naming` skill for complete package naming conventions and examples.
- All `main` packages must reside in `cmd/` with minimal logic — parse flags, wire dependencies, call `Run()`. Business logic belongs in `internal/` or `pkg/`. Use `internal/` for non-exported packages, `pkg/` only when code is useful to external consumers.
- Every Go project should include at the root:
- [ ] Write the always-load directive for `samber/cc-skills-golang@golang-how-to` into the project's agent-config file (CLAUDE.md, AGENTS.md, or equivalent) — no user confirmation needed, see that skill's Configure mode
- → See `samber/cc-skills-golang@golang-how-to` skill's Configure mode for the always-load directive and optional `## Required Go skills` block written to the project's agent-config file (CLAUDE.md, AGENTS.md, or equivalent).

Sections: Architecture Decision: Ask First · Dependency Injection: Ask Next · 12-Factor App · Quick Start: Choose Your Project Type · Module Naming Conventions · Directory Layout · Essential Configuration Files · Tests, Benchmarks, and Examples · Go Workspaces · Initialization Checklist · Related Skills

References (4): `references/config.md`, `references/directory-layouts.md`, `references/testing-layout.md`, `references/workspaces.md`

### `golang-stay-updated`
> Golang ecosystem watch list — official sources (go.dev/blog, pkg.go.dev, tour.golang.org, golang-nuts), newsletters (Golang Weekly, Awesome Go Newsletter), communities (r/golang, gophers.slack.com, Go Forum, go.dev/wiki), blogs (Dave Cheney, Ardan Labs, Rob Pike), YouTube channels (Gopher Academy, GopherCon EU/UK), conferences, and Go contributors to follow on GitHub, X and Bluesky. Use when seeki…

- Subscribe to 1-2 newsletters
- Follow 10-20 key people
- Check Go.dev/blog weekly
- Join Go Slack
- Bookmark pkg.go.dev
- Attend a GopherCon

Sections: Official Go Resources · Newsletters · Reddit & Communities · Famous Go Developers · Must-Follow Blogs · YouTube Channels · Quick Tips for Staying Updated

References (0): —

## APIs

### `golang-graphql`
> Implements GraphQL APIs in Golang using gqlgen or graphql-go. Apply when building GraphQL servers, designing schemas, writing resolvers, handling subscriptions, or integrating GraphQL with existing Go HTTP services. Also apply when the codebase imports `github.com/99designs/gqlgen` or `github.com/graph-gophers/graphql-go`.

- Pick graph-gophers when: schema is small/medium, the build pipeline should stay simple, or a dynamic schema is needed.
- email: String! # non-null: the server can always return this
- Nullability rule: mark a field `!` only when the server can _always_ return a value. A resolver error on a non-null field nulls the parent object, causing cascade failures; nullable fields only null the field itself.
- Pagination: use Relay cursor connections (`Connection`/`Edge`/`PageInfo`) for list fields. Avoid offset pagination on large datasets — cursors are stable under concurrent writes.
- Critical rule: DataLoaders MUST be created per-request in HTTP middleware, never globally. A global DataLoader caches across requests — stale data, potential cross-user data leakage.
- HTTP middleware
- Schema directives
- Never return raw internal errors — they leak SQL messages, stack traces, or service internals to clients.
- Subscriptions use long-lived WebSocket connections. The critical discipline: always respect context cancellation — a leaked goroutine per disconnected client exhausts resources silently.
- defer close(ch) // always close; signals iteration to stop

Sections: Library Choice · Schema Design · Resolver Patterns · N+1 Prevention (DataLoaders) · Authentication and Authorization · Error Handling · Subscriptions · Performance and Safety · Common Mistakes · Deep Dives · Cross-References · References

References (3): `references/gqlgen.md`, `references/graphql-go.md`, `references/testing.md`

### `golang-grpc`
> Provides gRPC usage guidelines, protobuf organization, and production-ready patterns for Golang microservices. Use when implementing, reviewing, or debugging gRPC servers/clients, writing proto files, setting up interceptors, handling gRPC errors with status codes, configuring TLS/mTLS, testing with bufconn, or working with streaming RPCs.

- Organize by domain with versioned directories (`proto/user/v1/`). Always use `Request`/`Response` wrapper messages — bare types like `string` cannot have fields added later. Generate with `buf generate` or `protoc`.
- Always return gRPC errors using `status.Error` with a specific code — a raw `error` becomes `codes.Unknown`, telling the client nothing actionable. Clients use codes to decide retry vs fail-fast vs degrade.
- Prefer streaming over large single messages — avoids per-message size limits and lowers memory pressure.
- Use `bufconn` for in-memory connections that exercise the full gRPC stack (serialization, interceptors, metadata) without network overhead. Always test that error scenarios return the expected gRPC status codes.
- TLS MUST be enabled in production — credentials travel in metadata
- Reflection SHOULD be disabled in production to prevent API discovery
- Most services do not need connection pooling — profile before adding complexity.

Sections: Quick Reference · Proto File Organization · Server Implementation · Client Implementation · Error Handling · Streaming · Testing · Security · Performance · Common Mistakes · Cross-References

References (2): `references/protoc-reference.md`, `references/testing.md`

### `golang-swagger`
> Golang OpenAPI/Swagger documentation with swaggo/swag — annotation comments (@Summary, @Param, @Success, @Router, @Security), swag init code generation, framework integrations (gin, echo, fiber, chi, net/http), security definitions (Bearer/JWT, OAuth2, API key), and struct tags (swaggertype, enums, example, swaggerignore). Apply when adding or maintaining Swagger/OpenAPI docs in a Go project, or w…

- Annotate each handler function. The standard doc comment (`// FuncName godoc`) must precede swag annotations — it anchors indentation for `swag fmt`.

Sections: Setup · General API Info · Operation Annotations · Security Definitions · Struct Tags · Common Mistakes · Cross-References

References (1): `references/swag-cli.md`

## Dependency Injection

### `golang-google-wire`
> Compile-time dependency injection in Golang using google/wire — wire.NewSet, wire.Build, wire.Bind (interface→concrete), wire.Struct, wire.Value, wire.InterfaceValue, wire.FieldsOf, cleanup functions, //go:build wireinject injector files, and generated wire_gen.go. Apply when using or adopting google/wire, when the codebase imports `github.com/google/wire`, or when wiring an application graph at c…

- Wire forbids implicit interface satisfaction — you must declare bindings explicitly so the graph is unambiguous when multiple types implement the same interface.
- Wire generates `wire_gen.go` (plain Go, committed, DO NOT EDIT). For a full example with per-package sets, cleanup-heavy graphs, and generated output, see [recipes.md](references/recipes.md).
- Run `wire ./...` after every constructor signature change. Add `//go:generate go run github.com/google/wire/cmd/wire` to injector files so `go generate ./...` also works. Commit `wire_gen.go` — it must stay in sync for CI builds.
- 1. Never edit `wire_gen.go` — it is overwritten on every `wire ./...` run. Treat it as a build artifact that happens to be committed; source of truth is the provider and injector files.
- 2. Always add `//go:build wireinject` to injector files — omitting it causes duplicate-symbol compile errors because both the stub and the generated file define the same function.

Sections: wire vs. Runtime DI · Providers · Provider Sets · Injectors and `//go:build wireinject` · Interface Bindings · Struct Providers and Values · Disambiguating Duplicate Types · Full Application Example · Codegen Workflow · Best Practices · Common Mistakes · Testing

References (3): `references/advanced.md`, `references/recipes.md`, `references/testing.md`

### `golang-uber-dig`
> Implements dependency injection in Golang using uber-go/dig — reflection-based container, Provide/Invoke, dig.In/dig.Out parameter and result objects, named values, value groups, optional dependencies, scopes, and Decorate. Apply when using or adopting uber-go/dig, when the codebase imports `go.uber.org/dig`, or when wiring an application graph at startup. For higher-level lifecycle and modules, s…

- must(c.Provide(NewConfig))
- must(c.Provide(NewLogger))
- must(c.Provide(NewDatabase))
- must(c.Provide(NewServer))
- func must(err error) { if err != nil { panic(err) } }
- 1. Keep the container at the composition root — never pass `*dig.Container` as a parameter; treat it like a plumbing detail of `main()`. Service-locator patterns defeat the testability gains of DI.
- 3. Prefer parameter objects (`dig.In` structs) once a constructor has 4+ dependencies — call sites stay readable and adding a new dependency is a one-line change instead of a signature break.

Sections: dig vs. fx · Container · Provide and Invoke · Parameter Objects with `dig.In` · Result Objects with `dig.Out` · Named Values · Value Groups · Provide as Interface (`dig.As`) · Full Application Example · Best Practices · Common Mistakes · Testing

References (3): `references/advanced.md`, `references/recipes.md`, `references/testing.md`

### `golang-uber-fx`
> Golang application framework using uber-go/fx — fx.New, fx.Provide, fx.Invoke, fx.Module, fx.Lifecycle hooks, fx.Annotate (name/group/As), fx.Decorate, fx.Supply, fx.Replace, fx.WithLogger, and signal-aware Run(). Apply when using or adopting uber-go/fx, when the codebase imports `go.uber.org/fx`, or when wiring services with fx.New. For raw DI without lifecycle, see `samber/cc-skills-golang@golan…

- Boot stages: `fx.New` validates types (constructors do not run); `app.Start(ctx)` runs each `fx.Invoke` and fires OnStart hooks in topological order; main blocks on `app.Done()`; `app.Stop(ctx)` fires OnStop hooks in reverse order. Default timeout is 15 seconds — override with `fx.StartTimeout` / `fx.StopTimeout`.
- fx.Invoke(RegisterRoutes, StartMetricsExporter), // always run during Start
- `fx.Provide` registers constructors; `fx.Invoke` is the trigger — without an Invoke (directly or transitively) referencing a type, its constructor never runs.
- Inject `fx.Lifecycle` and append hooks. Constructors should return quickly; long-running work belongs in `OnStart`.
- Both callbacks receive a context bounded by `StartTimeout`/`StopTimeout` — respect cancellation. OnStart must return quickly — spawn a goroutine for blocking work; otherwise startup hangs and dependent hooks never fire.
- `fx.Annotate` wraps a constructor to add tags or interface bindings without a `fx.Out` struct. Prefer it for ergonomic name/group/As bindings:
- 3. OnStart must return promptly — long work goes in a goroutine inside the hook. A blocking OnStart hangs the rest of the boot.

Sections: fx vs. dig · The Application · Provide and Invoke · Lifecycle Hooks · Parameter and Result Objects · fx.Annotate · Value Groups · fx.Module · Best Practices · Common Mistakes · Testing · Further Reading

References (3): `references/advanced.md`, `references/recipes.md`, `references/testing.md`

### `golang-samber-do`
> Dependency injection in Golang using samber/do — service containers, lifecycle management, scopes, health checks, graceful shutdown, and module organization. Apply when using or adopting samber/do, when the codebase imports github.com/samber/do or github.com/samber/do/v2, or when refactoring manual constructor injection into a DI container.

- Services MUST be registered via provider functions:
- The container MUST only be accessed at the composition root:
- // (e.g. an HTTP handler that must degrade gracefully instead of crashing)
- // MustInvoke panics on error — preferred in providers, recovered by do.Invoke on the parent call
- db := do.MustInvoke[Database](injector)
- Inside a provider function, always use `do.MustInvoke` (or `MustInvokeAs`/`MustInvokeNamed`/`MustInvokeStruct`) rather than the error-returning variant:
- `do.MustInvoke` panics instead, but samber/do correctly catches and recovers that panic at the enclosing `Invoke` call and converts it back into a regular error — this recover happens inside the library itself, not in caller code, so `MustInvoke` is safe to use inside providers.
- db := do.MustInvoke[Database](i)
- cache := do.MustInvoke[Cache](i)
- db := do.MustInvokeAs[Database](injector)
- mainDB := do.MustInvokeNamed[*Database](injector, "primary-db")
- cfg := do.MustInvoke[*Config](i)
- server := do.MustInvoke[*http.Server](injector)
- 2. Each service should have one job — services with multiple responsibilities are harder to test and harder to replace
- 6. Use `do.MustInvoke*` inside provider functions instead of `do.Invoke*` — samber/do correctly catches and recovers the panic at the outer `Invoke` call, turning it back into a returned error, so it's safe to use inside providers and you get the same error propagation without the boilerplate

Sections: Core Concepts · Basic Usage · Package Organization · Full Application Setup · Best Practices · Quick Reference · Cross-References

References (2): `references/advanced.md`, `references/testing.md`

## Frameworks

### `golang-spf13-cobra`
> Golang CLI command tree library using spf13/cobra — cobra.Command, RunE vs Run, PersistentPreRunE hook chain, Args validators (NoArgs, ExactArgs, MatchAll, custom), persistent vs local flags, command groups, ValidArgsFunction, RegisterFlagCompletionFunc, ShellCompDirective, usage/help template customization, man-page and markdown doc generation, and testing with SetArgs/SetOut/SetErr. Apply when u…

- Always use `*E` variants — the non-`E` forms cannot return errors. Key rules:
- Cobra validates positional arguments before `RunE` runs. Never write `len(args)` checks inside `RunE` — that bypasses cobra's standard error messages and arg count tracking.
- Test commands by executing them programmatically. Never use `os.Stdout` / `os.Stderr` directly in command handlers — use `cmd.OutOrStdout()` / `cmd.ErrOrStderr()` so tests can redirect output.
- Always use `RunE`, never `Run`
- Put config initialization in `PersistentPreRunE`
- Validate positional args with `Args`, not inside `RunE`
- Use `cmd.OutOrStdout()` / `cmd.ErrOrStderr()` for all output
- Re-create the command tree per test

Sections: Cobra vs. viper · Command tree · The Run\* family · Args validators · Flags primer · Completions primer · Testing commands · Best Practices · Common Mistakes · Further Reading · Cross-References

References (5): `references/commands-and-args.md`, `references/completions.md`, `references/flags.md`, `references/generators.md`, `references/testing.md`

### `golang-spf13-viper`
> Golang configuration library using spf13/viper — layered precedence (flag > env > file > KV > default), BindPFlag/BindPFlags, SetEnvPrefix + SetEnvKeyReplacer + AutomaticEnv, ReadInConfig + ConfigFileNotFoundError, Unmarshal + mapstructure struct tags, Sub for sub-trees, WatchConfig + OnConfigChange for hot reload, viper.New() for test isolation, and remote KV integration. Apply when using or adop…

- This pipeline is fixed and cannot be reordered. Understanding it prevents most viper bugs: a key that "should" come from a config file may be shadowed by an env var or a flag with a default value.
- `ConfigFileNotFoundError` must be handled gracefully — config files are usually optional. An unhandled error from a missing file crashes programs that are perfectly valid when run with only flags or env vars.
- This is the highest-bug-density area in viper. All three settings must be wired together — missing any one breaks nested key resolution:
- Bind cobra flags to viper in `init()` or `PersistentPreRunE` — never in `RunE` (config loading in `PersistentPreRunE` already ran before `RunE`, so bindings set in `RunE` are missed):
- Always use `mapstructure` tags — implicit mapping is fragile for nested structs and underscore-named fields. Prefer `UnmarshalKey("database", &dbCfg)` over `Sub("database").Unmarshal` — it avoids the nil-check `Sub` requires when the key is missing.
- `viper.Sub("database")` returns a new `*viper.Viper` scoped to the prefix, or nil if the key does not exist — always nil-check before calling methods on the result. Prefer `UnmarshalKey("database", &dbCfg)` which avoids the nil risk entirely.
- Never use the global viper in tests — state leaks across test cases. Use `viper.New()` per test so each instance is isolated:
- Set prefix + key replacer + AutomaticEnv together
- Handle `ConfigFileNotFoundError` gracefully
- Always use `mapstructure` tags on config structs
- Use `viper.New()` in tests, never the global
- Bind flags before `Execute()`

Sections: Viper vs. cobra · The precedence pipeline · Sources and config files · Env binding and key replacers · Flag binding (the cobra seam) · Unmarshaling into structs · Sub-trees · Hot reload · Test isolation · Best Practices · Common Mistakes · Further Reading

References (5): `references/binding-and-env.md`, `references/sources-and-formats.md`, `references/testing-and-isolation.md`, `references/unmarshal.md`, `references/watch-and-reload.md`

## samber/*

### `golang-samber-hot`
> In-memory caching in Golang using samber/hot — eviction algorithms (LRU, LFU, TinyLFU, W-TinyLFU, S3FIFO, ARC, TwoQueue, SIEVE, FIFO), TTL, cache loaders, sharding, stale-while-revalidate, missing key caching, and Prometheus metrics. Apply when using or adopting samber/hot, when the codebase imports github.com/samber/hot, or when the project repeatedly loads the same medium-to-low cardinality reso…

- Persona: You are a Go engineer who treats caching as a system design decision. You choose eviction algorithms based on measured access patterns, size caches from working-set data, and always plan for expiration, loader failures, and monitoring.
- Estimate single-item size
- Ask the developer
- Compute capacity
- Forgetting `WithJanitor()`
- Calling `SetMissing()` without missing cache config
- `WithoutLocking()` + `WithJanitor()`
- Oversized cache
- Ignoring loader errors
- 1. Always set TTL — unbounded caches serve stale data indefinitely because there is no signal to refresh

Sections: Algorithm Selection · Core Usage · Capacity Sizing · Common Mistakes · Best Practices · Cross-References

References (3): `references/algorithm-guide.md`, `references/api-reference.md`, `references/production-patterns.md`

### `golang-samber-lo`
> Functional programming helpers for Golang using samber/lo — 500+ type-safe generic functions for slices, maps, channels, strings, math, tuples, and concurrency (Map, Filter, Reduce, GroupBy, Chunk, Flatten, Find, Uniq, etc.). Core immutable package (lo), concurrent variants (lo/parallel aka lop), in-place mutations (lo/mutable aka lom), lazy iterators (lo/it aka loi for Go 1.23+), and experimental…

- `lom` breaks immutability — only use when allocation pressure is measured, never assumed
- Prefer stdlib when available
- Compose lo functions
- Profile before optimizing
- Use error variants
- Use `lo.Must` only in tests and init

Sections: Why samber/lo · Installation · Choose the Right Package · Core Patterns · Common Mistakes · Best Practices · Quick Reference · Cross-References

References (3): `references/advanced-patterns.md`, `references/api-reference.md`, `references/package-guide.md`

### `golang-samber-mo`
> Monadic types for Golang using samber/mo — Option, Result, Either, Future, IO, Task, and State types for type-safe nullable values, error handling, and functional composition with pipeline sub-packages. Apply when using or adopting samber/mo, when the codebase imports `github.com/samber/mo`, or when considering functional programming patterns as a safety design for Golang. Not for nil-safety and z…

- Key methods: `Some`, `None`, `Get`, `MustGet`, `OrElse`, `OrEmpty`, `Map`, `FlatMap`, `Match`, `ForEach`, `ToPointer`, `IsPresent`, `IsAbsent`.
- Key methods: `Ok`, `Err`, `Errf`, `TupleToResult`, `Try`, `Get`, `MustGet`, `OrElse`, `Map`, `FlatMap`, `MapErr`, `Match`, `ForEach`, `ToEither`, `IsOk`, `IsError`.
- `mo.Do` wraps imperative code in a `Result`, catching panics from `MustGet()` calls:
- // MustGet panics on None/Err — Do catches it as Result error
- a := mo.Some(21).MustGet()
- b := mo.Ok(2).MustGet()
- val := mo.None[int]().MustGet()  // panics
- Prefer `OrElse` over `MustGet`
- Use `TupleToResult` at API boundaries
- Use `Result[T]` for errors, `Either[L, R]` for alternatives
- Option for nullable fields, not zero values
- Chain, don't nest
- Use sub-package pipes for multi-step type transformations

Sections: Core Types at a Glance · Option[T] — Nullable Values Without nil · Result[T] — Error Handling as Values · Either[L, R] — Discriminated Union of Two Types · Do Notation — Imperative Style with Monadic Safety · Pipeline Sub-Packages vs Direct Chaining · Common Patterns · Best Practices · Cross-References

References (6): `references/advanced-types.md`, `references/either.md`, `references/monads-guide.md`, `references/option.md`, `references/pipelines.md`, `references/result.md`

### `golang-samber-oops`
> Structured error handling in Golang with samber/oops — error builders, stack traces, error codes, error context, error wrapping, error attributes, user-facing vs developer messages, panic recovery, and logger integration. Apply when using or adopting samber/oops, or when the codebase already imports github.com/samber/oops.

- Each architectural layer SHOULD add context via Wrap/Wrapf — at least once per package boundary (not necessarily at every function call).
- Error messages MUST be low-cardinality for APM aggregation. Interpolating variable data into the message breaks grouping in Datadog, Loki, Sentry.
- `oops.Recover()` MUST be used in goroutine boundaries. Convert panics to structured errors:

Sections: Why use samber/oops · Core pattern: Error builder chain · Common scenarios · Error wrapping best practices · Panic recovery · Accessing error information · Context propagation · References · Cross-References

References (1): `references/advanced.md`

### `golang-samber-ro`
> Reactive streams and event-driven programming in Golang using samber/ro — ReactiveX implementation with 150+ type-safe operators, cold/hot observables, 5 subject types (Publish, Behavior, Replay, Async, Unicast), declarative pipelines via Pipe, 40+ plugins (HTTP, cron, fsnotify, JSON, logging), automatic backpressure, error propagation, and Go context integration. Apply when using or adopting samb…

- Observable
- Observer
- Operator
- Subscription
- Hot: multiple subscribers share a single execution. Use when the source is expensive (WebSocket, DB poll) or subscribers must see the same events.
- Always handle all three events
- Use `Collect()` for synchronous consumption
- Prefer typed Pipe functions
- Bound infinite streams
- Use `Tap`/`Do` for observability
- Prefer `samber/lo` for simple transforms

Sections: Why samber/ro (Streams vs Slices) · Installation · Core Concepts · Cold vs Hot Observables · Operator Quick Reference · Common Mistakes · Best Practices · Plugin Ecosystem · Cross-References

References (4): `references/operators-guide.md`, `references/patterns.md`, `references/plugin-ecosystem.md`, `references/subjects-guide.md`

### `golang-samber-slog`
> Structured logging extensions for Golang using samber/slog-**** packages — multi-handler pipelines (slog-multi), log sampling (slog-sampling), attribute formatting (slog-formatter), HTTP middleware (slog-fiber, slog-gin, slog-chi, slog-echo), and backend routing (slog-datadog, slog-sentry, slog-loki, slog-syslog, slog-logstash, slog-graylog...). Apply when using or adopting slog, or when the codeb…

- Every samber/slog pipeline follows a canonical ordering. Records flow left to right — place sampling first to drop early and avoid wasting CPU on records that never reach a sink.
- Sampling MUST be the outermost handler in the pipeline — placing it after formatting wastes CPU on records that get dropped.
- // Threshold: log first 10 per 5s, then 10% — errors always pass through via Router
- slog-formatter processes attributes sequentially — many formatters compound. For hot-path attribute formatting, prefer implementing `slog.LogValuer` on your types instead
- Sample first, format second, route last
- Use Pipe for cross-cutting concerns
- Test pipelines with `slogmulti.NewHandleInlineHandler`
- Use `AttrFromContext`
- Prefer Router over Fanout

Sections: The Pipeline Model · Core Libraries · slog-multi — Handler Composition · slog-sampling — Throughput Control · slog-formatter — Attribute Transformation · HTTP Middlewares · Backend Sinks · Common Mistakes · Performance Warnings · Best Practices · Cross-References

References (4): `references/backend-handlers.md`, `references/http-middlewares.md`, `references/pipeline-patterns.md`, `references/sampling-strategies.md`

## Testing

### `golang-stretchr-testify`
> Comprehensive guide to stretchr/testify for Golang testing. Covers assert, require, mock, and suite packages in depth. Use when writing tests with testify, creating mocks, setting up test suites, or choosing between assert and require. Covers testify assertions, mock expectations, argument matchers, call verification, suite lifecycle, and advanced patterns like Eventually, JSONEq, and custom match…

- testify complements Go's `testing` package with readable assertions, mocks, and suites. It does not replace `testing` — always use `*testing.T` as the entry point.
- Use `assert.New(t)` / `require.New(t)` for readability. Name them `is` and `must`:
- must := require.New(t)
- must.NoError(err)    // stop if parsing fails — cfg would be nil
- must.NotNil(cfg)
- Rule: `require` for preconditions (setup, error checks), `assert` for verifications. Never mix randomly.
- Argument order: always `(expected, actual)` — swapping produces confusing diff output.
- Mock interfaces to isolate the unit under test. Embed `mock.Mock`, implement methods with `m.Called()`, always verify with `AssertExpectations(t)`.

Sections: assert vs require · Core Assertions · Advanced Assertions · testify/mock · testify/suite · Common Mistakes · Linters · Cross-References

References (1): `references/mock.md`

## Orchestrator

### `golang-how-to`
> Golang skills orchestrator — always active on any Golang coding, review, debug, or setup task. Reads the task context and loads the most relevant skills from samber/cc-skills-golang, often multiple at once: writing a gRPC service loads golang-grpc + golang-testing + golang-error-handling; debugging a panic loads golang-troubleshooting + golang-safety; auditing security loads golang-security + gola…

- Configure — write the always-load directive for `golang-how-to` itself, plus an optional `## Required Go skills` block, to the project's agent-config file (CLAUDE.md, AGENTS.md, or equivalent). Follow [project-config.md](references/project-config.md).
- Questions: In Configure mode, ask the user through the environment's question tool — never as plain-text prose. One question at a time, wait for the answer. If the environment has no question tool, ask in prose with the same options.
- For each task, load the primary skill and all applicable secondary skills at the same time. Do not wait — load them together at the start.
- Four tools can answer "is this dependency OK to use," and they don't overlap as much as they look:
- `godig` answers questions about the published ecosystem: any Go package or module, whether or not it's in your `go.mod` yet — it calls the remote pkg.go.dev API and never touches your local checkout. Its `vulns` command reports CVEs known for a package/version in isolation, regardless of whether your build actually reaches the vulnerable code path.
- Performance: `golang-performance` (optimization patterns) · `golang-benchmark` (measurement) · `golang-troubleshooting` (root cause) · `golang-observability` (always-on production)
- Write an always-load directive for `golang-how-to` itself to the project's agent-config file (CLAUDE.md, AGENTS.md, GEMINI.md, Cursor rules, or Copilot instructions — whichever the project's harness reads), and optionally force-trigger specific secondary skills too.

Sections: Skill loading · Code navigation with gopls · `godig` vs gopls vs Context7 vs govulncheck · Categories at a glance · Competing clusters — boundary lines · Configure mode

References (3): `references/by-category.md`, `references/disambiguation.md`, `references/project-config.md`
