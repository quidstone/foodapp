# Food Delivery Backend

Backend API and ETL for a food delivery platform using Go, PostgreSQL, and a simple Python loader for the provided JSON datasets.

## Quick start (Docker)
- `docker-compose up --build` starts PostgreSQL and the Go API on `:8080` with `fooduser/foodpass@fooddb`.
- Seed data after the containers are up: `docker-compose exec api python3 /app/ETL/etl_load_data.py` (requires `psycopg2` in the image; see ETL section below for local run).

## Running locally (without Docker)
1) Start PostgreSQL and apply the schema from `db/init.sql` (the compose file mounts it automatically). Defaults: host `localhost`, port `5432`, db `fooddb`, user `fooduser`, password `foodpass`.
2) From `backend/`: `go run main.go` (environment variables `POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`, `PORT` override defaults).

## ETL
The raw datasets live in `ETL/restaurant_with_menu.json` and `ETL/users_with_purchase_history.json`.
- Install deps locally: `python3 -m venv .venv && source .venv/bin/activate && pip install psycopg2-binary`
- Load data: `cd ETL && POSTGRES_HOST=localhost POSTGRES_PORT=5432 POSTGRES_USER=fooduser POSTGRES_PASSWORD=foodpass POSTGRES_DB=fooddb python3 etl_load_data.py`
- Environment variables `RESTAURANTS_JSON` and `USERS_JSON` let you point to different input files.

## API
- OpenAPI stub: `openapi.yaml` (paths for health, metrics, open restaurants, top restaurants by dish count, search, and purchase).
- Default base URL in Docker: `http://localhost:8080`.
- Example requests:
  - `GET /restaurants/open?datetime=2024-01-15T14:30:00Z`
  - `GET /restaurants/top?limit=5&dish_count=10&min_price=5&max_price=20&comparison=more`
  - `GET /search?q=pizza&limit=10`
  - `POST /purchase` with header `Idempotency-Key: <uuid>` (optional) and body:
    ```json
    {
      "user_id": 1,
      "items": [
        {"menu_item_id": 123, "quantity": 2},
        {"menu_item_id": 456, "quantity": 1}
      ]
    }
    ```
  - `GET /metrics` to retrieve in-memory API and DB metrics.

## Optional caching/metrics
- Set `ENABLE_CACHE=true` to enable the in-memory query cache (default TTL 5 minutes) in the API.
- `/metrics` exposes in-memory counts/latencies for API endpoints and DB queries; shape documented in `openapi.yaml`.

## Project layout
- `backend/` Go API server (PostgreSQL, sqlx).
- `db/init.sql` database schema and search triggers.
- `ETL/etl_load_data.py` loader for the provided JSON datasets.
- `docker-compose.yml` Postgres + API stack; mounts `db/init.sql`.
