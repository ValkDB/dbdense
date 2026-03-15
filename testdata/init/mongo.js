// dbdense integration test seed data for MongoDB.
// This file runs automatically via docker-entrypoint-initdb.d.

db = db.getSiblingDB("dbdense_test");

// ---------------------------------------------------------------------------
// Deterministic PRNG (mulberry32) seeded with a fixed value.
// Produces repeatable pseudo-random numbers in [0, 1).
// ---------------------------------------------------------------------------
var _seed = 42;
function mulberry32() {
  _seed |= 0;
  _seed = (_seed + 0x6d2b79f5) | 0;
  var t = Math.imul(_seed ^ (_seed >>> 15), 1 | _seed);
  t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
  return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
}

// ---------------------------------------------------------------------------
// Shared name/category arrays for deterministic data generation.
// ---------------------------------------------------------------------------
var firstNames = [
  "Alice","Bob","Charlie","Diana","Eve","Frank","Grace","Hank",
  "Iris","Jack","Karen","Leo","Mia","Noah","Olivia","Paul",
  "Quinn","Rosa","Sam","Tina","Uma","Victor","Wendy","Xander",
  "Yara","Zane","Abby","Ben","Cora","Derek","Elena","Felix",
  "Gina","Hugo","Ivy","Joel","Kara","Liam","Mona","Nate",
  "Opal","Pete","Rita","Sean","Tara","Uri","Vera","Wade",
  "Xena","Yuri"
];

var lastNames = [
  "Smith","Jones","Brown","Davis","Miller","Wilson","Moore","Taylor",
  "Anderson","Thomas","Jackson","White","Harris","Martin","Thompson",
  "Garcia","Martinez","Robinson","Clark","Rodriguez","Lewis","Lee",
  "Walker","Hall","Allen","Young","King","Wright","Scott","Green",
  "Baker","Adams","Nelson","Hill","Ramirez","Campbell","Mitchell",
  "Roberts","Carter","Phillips","Evans","Turner","Torres","Parker",
  "Collins","Edwards","Stewart","Flores","Morris","Nguyen"
];

var productPrefixes = [
  "Ultra","Pro","Elite","Prime","Eco","Nano","Mega","Turbo",
  "Flex","Core","Max","Mini","Smart","Swift","Zen"
];

var productNouns = [
  "Widget","Gadget","Sensor","Module","Board","Drive","Cable",
  "Adapter","Switch","Relay","Valve","Pump","Filter","Motor",
  "Lens","Panel","Cell","Chip","Coil","Fuse","Plug","Bracket",
  "Sleeve","Tube","Rod","Gear","Shaft","Ring","Seal","Bolt",
  "Spring","Clamp","Joint"
];

var productTags = [
  ["hardware"],["electronics"],["industrial"],["automotive"],["medical"],
  ["aerospace"],["consumer"],["telecom"],["marine"],["energy"]
];

var statuses = ["pending", "confirmed", "shipped", "delivered", "cancelled"];
var roles = ["admin", "member", "viewer", "editor", "moderator"];
var paymentStatuses = ["pending", "authorized", "paid", "failed", "refunded"];
var paymentProviders = ["stripe", "adyen", "paypal", "braintree"];
var shipmentCarriers = ["ups", "fedex", "dhl", "usps", "ontrac"];
var shipmentStatuses = ["label_created", "in_transit", "delivered", "delayed", "lost"];
var regions = ["na-east", "na-west", "eu-central", "ap-south"];

var BATCH_SIZE = 5000;

// ---------------------------------------------------------------------------
// Users: 20,000 rows, ~5% soft-deleted
// ---------------------------------------------------------------------------
print("Seeding users...");
var userIds = [];
var userBatch = [];

for (var i = 1; i <= 20000; i++) {
  var firstName = firstNames[i % 50];
  var lastName = lastNames[((i * 7) + 13) % 50];
  var softDeleted = ((i * 31 + 17) % 20 === 0);
  var deletedAt = null;
  if (softDeleted) {
    // Deterministic date in 2024
    var dayOffset = (i * 97) % 365;
    var minOffset = (i * 53) % 1440;
    deletedAt = new Date(2024, 0, 1 + dayOffset, Math.floor(minOffset / 60), minOffset % 60, 0);
  }

  var userId = ObjectId();
  userIds.push(userId);

  userBatch.push({
    _id: userId,
    email: "user" + i + "@example.com",
    name: firstName + " " + lastName,
    role: roles[i % 5],
    deleted_at: deletedAt
  });

  if (userBatch.length >= BATCH_SIZE) {
    db.users.insertMany(userBatch);
    userBatch = [];
  }
}
if (userBatch.length > 0) {
  db.users.insertMany(userBatch);
  userBatch = [];
}
print("  Users: " + userIds.length + " inserted.");

