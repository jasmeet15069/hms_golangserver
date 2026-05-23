# Hotel Harmony Go API

Repository: `hms_golangserver`

## Run

1. Install Go 1.22+ and PostgreSQL.
2. Copy `.env.example` to `.env` and fill secrets.
3. Create a database and run `migrations/001_init.sql`.
4. Start the server:

```powershell
.\scripts\run-dev.ps1
```

Or run directly:

```sh
go run ./cmd/server
```

Health check: `GET /health`
API root: `GET /api`

## Optimization Notes

- Fiber server with recovery, request IDs, security headers, ETags, gzip compression, CORS, and rate limiting.
- PostgreSQL uses `pgxpool` with statement caching and tuned pool defaults.
- Dashboard stats run as one CTE query and are cached.
- Room list reads are cached and invalidated after room changes.
- Stripe booking/payment checkout has short idempotency locks.
- External API calls use timeouts and cached exchange rates.
- Groq AI calls use retries, circuit breaking, and local fallbacks.
- Migration includes indexes for high-traffic room, booking, payment, order, complaint, and inventory reads.

## Docker

```sh
docker compose -f deployments/docker/docker-compose.yml up --build
```
