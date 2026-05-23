# Repository Guidelines

## Project Structure & Module Organization
`cmd/immich-public-proxy/` contains the application entrypoint. Core packages live under `internal/`: `app` loads runtime config, `config` parses repository and inline settings, `immich` wraps upstream API calls, `render` builds HTML responses, `server` defines routes and HTTP behavior, and `session` manages password-protected share sessions. Static assets are in `public/`, HTML templates are in `templates/`, and longer operational docs live in `docs/`.

## Build, Test, and Development Commands
Use the Go toolchain for local development:

- `go run ./cmd/immich-public-proxy` starts the proxy with environment-driven config.
- `go test ./...` runs the full unit test suite across `internal/*`.
- `go test ./internal/server -run TestPasswordRequiredRendersPasswordPage` runs a focused test while iterating.
- `gofmt -w cmd internal` formats the Go source in place.
- `docker compose up -d` starts the containerized app from `docker-compose.yml` for end-to-end checks.

## Coding Style & Naming Conventions
Follow standard Go formatting and imports; use tabs via `gofmt`, not manual spacing. Keep packages small and cohesive under `internal/`. Exported identifiers use `CamelCase`; unexported helpers use `camelCase`. Prefer table-free, descriptive test names like `TestDownloadAllReturnsZip`. Keep templates and static asset names lowercase with hyphens or simple words, for example `templates/password.html` and `public/style.css`.

## Testing Guidelines
Tests sit next to the code they cover as `*_test.go` files and use the standard `testing` package with `httptest` for HTTP behavior. Add coverage for new routes, config parsing branches, and rendering changes. For bug fixes, add a regression test in the affected package before or alongside the code change. Run `go test ./...` before opening a pull request.

## Commit & Pull Request Guidelines
Recent commits use short, imperative subjects such as `Refactor session storage to encrypt share passwords`. Keep that style: one line, present tense, focused on the user-visible change or refactor. Pull requests should include a concise description, test evidence (`go test ./...`, manual Docker check, or both), and screenshots when changing templates, gallery output, or other rendered pages. Link related issues or docs updates when applicable.

## Configuration & Security Tips
Do not commit real Immich URLs, secrets, or session keys. Keep local overrides in environment variables or an untracked config file based on `config.json`. When changing proxy behavior, verify that password-protected shares, download rules, and response headers still behave safely.
