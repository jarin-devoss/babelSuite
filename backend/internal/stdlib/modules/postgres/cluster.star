load("@babelsuite/runtime", "service")
load("_shared.star", "merge_dicts")

def pg(name = "db", image = "postgres:16", database = "app", username = "postgres", password = "postgres", port = 5432, after = [], env = {}):
    return service.run(
        name  = name,
        image = image,
        after = after,
        env   = merge_dicts({
            "POSTGRES_DB":       database,
            "POSTGRES_USER":     username,
            "POSTGRES_PASSWORD": password,
            "PGPORT":            str(port),
        }, env),
    )
