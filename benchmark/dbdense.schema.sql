-- dbdense schema context
CREATE TABLE audit_events ( -- Operational audit trail (noise/trap table).
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  event_type text NOT NULL,
  actor_id uuid NOT NULL,
  occurred_at timestamp with time zone NOT NULL,
  payload jsonb NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX idx_audit_events_actor_id ON audit_events (actor_id);
CREATE INDEX idx_audit_events_event_type ON audit_events (event_type);

CREATE TABLE internal_metrics ( -- Internal observability metrics (noise/trap table).
  id bigint PRIMARY KEY DEFAULT nextval('internal_metrics_id_seq'::regclass),
  metric_name text NOT NULL,
  metric_value numeric(12,2) NOT NULL,
  recorded_at timestamp with time zone NOT NULL,
  tags jsonb NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX idx_internal_metrics_metric_name ON internal_metrics (metric_name);
CREATE INDEX idx_internal_metrics_recorded_at ON internal_metrics (recorded_at);

CREATE TABLE order_items ( -- Line items in an order.
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id uuid NOT NULL,
  product_id uuid NOT NULL,
  quantity integer NOT NULL DEFAULT 1
);
CREATE INDEX idx_order_items_order_id ON order_items (order_id);

CREATE TABLE orders ( -- Customer orders.
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL,
  "status" text NOT NULL DEFAULT 'pending'::text, -- Order lifecycle status.
  total numeric(10,2) NOT NULL DEFAULT 0,
  payload jsonb NOT NULL DEFAULT '{}'::jsonb -- JSONB. Structure: {status: string (pending|confirmed|shipped|delivered|cancelled), items: [{sku: string, qty: int, price: numeric}], shipping: {carrier: string, tracking_id: string, shipped_at: timestamptz}}. Query with payload->>'status', payload->'items', payload->'shipping'->>'carrier'.
);
CREATE INDEX idx_orders_payload ON orders (payload);
CREATE INDEX idx_orders_status ON orders ("status");
CREATE INDEX idx_orders_status_total ON orders ("status", total);
CREATE INDEX idx_orders_user_id ON orders (user_id);

CREATE TABLE payments ( -- Payment attempts and settlement state.
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id uuid NOT NULL,
  "status" text NOT NULL,
  provider text NOT NULL,
  amount numeric(10,2) NOT NULL,
  paid_at timestamp with time zone,
  retry_count integer NOT NULL DEFAULT 0,
  gateway_payload jsonb NOT NULL DEFAULT '{}'::jsonb -- Provider-specific payment metadata.
);
CREATE INDEX idx_payments_order_id ON payments (order_id);
CREATE INDEX idx_payments_provider_status ON payments (provider, "status");
CREATE INDEX idx_payments_status ON payments ("status");

CREATE TABLE products ( -- Product catalog.
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  "name" text NOT NULL,
  price numeric(10,2) NOT NULL,
  attributes jsonb NOT NULL DEFAULT '{}'::jsonb
);

CREATE TABLE shipments ( -- Shipment and delivery tracking records.
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id uuid NOT NULL,
  carrier text NOT NULL,
  "status" text NOT NULL,
  region text NOT NULL,
  shipped_at timestamp with time zone,
  delivered_at timestamp with time zone,
  tracking_events jsonb NOT NULL DEFAULT '[]'::jsonb -- Event stream for shipment lifecycle.
);
CREATE INDEX idx_shipments_carrier_region ON shipments (carrier, region);
CREATE INDEX idx_shipments_order_id ON shipments (order_id);
CREATE INDEX idx_shipments_status ON shipments ("status");

CREATE TABLE users ( -- Core identity table.
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  email text NOT NULL, -- Login email address.
  "name" text NOT NULL,
  deleted_at timestamp with time zone -- Soft delete timestamp.
);
ALTER TABLE users ADD UNIQUE (email);

-- Foreign keys
ALTER TABLE order_items ADD FOREIGN KEY (order_id) REFERENCES orders (id);
ALTER TABLE order_items ADD FOREIGN KEY (product_id) REFERENCES products (id);
ALTER TABLE orders ADD FOREIGN KEY (user_id) REFERENCES users (id);
ALTER TABLE payments ADD FOREIGN KEY (order_id) REFERENCES orders (id);
ALTER TABLE shipments ADD FOREIGN KEY (order_id) REFERENCES orders (id);
