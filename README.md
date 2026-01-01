# Food Delivery Backend

Backend API and ETL for a food delivery platform using Go, PostgreSQL, and Python for data loading.

## Features

- **Restaurant Discovery**: Find open restaurants by datetime with timezone support
- **Advanced Search**: Full-text search with prefix matching, fuzzy/typo tolerance (trigram similarity)
- **Menu Filtering**: Filter restaurants by dish count and price range
- **Purchase Transactions**: ACID-compliant orders with idempotency keys to prevent duplicates
- **Performance Monitoring**: Built-in metrics and optional query result caching

## Prerequisites

- **Docker & Docker Compose** (recommended) OR:
  - Go 1.22+
  - Python 3.11+
  - PostgreSQL 16+ with `pg_trgm` extension

## Quick Start (Docker)

```bash
# Start all services (PostgreSQL, ETL, API)
docker-compose up --build

# API available at: http://localhost:8080
# Database: localhost:5432 (fooduser/foodpass@fooddb)
```

The ETL service automatically loads seed data on first run.

## Local Development (without Docker)

### 1. Database Setup

```bash
# Start PostgreSQL and create the database
psql -U postgres -c "CREATE DATABASE fooddb;"
psql -U postgres -c "CREATE USER fooduser WITH PASSWORD 'foodpass';"
psql -U postgres -c "GRANT ALL PRIVILEGES ON DATABASE fooddb TO fooduser;"

# Apply schema
psql -U fooduser -d fooddb -f db/init.sql
```

### 2. Load Data (ETL)

```bash
cd ETL
python3 -m venv .venv && source .venv/bin/activate
pip install -r requirements.txt

POSTGRES_HOST=localhost POSTGRES_PORT=5432 POSTGRES_USER=fooduser \
POSTGRES_PASSWORD=foodpass POSTGRES_DB=fooddb \
python3 etl_load_data.py
```

### 3. Run the API Server

```bash
cd backend
go run main.go
```

## API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/health` | Database connectivity check |
| GET | `/metrics` | API and DB performance metrics |
| GET | `/restaurants/open` | List restaurants open at a given datetime |
| GET | `/restaurants/top` | Top restaurants by dish count in price range |
| GET | `/search` | Search restaurants and menu items |
| POST | `/purchase` | Create a purchase order (idempotent) |

### Example Requests

**List open restaurants:**
```bash
curl "http://localhost:8080/restaurants/open?datetime=2024-01-15T14:30:00Z"
```

**Top restaurants by dish count:**
```bash
curl "http://localhost:8080/restaurants/top?limit=5&dish_count=10&min_price=5&max_price=20&comparison=more"
```

**Search restaurants and dishes:**
```bash
curl "http://localhost:8080/search?q=pizza&limit=10"
```

**Create a purchase:**
```bash
curl -X POST http://localhost:8080/purchase \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: $(uuidgen)" \
  -d '{
    "user_id": 1,
    "items": [
      {"menu_item_id": 123, "quantity": 2},
      {"menu_item_id": 456, "quantity": 1}
    ]
  }'
```

See `openapi.yaml` for full API documentation.

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `POSTGRES_HOST` | `db` | Database host |
| `POSTGRES_PORT` | `5432` | Database port |
| `POSTGRES_USER` | `fooduser` | Database user |
| `POSTGRES_PASSWORD` | `foodpass` | Database password |
| `POSTGRES_DB` | `fooddb` | Database name |
| `PORT` | `8080` | API server port |
| `ENABLE_CACHE` | `false` | Enable in-memory query cache (5-min TTL) |
| `RESTAURANTS_JSON` | `restaurant_with_menu.json` | ETL input file |
| `USERS_JSON` | `users_with_purchase_history.json` | ETL input file |

## Testing

### Backend Tests

```bash
cd backend
go test -v ./...
```

### ETL Tests

```bash
cd ETL
python -m pytest -v
```

## Project Structure

```
.
├── backend/                    # Go API server
│   ├── main.go                # Routes and handlers
│   ├── main_test.go           # Integration tests
│   └── internal/
│       ├── db/                # Database repositories
│       │   ├── restaurants.go # Restaurant queries
│       │   ├── users.go       # User & purchase logic
│       │   ├── wrapper.go     # DB wrapper with metrics/cache
│       │   └── cache.go       # Query result cache
│       └── metrics/           # Performance monitoring
│
├── ETL/                       # Python data loader
│   ├── etl_load_data.py      # Main ETL script
│   ├── test_etl_load_data.py # Unit tests
│   ├── requirements.txt
│   └── *.json                 # Raw datasets
│
├── db/
│   └── init.sql              # Database schema, indexes, triggers
│
├── docker-compose.yml        # Multi-service orchestration
└── openapi.yaml              # API specification
```

## Technical Details

### Database

- **PostgreSQL 16** with full-text search (tsvector + GIN indexes)
- **Fuzzy search** via `pg_trgm` extension for typo tolerance
- **NUMERIC(12,2)** for currency precision
- **Triggers** for automatic search vector updates

### Search Implementation

The search combines multiple strategies ranked by relevance:
1. Full-text search with stemming (e.g., "pizzas" → "pizza")
2. Prefix matching (e.g., "piz" matches "pizza")
3. Trigram similarity for fuzzy matching (e.g., "piza" → "pizza")

### Transaction Safety

- Row-level locking (`FOR UPDATE`) prevents race conditions
- Idempotency keys prevent duplicate orders on retries
- Atomic balance updates within single transaction
