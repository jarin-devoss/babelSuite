load("@babelsuite/postgres", "pg", "connect", "insert", "select", "upsert", "delete")
load("@babelsuite/runtime",  "service", "log")

db = pg(name="payments-db", database="payments")

ready = connect(db)

seed = insert(
    db,
    table  = "merchants",
    values = {"merchant_id": "m-100", "status": "active"},
    after  = [ready],
)

bump = upsert(
    db,
    table            = "merchants",
    values           = {"merchant_id": "m-100", "status": "vip"},
    conflict_columns = ["merchant_id"],
    after            = [seed],
)

read = select(db, "merchants", columns=["merchant_id", "status"], where={"merchant_id": "m-100"}, after=[bump])

seeded = log.info("seed data applied", after=[read])

api = service.run(
    name  = "payments-api",
    image = "ghcr.io/acme/payments-api:latest",
    after = [seeded],
)

delete(db, "merchants", where={"merchant_id": "m-100"}, after=[api])
