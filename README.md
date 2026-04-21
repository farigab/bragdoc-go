# Bragdoc

> Bragdoc is a lightweight Go backend service that uses GitHub OAuth to fetch
> user data and generate human-readable reports using a generative model
> (Gemini). It exposes a JSON HTTP API, persists users and refresh tokens in
> PostgreSQL, and uses JWT + rotating refresh tokens stored in HttpOnly cookies
> for session management.

## Notable changes

- Added GitHub import endpoints to list and import repositories and count commits.
- Added endpoints to save/clear a user's GitHub personal access token manually.
- Added report generation endpoint that accepts a custom prompt and returns an
  AI-generated summary (via Gemini client).
- Access tokens (JWT) are short-lived (15 minutes) and refresh tokens are
  rotated (7 days); expired refresh tokens are cleaned hourly by a background job.
- Gemini client is configurable via `GEMINI_API_KEY`, `GEMINI_API_URL` and
  `GEMINI_MODEL` and uses a sensible default generation configuration.
- The app can run SQL migrations automatically when `MIGRATE=true`.

## Features

- GitHub OAuth authentication and user upsert
- Manual GitHub token storage (per-user) and import helpers
- PostgreSQL persistence for users and refresh tokens
- JWT-based session management with rotating refresh tokens
- Report generation via Gemini (configurable model/API URL)
- Automatic DB migrations (optional at startup)
- CORS middleware and request logging

## Prerequisites

- Go 1.21 or newer
- PostgreSQL database
- GitHub OAuth app (client ID + secret) or a personal access token
- Gemini API key (or another supported generative API)
- Docker (optional)

## Quick Start (local)

1. Clone the repository:

```bash
git clone https://github.com/farigab/bragdoc.git
cd bragdoc
```

1. Create a `.env` file or export environment variables. Example `.env`:

```env
# Database
DB_URL=postgres://user:password@localhost:5432/bragdoc?sslmode=disable

# App
JWT_SECRET=replace-with-a-secure-random-string
PORT=8080

# GitHub OAuth
GITHUB_OAUTH_CLIENT_ID=your_github_client_id
GITHUB_OAUTH_CLIENT_SECRET=your_github_client_secret
GITHUB_OAUTH_REDIRECT_URI=http://localhost:8080/api/auth/callback

# Frontend redirect (where the app should redirect after OAuth)
OAUTH_FRONTEND_REDIRECT=http://localhost:4200

# Gemini / generative model
GEMINI_API_KEY=your_gemini_api_key
GEMINI_API_URL=https://generativelanguage.googleapis.com/v1
GEMINI_MODEL=gemini-2.5-flash

# Cookie options
APP_COOKIE_DOMAIN=localhost
APP_COOKIE_SECURE=false
APP_COOKIE_SAME_SITE=Lax

# Migrate on startup (optional)
MIGRATE=true
```

1. Run the app (development):

```bash
# run directly with Go (reads .env via godotenv)
go run ./cmd/bragdoc

# or build and run (recommended for production-like runs)
go build -o bin/bragdoc ./cmd/bragdoc
MIGRATE=true DB_URL="$DB_URL" JWT_SECRET="$JWT_SECRET" ./bin/bragdoc
```

The server listens on the port specified by `PORT` (defaults to `8080`). On
Windows there is a helper script `run.bat` which will load `.env` (if present)
and run the server.

## API Endpoints

All API endpoints are under `/api`. Authentication is performed via an
HttpOnly cookie named `token` (JWT). A rotating `refreshToken` cookie is used
to obtain new JWTs.

- `GET /api/health` — basic health check, returns `{"status":"ok"}`.
- `GET /api/auth/github` — redirects to GitHub OAuth authorization URL.
- `GET /api/auth/callback` — OAuth callback: exchanges code, upserts user,
  issues `token` + `refreshToken` cookies and redirects to `OAUTH_FRONTEND_REDIRECT`.
- `POST /api/auth/refresh` — rotates the refresh token and returns 200 on
  success (requires `refreshToken` cookie).
- `POST /api/auth/logout` — revokes refresh tokens for the current user and
  clears auth cookies.
- `POST /api/auth/github/token` — persist a GitHub personal access token for
  the authenticated user. Body: `{ "token": "<your_token>" }`.
- `DELETE /api/auth/github/token` — remove stored GitHub token for the user.
- `GET /api/user` — returns the authenticated user's `login`, `name`,
  `avatarUrl` and whether a GitHub token is stored.