db.users.createIndex({ email: 1 }, { unique: true, name: "idx_email" });

// ---------------------------------------------------------------------------
// Products: 500 distinct products, prices $1.00-$999.99
// ---------------------------------------------------------------------------
print("Seeding products...");
var productIds = [];
var productBatch = [];

for (var i = 1; i <= 500; i++) {
  var prefix = productPrefixes[i % 15];
  var noun = productNouns[((i * 3) % 33)];
  var num = String(i).padStart(3, "0");
  var price = Math.round(((i * 73 + 29) % 99900 + 100)) / 100.0;

  var productId = ObjectId();
  productIds.push(productId);

  productBatch.push({
    _id: productId,
    name: prefix + " " + noun + " " + num,
    price: price,
    tags: productTags[i % 10],
    attributes: {
      category: ["hardware", "electronics", "industrial", "consumer", "medical", "energy"][(i * 11 + 5) % 6],
      fragile: ((i * 19 + 3) % 7 === 0),
      weight_kg: Math.round((((i * 5 + 7) % 240 + 10) / 10.0) * 10) / 10,
      lifecycle: ["core", "premium", "legacy", "fast-moving", "slow-moving"][(i * 13 + 1) % 5]
    }
  });

  if (productBatch.length >= BATCH_SIZE) {
    db.products.insertMany(productBatch);
    productBatch = [];
  }
}
if (productBatch.length > 0) {
  db.products.insertMany(productBatch);
  productBatch = [];
}
print("  Products: " + productIds.length + " inserted.");

db.products.createIndex({ name: 1 }, { name: "idx_name" });

// ---------------------------------------------------------------------------
// Orders: 20,000 rows
// Status distribution: pending ~20%, confirmed ~25%, shipped ~25%,
//   delivered ~20%, cancelled ~10%.
// ---------------------------------------------------------------------------
print("Seeding orders...");
var orderIds = [];
var orderBatch = [];

for (var i = 1; i <= 20000; i++) {
  var userIdx = ((i * 37 + 11) % 20000);
  var mod100 = (i * 41 + 7) % 100;
  var statusIdx;
  if (mod100 < 20)      statusIdx = 0;  // pending    ~20%
  else if (mod100 < 45) statusIdx = 1;  // confirmed  ~25%
  else if (mod100 < 70) statusIdx = 2;  // shipped    ~25%
  else if (mod100 < 90) statusIdx = 3;  // delivered  ~20%
  else                  statusIdx = 4;  // cancelled  ~10%

  var total = Math.round(((i * 137 + 53) % 99900 + 100)) / 100.0;

  var orderId = ObjectId();
  orderIds.push(orderId);

  orderBatch.push({
    _id: orderId,
    user_id: userIds[userIdx],
    status: statuses[statusIdx],
    total: total,
    payload: {
      status: statuses[statusIdx],
      channel: ["web", "mobile", "partner", "api"][(i * 17 + 9) % 4],
      priority: ["normal", "high", "urgent"][(i * 23 + 7) % 3],
      contains_gift: ((i * 29 + 11) % 9 === 0),
      items: [
        {
          sku: "SKU-" + String(1 + ((i * 13 + 5) % 500)).padStart(4, "0"),
          qty: 1 + ((i * 11 + 3) % 5),
          price: Math.round(((i * 73 + 29) % 99900 + 100)) / 100.0
        }
      ],
      shipping: {
        carrier: shipmentCarriers[(i * 19 + 2) % 5],
        tracking_id: "TRK-" + i + "-" + ((i * 31) % 100000),
        shipped_at: new Date(2024, 0, 1 + ((i * 83) % 365), Math.floor(((i * 29) % 1440) / 60), ((i * 29) % 1440) % 60, 0)
      },
      flags: mod100 >= 90 ? ["manual_review"] : [],
      experiment: i % 2000 === 0 ? "ctx-pressure" : null
    }
  });

  if (orderBatch.length >= BATCH_SIZE) {
    db.orders.insertMany(orderBatch);
    orderBatch = [];
  }
}
if (orderBatch.length > 0) {
  db.orders.insertMany(orderBatch);
  orderBatch = [];
}
print("  Orders: " + orderIds.length + " inserted.");

db.orders.createIndex({ user_id: 1 }, { name: "idx_user_id" });
db.orders.createIndex({ status: 1 }, { name: "idx_status" });
db.orders.createIndex({ status: 1, total: 1 }, { name: "idx_status_total" });
db.orders.createIndex({ "payload.status": 1 }, { name: "idx_payload_status" });

// ---------------------------------------------------------------------------
// Payments: 20,000 rows (1 payment record per order)
// Includes nested gateway payload JSON.
// ---------------------------------------------------------------------------
print("Seeding payments...");
var paymentCount = 0;
var paymentBatch = [];

