# ETL Data Loader

Python script `etl_load_data.py` loads the provided JSON datasets into PostgreSQL. It performs basic validation, normalizes a few edge cases, and reports skipped rows so you can check data quality after each run.

## Data sources
- `restaurant_with_menu.json`: restaurant metadata, opening hours string, and menu entries.
- `users_with_purchase_history.json`: user profile data plus purchase history (used to synthesize orders and order items).
- Override input files with `RESTAURANTS_JSON` and `USERS_JSON` env vars.

## Targets and flow
1) **DB connection**: waits for Postgres readiness with exponential backoff, then connects using `POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`.
2) **Restaurants + menu**: inserts/upserts restaurants and menu items. Missing `restaurantName` rows are skipped; malformed cash/price values are logged. Blank dish names are renamed to `[Unnamed]` and marked inactive.
3) **Opening hours**: parses the free-form `openingHours` string into day/time slots. Handles ranges (`Mon - Weds`), multiple groups, and overnight spans by splitting into multiple records.
4) **Users**: bulk upsert of user id, name, and cash balance; rows with missing names or invalid balances are skipped with a warning.
5) **Orders from history**: for each purchase history entry, creates an order and order item. Missing restaurant names or missing restaurant records are skipped; missing dish names become `[Unnamed]`; malformed amounts are skipped; timestamps are parsed (falls back to `now()` on failure).
6) All inserts are committed in phases per section; failures are summarized (first 10 shown) so you can spot data issues without aborting the whole run.

## Running locally
```bash
cd ETL
python3 -m venv .venv && source .venv/bin/activate
pip install -r requirements.txt  # psycopg2

POSTGRES_HOST=localhost POSTGRES_PORT=5432 POSTGRES_USER=fooduser \
POSTGRES_PASSWORD=foodpass POSTGRES_DB=fooddb \
python3 etl_load_data.py
```

The script exits on connection failure, but continues past individual bad rows while logging counts of skips/failures for restaurants, menu items, users, opening hours, and purchase history.

## Notes and expectations
- Assumes schema from `db/init.sql` exists (tables: restaurants, menu_items, restaurant_opening_hours, users, orders, order_items).
- Decimal parsing protects against float rounding when loading currency values.
- Overnight opening hours are split into two days; multi-day ranges also split into per-day rows.
- Order creation uses total amount as both unit and line amounts with quantity fixed at 1 (matches the input dataset shape).

