-- dbdense integration test seed data for PostgreSQL.
-- This file runs automatically via docker-entrypoint-initdb.d.

-- Schema: public (default)
CREATE TABLE users (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email      TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    deleted_at TIMESTAMPTZ
);
COMMENT ON TABLE users IS 'Core identity table.';
COMMENT ON COLUMN users.email IS 'Login email address.';
COMMENT ON COLUMN users.deleted_at IS 'Soft delete timestamp.';

CREATE TABLE products (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL,
    price      NUMERIC(10, 2) NOT NULL,
    attributes JSONB NOT NULL DEFAULT '{}'::jsonb
);
COMMENT ON TABLE products IS 'Product catalog.';

CREATE TABLE orders (
    id      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    status  TEXT NOT NULL DEFAULT 'pending',
    total   NUMERIC(10, 2) NOT NULL DEFAULT 0,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);
COMMENT ON TABLE orders IS 'Customer orders.';
COMMENT ON COLUMN orders.status IS 'Order lifecycle status.';
COMMENT ON COLUMN orders.payload IS 'JSONB. Structure: {status: string (pending|confirmed|shipped|delivered|cancelled), items: [{sku: string, qty: int, price: numeric}], shipping: {carrier: string, tracking_id: string, shipped_at: timestamptz}}. Query with payload->>''status'', payload->''items'', payload->''shipping''->>''carrier''.';

CREATE TABLE order_items (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id   UUID NOT NULL REFERENCES orders(id),
    product_id UUID NOT NULL REFERENCES products(id),
    quantity   INT NOT NULL DEFAULT 1
);
COMMENT ON TABLE order_items IS 'Line items in an order.';

CREATE TABLE payments (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id        UUID NOT NULL REFERENCES orders(id),
    status          TEXT NOT NULL,
    provider        TEXT NOT NULL,
    amount          NUMERIC(10, 2) NOT NULL,
    paid_at         TIMESTAMPTZ,
    retry_count     INT NOT NULL DEFAULT 0,
    gateway_payload JSONB NOT NULL DEFAULT '{}'::jsonb
);
COMMENT ON TABLE payments IS 'Payment attempts and settlement state.';
COMMENT ON COLUMN payments.gateway_payload IS 'Provider-specific payment metadata.';

CREATE TABLE shipments (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id        UUID NOT NULL REFERENCES orders(id),
    carrier         TEXT NOT NULL,
    status          TEXT NOT NULL,
    region          TEXT NOT NULL,
    shipped_at      TIMESTAMPTZ,
    delivered_at    TIMESTAMPTZ,
    tracking_events JSONB NOT NULL DEFAULT '[]'::jsonb
);
COMMENT ON TABLE shipments IS 'Shipment and delivery tracking records.';
COMMENT ON COLUMN shipments.tracking_events IS 'Event stream for shipment lifecycle.';

CREATE TABLE audit_events (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type  TEXT NOT NULL,
    actor_id    UUID NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    payload     JSONB NOT NULL DEFAULT '{}'::jsonb
);
COMMENT ON TABLE audit_events IS 'Operational audit trail (noise/trap table).';

CREATE TABLE internal_metrics (
    id          BIGSERIAL PRIMARY KEY,
    metric_name TEXT NOT NULL,
    metric_value NUMERIC(12, 2) NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL,
    tags        JSONB NOT NULL DEFAULT '{}'::jsonb
);
COMMENT ON TABLE internal_metrics IS 'Internal observability metrics (noise/trap table).';

CREATE INDEX idx_orders_user_id ON orders(user_id);
CREATE INDEX idx_orders_status ON orders(status);
CREATE INDEX idx_orders_status_total ON orders(status, total);
CREATE INDEX idx_orders_payload ON orders USING gin (payload);
CREATE INDEX idx_orders_payload_status ON orders ((payload->>'status'));
CREATE INDEX idx_order_items_order_id ON order_items(order_id);
CREATE INDEX idx_payments_order_id ON payments(order_id);
CREATE INDEX idx_payments_status ON payments(status);
CREATE INDEX idx_payments_provider_status ON payments(provider, status);
CREATE INDEX idx_shipments_order_id ON shipments(order_id);
CREATE INDEX idx_shipments_status ON shipments(status);
CREATE INDEX idx_shipments_carrier_region ON shipments(carrier, region);
CREATE INDEX idx_audit_events_event_type ON audit_events(event_type);
CREATE INDEX idx_audit_events_actor_id ON audit_events(actor_id);
CREATE INDEX idx_internal_metrics_metric_name ON internal_metrics(metric_name);
CREATE INDEX idx_internal_metrics_recorded_at ON internal_metrics(recorded_at);

