# GitHub to Resume

A Go web application that turns a public GitHub profile into an editable, professional developer resume and exports it as a PDF.

1. Enter a GitHub username or profile URL.
2. The server fetches the public profile and repositories from the GitHub REST API and ranks them (recent, documented and non-trivial projects first).
3. You choose which repositories to include and can add contact details, background and a target job description.
4. The server sends sanitized evidence (metadata, language stats and README excerpts) to Claude, which returns structured JSON. The JSON is validated before anything is shown.
5. You edit every section in a live-preview editor, pick a template (ATS-friendly, modern or minimalist) and download a PDF.

Without an Anthropic API key, the app still works: it builds a facts-only draft from GitHub data that you finish in the editor.

## Repository layout

```
code/      Go module: all source code, templates, static assets and tests
build/     Compiled binaries (created by the build scripts; not committed)
build.sh   Build script for Linux, macOS and Git Bash
build.ps1  Build script for Windows PowerShell
```

Inside `code/`:

| Path | Purpose |
|---|---|
| `cmd/server/` | Entry point: wiring, HTTP server, graceful shutdown |
| `internal/config/` | Environment-based configuration (plus optional `.env` file) |
| `internal/safehttp/` | Outbound HTTP client restricted to an allowlist of hosts (SSRF guard) |
| `internal/github/` | GitHub REST client: users, repos (paginated), README, languages, caching, rate-limit errors |
| `internal/ai/` | Claude Messages API client over `net/http` with structured JSON output |
| `internal/resume/` | Domain model, repo ranking, evidence sanitizing, prompt, AI-output validation, generation service |
| `internal/pdf/` | Dependency-free PDF writer and the three resume templates |
| `internal/handler/` | HTTP handlers |
| `internal/middleware/` | Logging, panic recovery, security headers, same-origin check, per-IP rate limiting |
| `internal/web/` | Embedded `html/template` pages, CSS and JavaScript |
| `tests/` | End-to-end tests against fake GitHub and Claude servers, with fixtures in `tests/testdata/` |

## Requirements

- Go 1.22 or newer (developed with Go 1.26). Nothing else: no C compiler, no Node.js, no database.
- Optional: an Anthropic API key for AI generation.
- Optional: a GitHub token for higher rate limits.

## Build

All builds use `CGO_ENABLED=0`, so binaries are fully static and cross-compile from any OS.

### Windows (PowerShell)

```powershell
.\build.ps1          # builds build\resume-generator.exe for this machine
.\build.ps1 all      # cross-compiles Linux, Windows and macOS binaries into build\
.\build.ps1 test     # gofmt check, go vet, go test
```

If script execution is blocked, run `powershell -ExecutionPolicy Bypass -File .\build.ps1`.

### Linux / macOS (or Git Bash on Windows)

```sh
./build.sh           # builds build/resume-generator for this machine
./build.sh all       # cross-compiles all targets into build/
./build.sh test      # gofmt check, go vet, go test
```

### Manually with the Go toolchain

```sh
cd code
go build -o ../build/resume-generator ./cmd/server                 # Linux/macOS
go build -o ../build/resume-generator.exe ./cmd/server             # Windows
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o ../build/resume-generator-linux-amd64 ./cmd/server
```

In PowerShell, set the target with `$env:GOOS="linux"; $env:GOARCH="amd64"` before `go build`.

`all` produces:

```
build/resume-generator-linux-amd64
build/resume-generator-linux-arm64
build/resume-generator-windows-amd64.exe
build/resume-generator-darwin-amd64
build/resume-generator-darwin-arm64
```

Templates and static files are embedded with `go:embed`, so each binary is self-contained.

## Run

```sh
cp .env.example .env              # then edit .env; or export the variables directly
./build/resume-generator          # Linux/macOS
```

```powershell
Copy-Item .env.example .env
.\build\resume-generator.exe      # Windows
```

Open http://localhost:8080. The `.env` file is read from the current working directory, and real environment variables take precedence over it. `resume-generator -version` prints the build version.

For development without building: `cd code && go run ./cmd/server`. In that case put `.env` in `code/`.

## Configuration