- `POST /api/github/import/repositories` — returns the authenticated user's
  repositories (requires a stored GitHub token).
- `POST /api/github/import` — import repositories and count commits. Request
  body example:

```json
{
  "repositories": ["owner/repo"],
  "dataInicio": "2024-01-01",
  "dataFim": "2024-12-31"
}
```

- `POST /api/reports/ai-summary/custom` — generate an AI report. Request body
  example:

```json
{
  "reportType": "monthly-summary",
  "category": "engineering",
  "startDate": "2024-03-01",
  "endDate": "2024-03-31",
  "userPrompt": "Highlight the most important work done this month.",
  "repositories": ["owner/repo1", "owner/repo2"]
}
```

The report endpoint returns a JSON object containing `aiGeneratedReport`,
`generatedAt` (RFC3339) and the supplied `reportType`.

## Authentication, tokens and cookies

- Access token (cookie `token`): JWT signed with `JWT_SECRET`, default
  expiry is 15 minutes.
- Refresh token (cookie `refreshToken`): rotates on `POST /api/auth/refresh` and
  is persisted in the database for 7 days by default. Refresh tokens are used to
  obtain new access tokens without re-running the OAuth flow.
- OAuth CSRF cookie `oauth_state` is used during the GitHub OAuth handshake and
  has a short lifetime (5 minutes).
- Cookies are set with `HttpOnly` and `SameSite=Lax` by default. Set
  `APP_COOKIE_SECURE=true` and a proper `APP_COOKIE_DOMAIN` for production.

## Configuration (environment variables)

- `DB_URL` — Postgres connection URL (required)
- `JWT_SECRET` — secret used to sign JWTs (required)
- `GITHUB_OAUTH_CLIENT_ID` — GitHub OAuth client ID
- `GITHUB_OAUTH_CLIENT_SECRET` — GitHub OAuth client secret
- `GEMINI_API_KEY` — API key for the generative model
- `GEMINI_API_URL` — Base URL for the generative API (defaults to
  `https://generativelanguage.googleapis.com/v1`)
- `GEMINI_MODEL` — Model to request from the generative API (defaults to
  `gemini-2.5-flash`)
- `PORT` — HTTP port (default: `8080`)
- `MIGRATE` — if `true`, run SQL migrations on startup
- `GITHUB_OAUTH_REDIRECT_URI` — OAuth callback URL (overrides default)
- `OAUTH_FRONTEND_REDIRECT` — frontend URL to redirect after OAuth
- `APP_COOKIE_DOMAIN`, `APP_COOKIE_SECURE`, `APP_COOKIE_SAME_SITE` — cookie settings
- `LOG_LEVEL` — application log level (default: `info`)

## Docker

Build the image and run with environment variables:

```bash
docker build -t bragdoc .

docker run -p 8080:8080 \
  -e DB_URL="postgres://user:pass@db:5432/bragdoc?sslmode=disable" \
  -e JWT_SECRET="your_jwt_secret" \
  -e GITHUB_OAUTH_CLIENT_ID="id" \
  -e GITHUB_OAUTH_CLIENT_SECRET="secret" \
  -e GEMINI_API_KEY="key" \
  bragdoc
```

The provided `Dockerfile` builds a static Linux binary and copies SQL
migrations into the image. The container runs as a non-root user and exposes
port `8080`.

## Database migrations

Migrations are SQL files in `db/migrations`. The application will run them on
startup when the environment variable `MIGRATE=true` is set. You can also run
them manually against your database using `psql` or another migration tool.

The initial migration creates the `users` and `refresh_tokens` tables.

## Project Structure

- `cmd/bragdoc` — application entrypoint
- `internal/config` — env configuration loader
- `internal/handlers` — HTTP handlers and routing
- `internal/integration` — external integrations (GitHub, Gemini, etc.)
- `internal/middleware` — CORS and auth middleware
- `internal/repository` — Postgres repository implementations
- `internal/security` — JWT service and auth helpers
- `db/migrations` — SQL migration files
- `report` — report/prompt builder logic

## Development notes

- The app uses `github.com/joho/godotenv` to load a local `.env` file when
  present (only for local development). On Windows use `run.bat` to run the
  server with `.env`.
- Background cleanup of expired refresh tokens runs every hour.

## Contributing

Contributions are welcome. Open an issue to discuss changes or submit a pull
request with a clear description and tests where appropriate.

## License

This project is licensed under the MIT License — see the [LICENSE](LICENSE) file for details.

## Contact

Maintainer: @farigab — <https://github.com/farigab>