-- Schema: auth (multi-schema test)
CREATE SCHEMA auth;

CREATE TABLE auth.sessions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL,
    token      TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);
COMMENT ON TABLE auth.sessions IS 'Active user sessions.';

CREATE INDEX idx_sessions_user_id ON auth.sessions(user_id);
CREATE INDEX idx_sessions_token ON auth.sessions(token);

-- =============================================================================
-- Seed data: 20,000+ rows per table using deterministic procedural generation.
-- Uses md5-based deterministic values for full reproducibility.
-- =============================================================================

-- ---------------------------------------------------------------------------
-- Users: 20,000 rows, ~5% soft-deleted
-- Deterministic UUIDs via md5 so downstream tables can reference by index.
-- ~2,500 distinct full names (50 first x 50 last with decorrelated indices).
-- ---------------------------------------------------------------------------
INSERT INTO users (id, email, name, deleted_at)
SELECT
    md5(format('user-%s', i))::uuid AS id,
    format('user%s@example.com', i) AS email,
    (ARRAY[
        'Alice','Bob','Charlie','Diana','Eve','Frank','Grace','Hank',
        'Iris','Jack','Karen','Leo','Mia','Noah','Olivia','Paul',
        'Quinn','Rosa','Sam','Tina','Uma','Victor','Wendy','Xander',
        'Yara','Zane','Abby','Ben','Cora','Derek','Elena','Felix',
        'Gina','Hugo','Ivy','Joel','Kara','Liam','Mona','Nate',
        'Opal','Pete','Rita','Sean','Tara','Uri','Vera','Wade',
        'Xena','Yuri'
    ])[1 + (i % 50)]
    || ' '
    || (ARRAY[
        'Smith','Jones','Brown','Davis','Miller','Wilson','Moore','Taylor',
        'Anderson','Thomas','Jackson','White','Harris','Martin','Thompson',
        'Garcia','Martinez','Robinson','Clark','Rodriguez','Lewis','Lee',
        'Walker','Hall','Allen','Young','King','Wright','Scott','Green',
        'Baker','Adams','Nelson','Hill','Ramirez','Campbell','Mitchell',
        'Roberts','Carter','Phillips','Evans','Turner','Torres','Parker',
        'Collins','Edwards','Stewart','Flores','Morris','Nguyen'
    ])[1 + ((i * 7 + 13) % 50)] AS name,
    CASE
        WHEN (i * 31 + 17) % 20 = 0
        THEN TIMESTAMP '2024-01-01 00:00:00+00'
             + (((i * 97) % 365) || ' days')::interval
             + (((i * 53) % 1440) || ' minutes')::interval
        ELSE NULL
    END AS deleted_at
FROM generate_series(1, 20000) AS s(i);

-- ---------------------------------------------------------------------------
-- Products: 500 distinct products, prices $1.00-$999.99
-- Each product has a unique name built from prefix + noun + number.
-- ---------------------------------------------------------------------------
INSERT INTO products (id, name, price, attributes)
SELECT
    md5(format('product-%s', i))::uuid AS id,
    (ARRAY[
        'Ultra','Pro','Elite','Prime','Eco','Nano','Mega','Turbo',
        'Flex','Core','Max','Mini','Smart','Swift','Zen'
    ])[1 + (i % 15)]
    || ' '
    || (ARRAY[
        'Widget','Gadget','Sensor','Module','Board','Drive','Cable',
        'Adapter','Switch','Relay','Valve','Pump','Filter','Motor',
        'Lens','Panel','Cell','Chip','Coil','Fuse','Plug','Bracket',
        'Sleeve','Tube','Rod','Gear','Shaft','Ring','Seal','Bolt',
        'Spring','Clamp','Joint'
    ])[1 + ((i * 3) % 33)]
    || ' '
    || lpad(i::text, 3, '0') AS name,
    round((((i * 73 + 29) % 99900 + 100)::numeric / 100.0), 2) AS price,
    jsonb_build_object(
        'category', (ARRAY['hardware','electronics','industrial','consumer','medical','energy'])[1 + ((i * 11 + 5) % 6)],
        'fragile', ((i * 19 + 3) % 7 = 0),
        'weight_kg', round((((i * 5 + 7) % 240 + 10)::numeric / 10.0), 1),
        'tags', jsonb_build_array((ARRAY['core','premium','legacy','fast-moving','slow-moving'])[1 + ((i * 13 + 1) % 5)])
    ) AS attributes
