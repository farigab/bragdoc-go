# Bragdoc

> Bragdoc is a small Go backend service that uses GitHub OAuth to fetch user data
> and generate reports using a generative model (Gemini). It exposes a JSON API,
> persists users and refresh tokens in PostgreSQL, and uses JWT cookies for
> authentication.

## Features

- GitHub OAuth authentication and token storage
- PostgreSQL persistence for users and refresh tokens
- JWT-based session management with refresh tokens
- Report generation via Gemini (configurable model/API URL)
- Automatic DB migrations (optional at startup)
- CORS middleware and basic health check endpoint

## Prerequisites

- Go 1.21 or newer
- PostgreSQL database
- GitHub OAuth app (client ID + secret)
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

The server listens on the port specified by `PORT` (defaults to `8080`).

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

The provided Dockerfile builds a static Linux binary and copies SQL migrations
into the image. The container runs as a non-root user and exposes port `8080`.

## Database migrations

Migrations are SQL files in `db/migrations`. The application can run them on
startup when the environment variable `MIGRATE=true` is set. You can also run
them manually against your database using `psql` or another migration tool.

The initial migration creates two tables:

- `users` — stores GitHub user info and access token
- `refresh_tokens` — stores refresh tokens with expiration and revoke status

## Configuration (environment variables)

- `DB_URL` — Postgres connection URL (required)
- `JWT_SECRET` — secret used to sign JWTs (required)
- `GITHUB_OAUTH_CLIENT_ID` — GitHub OAuth client ID
- `GITHUB_OAUTH_CLIENT_SECRET` — GitHub OAuth client secret
- `GEMINI_API_KEY` — API key for the generative model
- `GEMINI_API_URL` — Base URL for the generative API (optional)
- `GEMINI_MODEL` — Model to request from the generative API (optional)
- `PORT` — HTTP port (default: `8080`)
- `MIGRATE` — if `true`, run SQL migrations on startup
- `GITHUB_OAUTH_REDIRECT_URI` — OAuth callback URL (overrides default)
- `OAUTH_FRONTEND_REDIRECT` — frontend URL to redirect after OAuth
- `APP_COOKIE_DOMAIN`, `APP_COOKIE_SECURE`, `APP_COOKIE_SAME_SITE` — cookie settings

## API Overview

- `GET /api/health` — basic health check
- Auth flow and protected routes are defined under `/api/*` and require the
  JWT cookie after authentication.

Refer to the handlers in `internal/handlers` for concrete endpoints and
behaviour.

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
  present (only for local development).
- Background cleanup of expired refresh tokens runs every hour.

## Contributing

Contributions are welcome. Open an issue to discuss changes or submit a pull
request with a clear description and tests where appropriate.

## License

This project is licensed under the MIT License — see the [LICENSE](LICENSE) file for details.

## Contact

Maintainer: @farigab — <https://github.com/farigab>
