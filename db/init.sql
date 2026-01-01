-- db/init.sql

-- Enable trigram extension for fuzzy search
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE users (
    id           BIGINT PRIMARY KEY,
    name         TEXT NOT NULL,
    cash_balance NUMERIC(12,2) NOT NULL,
    created_at   TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE restaurants (
    id           BIGSERIAL PRIMARY KEY,
    name         TEXT NOT NULL UNIQUE,
    cash_balance NUMERIC(12,2) NOT NULL,
    timezone     TEXT NOT NULL DEFAULT 'UTC',
    created_at   TIMESTAMPTZ DEFAULT now(),
    name_search  TSVECTOR  -- Full-text search vector for restaurant names
);

CREATE TABLE menu_items (
    id            BIGSERIAL PRIMARY KEY,
    restaurant_id BIGINT NOT NULL REFERENCES restaurants(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    price         NUMERIC(10,2) NOT NULL,
    is_active     BOOLEAN NOT NULL DEFAULT TRUE,
    name_search   TSVECTOR  -- Full-text search vector for dish names
);

CREATE INDEX idx_menu_items_restaurant ON menu_items (restaurant_id);
CREATE INDEX idx_menu_items_price ON menu_items (price);

CREATE TABLE restaurant_opening_hours (
    id            BIGSERIAL PRIMARY KEY,
    restaurant_id BIGINT NOT NULL REFERENCES restaurants(id) ON DELETE CASCADE,
    day_of_week   SMALLINT NOT NULL,   -- 0=Sunday ... 6=Saturday
    open_time     TIME NOT NULL,
    close_time    TIME NOT NULL
);

CREATE INDEX idx_opening_hours_rest_day
    ON restaurant_opening_hours (restaurant_id, day_of_week);

CREATE TABLE orders (
    id            BIGSERIAL PRIMARY KEY,
    user_id       BIGINT NOT NULL REFERENCES users(id),
    restaurant_id BIGINT NOT NULL REFERENCES restaurants(id),
    total_amount  NUMERIC(12,2) NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_orders_user ON orders (user_id);
CREATE INDEX idx_orders_restaurant ON orders (restaurant_id);
CREATE INDEX idx_orders_created_at ON orders (created_at);

CREATE TABLE order_items (
    id           BIGSERIAL PRIMARY KEY,
    order_id     BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    menu_item_id BIGINT REFERENCES menu_items(id),
    dish_name    TEXT NOT NULL,
    unit_price   NUMERIC(10,2) NOT NULL,
    quantity     INT NOT NULL DEFAULT 1,
    line_amount  NUMERIC(12,2) NOT NULL
);

CREATE INDEX idx_order_items_order ON order_items (order_id);

-- Idempotency keys for purchase requests to prevent duplicate orders
CREATE TABLE purchase_idempotency_keys (
    key         TEXT PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id),
    response    JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_purchase_idempotency_keys_created ON purchase_idempotency_keys (created_at);
CREATE INDEX idx_purchase_idempotency_keys_user ON purchase_idempotency_keys (user_id);

-- Full-text search indexes (GIN indexes for fast text search)
CREATE INDEX idx_restaurants_name_search ON restaurants USING GIN (name_search);
CREATE INDEX idx_menu_items_name_search ON menu_items USING GIN (name_search);

-- Trigram indexes for fuzzy/typo-tolerant search
CREATE INDEX idx_restaurants_name_trgm ON restaurants USING GIN (name gin_trgm_ops);
CREATE INDEX idx_menu_items_name_trgm ON menu_items USING GIN (name gin_trgm_ops);

-- Triggers to automatically update tsvector columns when name changes
CREATE OR REPLACE FUNCTION update_restaurant_search_vector()
RETURNS TRIGGER AS $$
BEGIN
    NEW.name_search := to_tsvector('english', COALESCE(NEW.name, ''));
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_restaurant_search_vector
    BEFORE INSERT OR UPDATE OF name ON restaurants
    FOR EACH ROW
    EXECUTE FUNCTION update_restaurant_search_vector();

CREATE OR REPLACE FUNCTION update_menu_item_search_vector()
RETURNS TRIGGER AS $$
BEGIN
    NEW.name_search := to_tsvector('english', COALESCE(NEW.name, ''));
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_menu_item_search_vector
    BEFORE INSERT OR UPDATE OF name ON menu_items
    FOR EACH ROW
    EXECUTE FUNCTION update_menu_item_search_vector();