FROM generate_series(1, 500) AS s(i);

-- ---------------------------------------------------------------------------
-- Orders: 20,000 rows
-- Status distribution: pending ~20%, confirmed ~25%, shipped ~25%,
--   delivered ~20%, cancelled ~10%.
-- Each order references a deterministic user via md5-based UUID.
-- Totals: $1.00-$999.99 with ~20-24% distinct values.
-- ---------------------------------------------------------------------------
INSERT INTO orders (id, user_id, status, total, payload)
SELECT
    md5(format('order-%s', i))::uuid AS id,
    md5(format('user-%s', 1 + ((i * 37 + 11) % 20000)))::uuid AS user_id,
    (ARRAY['pending','confirmed','shipped','delivered','cancelled'])[
        1 + CASE
            WHEN (i * 41 + 7) % 100 < 20 THEN 0
            WHEN (i * 41 + 7) % 100 < 45 THEN 1
            WHEN (i * 41 + 7) % 100 < 70 THEN 2
            WHEN (i * 41 + 7) % 100 < 90 THEN 3
            ELSE 4
        END
    ] AS status,
    round((((i * 137 + 53) % 99900 + 100)::numeric / 100.0), 2) AS total,
    jsonb_build_object(
        'status', (ARRAY['pending','confirmed','shipped','delivered','cancelled'])[
            1 + CASE
                WHEN (i * 41 + 7) % 100 < 20 THEN 0
                WHEN (i * 41 + 7) % 100 < 45 THEN 1
                WHEN (i * 41 + 7) % 100 < 70 THEN 2
                WHEN (i * 41 + 7) % 100 < 90 THEN 3
                ELSE 4
            END
        ],
        'channel', (ARRAY['web','mobile','partner','api'])[1 + ((i * 17 + 9) % 4)],
        'priority', (ARRAY['normal','high','urgent'])[1 + ((i * 23 + 7) % 3)],
        'contains_gift', ((i * 29 + 11) % 9 = 0),
        'items', jsonb_build_array(
            jsonb_build_object(
                'sku', format('SKU-%s', lpad((1 + ((i * 13 + 5) % 500))::text, 4, '0')),
                'qty', 1 + ((i * 11 + 3) % 5),
                'price', round((((i * 73 + 29) % 99900 + 100)::numeric / 100.0), 2)
            )
        ),
        'shipping', jsonb_build_object(
            'carrier', (ARRAY['ups','fedex','dhl','usps','ontrac'])[1 + ((i * 19 + 2) % 5)],
            'tracking_id', format('TRK-%s-%s', i, (i * 31) % 100000),
            'shipped_at', to_char(
                TIMESTAMP '2024-01-01 00:00:00+00'
                    + (((i * 83) % 365) || ' days')::interval
                    + (((i * 29) % 1440) || ' minutes')::interval,
                'YYYY-MM-DD"T"HH24:MI:SS"Z"'
            )
        ),
        'flags', CASE
            WHEN (i * 41 + 7) % 100 >= 90 THEN jsonb_build_array('manual_review')
            ELSE '[]'::jsonb
        END,
        'experiment', CASE
            WHEN i % 2000 = 0 THEN 'ctx-pressure'
            ELSE NULL
        END
    ) AS payload
FROM generate_series(1, 20000) AS s(i);