| Variable | Default | Description |
|---|---|---|
| `ANTHROPIC_API_KEY` | (unset) | Enables AI generation. Without it, resumes are facts-only drafts. |
| `ANTHROPIC_MODEL` | `claude-opus-5-5` | Claude model ID. |
| `ANTHROPIC_EFFORT` | `medium` | `low`, `medium`, `high`, `xhigh` or `max`. Higher effort means more thinking tokens and cost. |
| `AI_TIMEOUT` | `180s` | Timeout for one Claude request. |
| `GITHUB_TOKEN` | (unset) | Optional token. Raises GitHub's limit from 60 to 5,000 requests/hour. Needs no scopes. |
| `GITHUB_TIMEOUT` | `15s` | Timeout per GitHub request. |
| `ADDR` | `:8080` | Listen address. |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`. Logs are JSON on stdout. |
| `TRUST_PROXY` | `false` | Use `X-Forwarded-For` for rate limiting. Enable only behind a trusted reverse proxy. |
| `MAX_REPOS` | `30` | Repositories listed for selection (1–100). |
| `MAX_SELECTED_REPOS` | `8` | Repositories analyzed per resume (1–15). |
| `MAX_README_CHARS` | `6000` | README characters kept per repository. |
| `RATE_LIMIT_PER_MINUTE` | `30` | Per-IP limit for analyze, generate and PDF endpoints. |
| `MAX_CONCURRENT_GENERATIONS` | `4` | Simultaneous generations. Extra requests get a "busy" response. |

`GITHUB_API_URL` and `ANTHROPIC_BASE_URL` can point at alternative hosts (used by tests). The outbound allowlist is built from these two URLs and nothing else.

## How GitHub API access works

- The app uses only public, unauthenticated-compatible REST endpoints: `/users/{user}`, `/users/{user}/repos`, `/repos/{owner}/{repo}/readme` and `/repos/{owner}/{repo}/languages`.
- Without a token, GitHub allows **60 requests per hour per server IP**. Analyzing a profile costs 2 requests; generating costs 2 + 2 per selected repository (up to 18 with the default limit of 8). Responses are cached in memory for 10 minutes, so going from analysis to generation doesn't re-fetch the profile.
- With `GITHUB_TOKEN`, the limit is 5,000/hour. A fine-grained personal access token with **no repository permissions** is enough. The app never requests write access.
- When GitHub's limit is hit, the user sees when it resets. Nonexistent users, empty profiles, missing READMEs and blocked repositories are handled individually.
- GitHub OAuth (for private repositories) isn't implemented. It would be a later phase and should request only the minimum read-only access actually needed.

## How Claude API billing works

- Each **Generate** click makes **one** Messages API request. You're billed per input and output token at the model's rate (`claude-opus-5-5`: $4 per million input tokens, $20 per million output tokens at the time of writing; see Anthropic's pricing page for current prices).
- A typical request with 8 repositories is roughly 10–20k input tokens (README excerpts dominate) and a few thousand output tokens including thinking, which is usually well under US$0.25 per resume. Lower `MAX_README_CHARS`, `MAX_SELECTED_REPOS` or `ANTHROPIC_EFFORT` to reduce cost.
- Token usage is logged for each generation (`input_tokens`, `output_tokens`) so you can track spend.
- Viewing, editing and PDF export don't call Claude.
- The request uses structured outputs (`output_config.format` with a JSON schema), adaptive thinking, and for supported models the server-side refusal fallback (`fallbacks: "default"`). Responses with `stop_reason` `refusal` or `max_tokens` are reported as errors, never shown half-parsed.

## Accuracy guarantees

- Experience, education and certifications are **never** produced by the AI. They come only from what you enter in the editor.
- Project URLs, demo links and facts (languages, stars, last update) come from GitHub, not from the model. Projects that reference a repository you didn't select are dropped.
- The model is instructed not to invent metrics or employers and not to treat stars or language percentages as proficiency. It must list its interpretations under **AI inferences to check**, and ask for missing information under **Information to add**. The editor shows both.
- AI output is decoded strictly (unknown fields rejected), length-limited and validated before rendering.

## Security

- **SSRF:** all outbound requests go through `internal/safehttp`, which only allows the configured GitHub and Anthropic hosts (HTTPS only; plain HTTP only for loopback test servers), including on redirects.
- **Untrusted repository content:** README text is stripped of HTML comments, badges and control characters, truncated, JSON-encoded and placed inside `<untrusted_repository_data>` tags. The system prompt tells the model to treat it as data, not instructions. Repository code is never fetched or executed.
- **XSS:** pages use `html/template`; the browser renders resumes with DOM APIs (`textContent`) only; links are limited to `http(s)` and `mailto`; a strict Content-Security-Policy allows scripts only from the app's own origin.
- **CSRF:** state-changing requests from another origin are rejected (`Origin` / `Sec-Fetch-Site` checks).
- **Abuse limits:** per-IP token-bucket rate limiting, a concurrency cap on generations, request body limits (64 KB forms, 256 KB JSON), response size limits on upstream calls, and timeouts on everything.
- **Secrets:** API keys come only from the environment and aren't logged or sent to the browser. Logs record method, path, status and duration, not query strings or bodies.
- **Shutdown:** `SIGINT`/`SIGTERM` drains in-flight requests for up to 20 seconds.

## Data storage and retention

The server stores **nothing** on disk and uses **no database**. SQLite was considered and left out because the MVP doesn't need it:

- Generated resumes are returned to the browser and saved in that browser's `localStorage`, so they survive a page reload. **Delete saved data** in the editor removes them. **Export JSON** / **Import JSON** let you move a resume between browsers.
- PDFs are generated in memory per request and streamed back. Nothing is written to disk.
- The only server-side state is an in-memory GitHub response cache (10-minute TTL, public data only) and rate-limiter counters, both lost on restart.

If persistence is added later (accounts, shareable links), the plan is SQLite behind a small `internal/storage` package with versioned migrations and an explicit retention period.

## PDF generation

`internal/pdf` writes PDF 1.4 directly with the Go standard library (`compress/zlib` for streams). It uses the 14 standard PDF fonts (Helvetica family) that every PDF reader includes, so:

- no headless browser, wkhtmltopdf, Node.js or font files are required;
- text is real, selectable text, which ATS parsers can read; links are clickable annotations; long content flows onto new pages with headings kept with their content.

The trade-off is that the standard fonts cover the Windows-1252 character set (Western European languages plus typographic punctuation). Other characters (for example CJK) are printed as `?`. For full Unicode, the renderer would need TrueType font embedding. In the meantime, the editor's **Print** button uses the browser's print-to-PDF, which supports every script.

To download a PDF: in the editor, choose a template and paper size (A4 or US Letter), then click **Download PDF**.

## Dependencies

**None outside the Go standard library.** `go.mod` has no `require` entries. The two places where a third-party package would normally be used are covered in-house:

- **SQLite driver:** not needed because nothing is persisted (see above).
- **PDF library:** replaced by the small standard-library PDF writer described above.

## Testing

```sh
cd code
gofmt -l .                     # should print nothing
go vet ./...
go test ./...                  # all tests
go test ./internal/resume -run TestParseAIOutput -v    # a single test
go test ./tests -v             # end-to-end tests only
```

The end-to-end tests in `code/tests` start the real server stack against fake GitHub and Claude servers. They cover analysis, generation, prompt-injection stripping, hallucinated-project removal, PDF export, malformed AI output, GitHub 404 and rate limits, network failure and cross-site requests. Setting `PDF_OUT=<dir>` when running `go test ./internal/pdf` writes sample PDFs for every template to that directory.

## Deployment

The binary is self-contained. Typical setup:

1. Build for the target platform: `./build.sh all`, then copy, for example, `build/resume-generator-linux-amd64` to the server.
2. Provide configuration through environment variables (systemd `EnvironmentFile=`, container env, etc.).
3. Run it behind a reverse proxy (Caddy, nginx) that terminates TLS, and set `TRUST_PROXY=true` so rate limiting sees real client IPs.
4. Use `GET /healthz` for health checks.

Minimal container image:

```dockerfile
FROM golang:1.26 AS build
WORKDIR /src
COPY code/ .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /resume-generator ./cmd/server

FROM gcr.io/distroless/static
COPY --from=build /resume-generator /resume-generator
EXPOSE 8080
ENTRYPOINT ["/resume-generator"]
```
