load("@babelsuite/mongodb", "mongodb", "create_collection", "create_index", "insert_documents", "run_script")
load("@babelsuite/runtime", "service", "test")

DATABASES = ["catalog", "orders", "analytics"]

COLLECTIONS = {
    "catalog":   ["products", "categories", "reviews"],
    "orders":    ["orders", "line_items", "shipments"],
    "analytics": ["events", "sessions", "metrics"],
}

INDEXES = {
    "catalog.products":     [{"sku": 1},       {"category_id": 1, "price": -1}],
    "orders.orders":        [{"customer_id": 1}, {"status": 1, "created_at": -1}],
    "analytics.events":     [{"session_id": 1}, {"event_type": 1, "ts": -1}],
}

SEED_PRODUCTS = [
    {"sku": "SKU-001", "name": "Widget A", "price": 9.99,  "category_id": "cat-1", "active": True},
    {"sku": "SKU-002", "name": "Widget B", "price": 19.99, "category_id": "cat-1", "active": True},
    {"sku": "SKU-003", "name": "Gadget X", "price": 49.99, "category_id": "cat-2", "active": False},
]

# cluster with auth and a fixed cache budget
db = mongodb(
    name                = "mongo",
    port                = 27017,
    username            = "root",
    password            = "secret",
    wired_tiger_cache_gb = 1,
)

# create all collections across all databases
collection_nodes = []
for database in DATABASES:
    for collection in COLLECTIONS.get(database, []):
        n = create_collection(cluster=db, db=database, collection=collection)
        collection_nodes.append(n)

# create indexes — build after the corresponding collection exists
index_nodes = []
for spec_key, index_list in INDEXES.items():
    parts      = spec_key.split(".")
    database   = parts[0]
    collection = parts[1]
    for keys in index_list:
        n = create_index(
            cluster    = db,
            db         = database,
            collection = collection,
            keys       = keys,
            unique     = False,
            after      = collection_nodes,
        )
        index_nodes.append(n)

# create a unique index on products.sku separately
sku_index = create_index(
    cluster    = db,
    db         = "catalog",
    collection = "products",
    keys       = {"sku": 1},
    unique     = True,
    after      = collection_nodes,
)
index_nodes.append(sku_index)

# seed product data
seed = insert_documents(
    cluster    = db,
    db         = "catalog",
    collection = "products",
    documents  = SEED_PRODUCTS,
    after      = index_nodes,
)

# run a migration script for the analytics schema
migrate = run_script(
    cluster = db,
    db      = "analytics",
    script_path = "migrations/001_partition_events.js",
    after   = [seed],
)

# application that reads catalog and writes orders
app = service.run(
    name  = "catalog-api",
    env   = {"MONGO_URI": db["uri"]},
    after = [seed, migrate],
)

test.run(
    name  = "mongo-smoke",
    file  = "mongo_smoke.py",
    image = "python:3.12",
    after = [app],
    env   = {
        "MONGO_URI":   db["uri"],
        "DATABASES":   ",".join(DATABASES),
    },
)