for (var i = 1; i <= 20000; i++) {
  var paymentMod = (i * 29 + 3) % 100;
  var paymentStatusIdx;
  if (paymentMod < 15)      paymentStatusIdx = 0;  // pending
  else if (paymentMod < 35) paymentStatusIdx = 1;  // authorized
  else if (paymentMod < 80) paymentStatusIdx = 2;  // paid
  else if (paymentMod < 95) paymentStatusIdx = 3;  // failed
  else                      paymentStatusIdx = 4;  // refunded

  var paidAt = null;
  if (paymentMod < 80) {
    var paidDayOffset = (i * 71) % 365;
    var paidMinOffset = (i * 37) % 1440;
    paidAt = new Date(2024, 0, 1 + paidDayOffset, Math.floor(paidMinOffset / 60), paidMinOffset % 60, 0);
  }

  paymentBatch.push({
    _id: ObjectId(),
    order_id: orderIds[i - 1],
    status: paymentStatuses[paymentStatusIdx],
    provider: paymentProviders[(i * 17 + 5) % 4],
    amount: Math.round(((i * 149 + 31) % 99900 + 100)) / 100.0,
    paid_at: paidAt,
    retry_count: paymentMod >= 80 ? (1 + ((i * 11) % 4)) : 0,
    gateway_payload: {
      currency: "USD",
      risk_score: (i * 13) % 100,
      attempt: 1 + ((i * 7) % 3),
      card: {
        brand: ["visa", "mastercard", "amex", "discover"][(i * 5 + 1) % 4],
        last4: String((i * 97) % 10000).padStart(4, "0")
      },
      flags: paymentMod >= 95 ? ["manual_review", "retry"] : (paymentMod >= 80 ? ["retry"] : [])
    }
  });
  paymentCount++;

  if (paymentBatch.length >= BATCH_SIZE) {
    db.payments.insertMany(paymentBatch);
    paymentBatch = [];
  }
}
if (paymentBatch.length > 0) {
  db.payments.insertMany(paymentBatch);
  paymentBatch = [];
}
print("  Payments: " + paymentCount + " inserted.");

db.payments.createIndex({ order_id: 1 }, { name: "idx_order_id" });
db.payments.createIndex({ status: 1 }, { name: "idx_status" });
db.payments.createIndex({ provider: 1, status: 1 }, { name: "idx_provider_status" });

// ---------------------------------------------------------------------------
// Shipments: 20,000 rows (1 shipment record per order)
// Includes nested tracking events JSON.
// ---------------------------------------------------------------------------
print("Seeding shipments...");
var shipmentCount = 0;
var shipmentBatch = [];

for (var i = 1; i <= 20000; i++) {
  var shipMod = (i * 47 + 9) % 100;
  var shipStatusIdx;
  if (shipMod < 10)      shipStatusIdx = 0;  // label_created
  else if (shipMod < 65) shipStatusIdx = 1;  // in_transit
  else if (shipMod < 90) shipStatusIdx = 2;  // delivered
  else if (shipMod < 97) shipStatusIdx = 3;  // delayed
  else                   shipStatusIdx = 4;  // lost

  var shipDayOffset = (i * 83) % 365;
  var shipMinOffset = (i * 29) % 1440;
  var shippedAt = new Date(2024, 0, 1 + shipDayOffset, Math.floor(shipMinOffset / 60), shipMinOffset % 60, 0);

  var deliveredAt = null;
  if (shipMod < 90) {
    var deliveredTotalMin = shipMinOffset + 120 + ((i * 17) % 720);
    deliveredAt = new Date(2024, 0, 1 + shipDayOffset, Math.floor(deliveredTotalMin / 60), deliveredTotalMin % 60, 0);
  }

  shipmentBatch.push({
    _id: ObjectId(),
    order_id: orderIds[i - 1],
    carrier: shipmentCarriers[(i * 19 + 2) % 5],
    status: shipmentStatuses[shipStatusIdx],
    region: regions[(i * 23 + 7) % 4],
    shipped_at: shippedAt,
    delivered_at: deliveredAt,
    tracking_events: [
      { state: "created", seq: 1 },
      { state: shipMod < 65 ? "in_transit" : (shipMod < 90 ? "delivered" : (shipMod < 97 ? "delayed" : "lost")), seq: 2 }
    ]
  });
  shipmentCount++;

  if (shipmentBatch.length >= BATCH_SIZE) {
    db.shipments.insertMany(shipmentBatch);
    shipmentBatch = [];
  }
}
if (shipmentBatch.length > 0) {
  db.shipments.insertMany(shipmentBatch);
  shipmentBatch = [];
}
print("  Shipments: " + shipmentCount + " inserted.");