-- ---------------------------------------------------------------------------
-- Payments: 20,000 rows (1 payment record per order)
-- Status distribution: pending/authorized/paid/failed/refunded.
-- Includes nested JSONB payload for gateway metadata.
-- ---------------------------------------------------------------------------
INSERT INTO payments (id, order_id, status, provider, amount, paid_at, retry_count, gateway_payload)
SELECT
    md5(format('payment-%s', i))::uuid AS id,
    md5(format('order-%s', i))::uuid AS order_id,
    (ARRAY['pending','authorized','paid','failed','refunded'])[
        1 + CASE
            WHEN (i * 29 + 3) % 100 < 15 THEN 0
            WHEN (i * 29 + 3) % 100 < 35 THEN 1
            WHEN (i * 29 + 3) % 100 < 80 THEN 2
            WHEN (i * 29 + 3) % 100 < 95 THEN 3
            ELSE 4
        END
    ] AS status,
    (ARRAY['stripe','adyen','paypal','braintree'])[1 + ((i * 17 + 5) % 4)] AS provider,
    round((((i * 149 + 31) % 99900 + 100)::numeric / 100.0), 2) AS amount,
    CASE
        WHEN (i * 29 + 3) % 100 < 80
        THEN TIMESTAMP '2024-01-01 00:00:00+00'
             + (((i * 71) % 365) || ' days')::interval
             + (((i * 37) % 1440) || ' minutes')::interval
        ELSE NULL
    END AS paid_at,
    CASE
        WHEN (i * 29 + 3) % 100 >= 80 THEN 1 + ((i * 11) % 4)
        ELSE 0
    END AS retry_count,
    jsonb_build_object(
        'currency', 'USD',
        'risk_score', (i * 13) % 100,
        'attempt', 1 + ((i * 7) % 3),
        'card', jsonb_build_object(
            'brand', (ARRAY['visa','mastercard','amex','discover'])[1 + ((i * 5 + 1) % 4)],
            'last4', lpad(((i * 97) % 10000)::text, 4, '0')
        ),
        'flags', CASE
            WHEN (i * 29 + 3) % 100 >= 95 THEN jsonb_build_array('manual_review', 'retry')
            WHEN (i * 29 + 3) % 100 >= 80 THEN jsonb_build_array('retry')
            ELSE '[]'::jsonb
        END
    ) AS gateway_payload
FROM generate_series(1, 20000) AS s(i);

-- ---------------------------------------------------------------------------
-- Shipments: 20,000 rows (1 shipment record per order)
-- Includes tracking events JSONB with nested objects.
-- ---------------------------------------------------------------------------
INSERT INTO shipments (id, order_id, carrier, status, region, shipped_at, delivered_at, tracking_events)
SELECT
    md5(format('shipment-%s', i))::uuid AS id,
    md5(format('order-%s', i))::uuid AS order_id,
    (ARRAY['ups','fedex','dhl','usps','ontrac'])[1 + ((i * 19 + 2) % 5)] AS carrier,
    (ARRAY['label_created','in_transit','delivered','delayed','lost'])[
        1 + CASE
            WHEN (i * 47 + 9) % 100 < 10 THEN 0
            WHEN (i * 47 + 9) % 100 < 65 THEN 1
            WHEN (i * 47 + 9) % 100 < 90 THEN 2
            WHEN (i * 47 + 9) % 100 < 97 THEN 3
            ELSE 4
        END
    ] AS status,
    (ARRAY['na-east','na-west','eu-central','ap-south'])[1 + ((i * 23 + 7) % 4)] AS region,
    TIMESTAMP '2024-01-01 00:00:00+00'
        + (((i * 83) % 365) || ' days')::interval
        + (((i * 29) % 1440) || ' minutes')::interval AS shipped_at,
    CASE
        WHEN (i * 47 + 9) % 100 < 90
        THEN TIMESTAMP '2024-01-01 00:00:00+00'
             + (((i * 83) % 365) || ' days')::interval
             + (((i * 29) % 1440 + 120 + ((i * 17) % 720)) || ' minutes')::interval
        ELSE NULL
    END AS delivered_at,
    jsonb_build_array(
        jsonb_build_object('state', 'created', 'seq', 1),
        jsonb_build_object(
            'state', CASE
                WHEN (i * 47 + 9) % 100 < 65 THEN 'in_transit'
                WHEN (i * 47 + 9) % 100 < 90 THEN 'delivered'
                WHEN (i * 47 + 9) % 100 < 97 THEN 'delayed'
                ELSE 'lost'
            END,
            'seq', 2
        )
    ) AS tracking_events
FROM generate_series(1, 20000) AS s(i);