db.shipments.createIndex({ order_id: 1 }, { name: "idx_order_id" });
db.shipments.createIndex({ status: 1 }, { name: "idx_status" });
db.shipments.createIndex({ carrier: 1, region: 1 }, { name: "idx_carrier_region" });

// ---------------------------------------------------------------------------
// Order items: ~60,000 rows (1-5 items per order, avg ~3)
// ---------------------------------------------------------------------------
print("Seeding order_items...");
var oiCount = 0;
var oiBatch = [];

for (var o = 1; o <= 20000; o++) {
  var numItems = 1 + ((o * 31 + 17) % 5);
  for (var item = 1; item <= numItems; item++) {
    var productIdx = ((o * 13 + item * 7) % 500);
    var qty = 1 + ((o * 11 + item * 3) % 5);

    oiBatch.push({
      order_id: orderIds[o - 1],
      product_id: productIds[productIdx],
      quantity: qty
    });
    oiCount++;

    if (oiBatch.length >= BATCH_SIZE) {
      db.order_items.insertMany(oiBatch);
      oiBatch = [];
    }
  }
}
if (oiBatch.length > 0) {
  db.order_items.insertMany(oiBatch);
  oiBatch = [];
}
print("  Order items: " + oiCount + " inserted.");

db.order_items.createIndex({ order_id: 1 }, { name: "idx_order_id" });

// ---------------------------------------------------------------------------
// Audit events: 40,000 rows (noise/trap collection)
// ---------------------------------------------------------------------------
print("Seeding audit_events...");
var auditCount = 0;
var auditBatch = [];

for (var i = 1; i <= 40000; i++) {
  var auditDayOffset = (i * 61) % 365;
  var auditMinOffset = (i * 47) % 1440;

  auditBatch.push({
    _id: ObjectId(),
    event_type: ["login", "password_reset", "token_refresh", "order_view", "shipment_view", "payment_retry", "report_export", "flag_toggle"][(i * 13 + 5) % 8],
    actor_id: userIds[(i * 43 + 17) % 20000],
    occurred_at: new Date(2024, 0, 1 + auditDayOffset, Math.floor(auditMinOffset / 60), auditMinOffset % 60, 0),
    payload: {
      source: ["api", "worker", "admin", "cron"][(i * 7 + 1) % 4],
      ip: "10." + ((i * 3) % 256) + "." + ((i * 5) % 256) + "." + ((i * 7) % 256),
      meta: {
        session: "sess-" + i,
        severity: ["info", "warn", "error"][(i * 11 + 2) % 3]
      }
    }
  });
  auditCount++;

  if (auditBatch.length >= BATCH_SIZE) {
    db.audit_events.insertMany(auditBatch);
    auditBatch = [];
  }
}
if (auditBatch.length > 0) {
  db.audit_events.insertMany(auditBatch);
  auditBatch = [];
}
print("  Audit events: " + auditCount + " inserted.");

db.audit_events.createIndex({ event_type: 1 }, { name: "idx_event_type" });
db.audit_events.createIndex({ actor_id: 1 }, { name: "idx_actor_id" });

// ---------------------------------------------------------------------------
// Internal metrics: 40,000 rows (noise/trap collection)
// ---------------------------------------------------------------------------
print("Seeding internal_metrics...");
var metricCount = 0;
var metricBatch = [];

for (var i = 1; i <= 40000; i++) {
  var metricDayOffset = (i * 41) % 365;
  var metricMinOffset = (i * 19) % 1440;

  metricBatch.push({
    _id: ObjectId(),
    metric_name: ["cpu_usage", "queue_depth", "api_latency_ms", "disk_iops", "cache_hit_ratio"][(i * 7 + 5) % 5],
    metric_value: Math.round(((i * 59 + 13) % 100000)) / 100.0,
    recorded_at: new Date(2024, 0, 1 + metricDayOffset, Math.floor(metricMinOffset / 60), metricMinOffset % 60, 0),
    tags: {
      host: "node-" + (1 + ((i * 3) % 32)),
      service: ["api", "worker", "scheduler", "ingest"][(i * 17 + 4) % 4],
      env: ["prod", "staging"][(i * 31) % 2]
    }
  });
  metricCount++;

  if (metricBatch.length >= BATCH_SIZE) {
    db.internal_metrics.insertMany(metricBatch);
    metricBatch = [];
  }
}
if (metricBatch.length > 0) {
  db.internal_metrics.insertMany(metricBatch);
  metricBatch = [];
}
print("  Internal metrics: " + metricCount + " inserted.");

db.internal_metrics.createIndex({ metric_name: 1 }, { name: "idx_metric_name" });
db.internal_metrics.createIndex({ recorded_at: 1 }, { name: "idx_recorded_at" });

print("Seed complete.");