-- ---------------------------------------------------------------------------
-- Order items: ~60,000 rows (1-5 items per order, avg ~3)
-- Each item references a deterministic order and one of the 500 products.
-- Quantities range from 1-5.
-- ---------------------------------------------------------------------------
INSERT INTO order_items (id, order_id, product_id, quantity)
SELECT
    md5(format('oi-%s-%s', o, item_num))::uuid AS id,
    md5(format('order-%s', o))::uuid AS order_id,
    md5(format('product-%s', 1 + ((o * 13 + item_num * 7) % 500)))::uuid AS product_id,
    1 + ((o * 11 + item_num * 3) % 5) AS quantity
FROM generate_series(1, 20000) AS o,
     generate_series(1, 5) AS item_num
WHERE item_num <= 1 + ((o * 31 + 17) % 5);

-- ---------------------------------------------------------------------------
-- Audit events: 40,000 rows (noise/trap workload)
-- ---------------------------------------------------------------------------
INSERT INTO audit_events (id, event_type, actor_id, occurred_at, payload)
SELECT
    md5(format('audit-%s', i))::uuid AS id,
    (ARRAY[
        'login','password_reset','token_refresh','order_view',
        'shipment_view','payment_retry','report_export','flag_toggle'
    ])[1 + ((i * 13 + 5) % 8)] AS event_type,
    md5(format('user-%s', 1 + ((i * 43 + 17) % 20000)))::uuid AS actor_id,
    TIMESTAMP '2024-01-01 00:00:00+00'
        + (((i * 61) % 365) || ' days')::interval
        + (((i * 47) % 1440) || ' minutes')::interval AS occurred_at,
    jsonb_build_object(
        'source', (ARRAY['api','worker','admin','cron'])[1 + ((i * 7 + 1) % 4)],
        'ip', format('10.%s.%s.%s', (i * 3) % 256, (i * 5) % 256, (i * 7) % 256),
        'meta', jsonb_build_object(
            'session', md5(format('sess-%s', i)),
            'severity', (ARRAY['info','warn','error'])[1 + ((i * 11 + 2) % 3)]
        )
    ) AS payload
FROM generate_series(1, 40000) AS s(i);

-- ---------------------------------------------------------------------------
-- Internal metrics: 40,000 rows (noise/trap workload)
-- ---------------------------------------------------------------------------
INSERT INTO internal_metrics (metric_name, metric_value, recorded_at, tags)
SELECT
    (ARRAY['cpu_usage','queue_depth','api_latency_ms','disk_iops','cache_hit_ratio'])[
        1 + ((i * 7 + 5) % 5)
    ] AS metric_name,
    round((((i * 59 + 13) % 100000)::numeric / 100.0), 2) AS metric_value,
    TIMESTAMP '2024-01-01 00:00:00+00'
        + (((i * 41) % 365) || ' days')::interval
        + (((i * 19) % 1440) || ' minutes')::interval AS recorded_at,
    jsonb_build_object(
        'host', format('node-%s', 1 + ((i * 3) % 32)),
        'service', (ARRAY['api','worker','scheduler','ingest'])[1 + ((i * 17 + 4) % 4)],
        'env', (ARRAY['prod','staging'])[1 + ((i * 31) % 2)]
    ) AS tags
FROM generate_series(1, 40000) AS s(i);

-- ---------------------------------------------------------------------------
-- Auth sessions: 20,000 rows
-- ~60% active (future expiry), ~40% expired (past expiry).
-- Tokens are deterministic 64-char hex strings from two md5 hashes.
-- ---------------------------------------------------------------------------
INSERT INTO auth.sessions (id, user_id, token, expires_at)
SELECT
    md5(format('session-%s', i))::uuid AS id,
    md5(format('user-%s', 1 + ((i * 23 + 5) % 20000)))::uuid AS user_id,
    md5(format('token-%s-%s', i, i * 97))
        || md5(format('salt-%s', i * 53)) AS token,
    CASE
        WHEN (i * 43 + 19) % 5 < 3
        THEN NOW() + (((i * 67) % 720 + 1) || ' hours')::interval
        ELSE NOW() - (((i * 89) % 720 + 1) || ' hours')::interval
    END AS expires_at
FROM generate_series(1, 20000) AS s(i);
