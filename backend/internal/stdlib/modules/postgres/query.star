load("@babelsuite/runtime", "task")

# _psql is the psql command for a database, built from what the service was
# created with (db.env): its port, user, password and database name, so pg(port=,
# database=, password=) is honoured by every helper. The values go in libpq's own
# environment variables, which need no URL encoding. A POSTGRES_URL in the step's
# env (a suite dependency's input, say) still wins, as the connection string.
def _psql(db):
    env = db.env
    return (
        "PGHOST=" + utils.quoted(db.name)
        + " PGPORT=" + utils.quoted(env.get("PGPORT", "5432"))
        + " PGUSER=" + utils.quoted(env.get("POSTGRES_USER", "postgres"))
        + " PGPASSWORD=" + utils.quoted(env.get("POSTGRES_PASSWORD", "postgres"))
        + " PGDATABASE=" + utils.quoted(env.get("POSTGRES_DB", "app"))
        + ' psql ${POSTGRES_URL:+"$POSTGRES_URL"}'
    )

def _query(db, name, sql, image = "postgres:16", after = []):
    return task.run(
        name     = name,
        image    = image,
        after    = [db] + after,
        commands = [_psql(db) + " -v ON_ERROR_STOP=1 -c " + utils.quoted(sql)],
    )

def connect(db, image = "postgres:16", after = []):
    return task.run(
        name     = db.name + "-connect",
        image    = image,
        after    = [db] + after,
        commands = ["for i in $(seq 1 30); do " + _psql(db) + " -v ON_ERROR_STOP=1 -c 'select 1;' && exit 0; sleep 2; done; exit 1"],
    )

def query(db, sql, name = None, image = "postgres:16", after = []):
    return _query(db, name or (db.name + "-query-" + utils.sanitize(sql[:24])), sql, image=image, after=after)

def insert(db, table, values, image = "postgres:16", after = []):
    columns = []
    vals = []
    for key, value in values.items():
        columns.append(str(key))
        vals.append(utils.sql_value(value))
    sql = "insert into " + table + " (" + ", ".join(columns) + ") values (" + ", ".join(vals) + ");"
    return _query(db, db.name + "-insert-" + utils.sanitize(table), sql, image=image, after=after)

def select(db, table, columns = ["*"], where = None, image = "postgres:16", after = []):
    sql = "select " + ", ".join(columns) + " from " + table
    if where != None:
        sql += " where " + utils.sql_predicate(where)
    sql += ";"
    return _query(db, db.name + "-select-" + utils.sanitize(table), sql, image=image, after=after)

def delete(db, table, where, image = "postgres:16", after = []):
    sql = "delete from " + table + " where " + utils.sql_predicate(where) + ";"
    return _query(db, db.name + "-delete-" + utils.sanitize(table), sql, image=image, after=after)

def upsert(db, table, values, conflict_columns, image = "postgres:16", after = []):
    columns = []
    vals = []
    assignments = []
    for key, value in values.items():
        columns.append(str(key))
        vals.append(utils.sql_value(value))
        assignments.append(str(key) + " = excluded." + str(key))
    sql = (
        "insert into " + table
        + " (" + ", ".join(columns) + ")"
        + " values (" + ", ".join(vals) + ")"
        + " on conflict (" + ", ".join(conflict_columns) + ")"
        + " do update set " + ", ".join(assignments) + ";"
    )
    return _query(db, db.name + "-upsert-" + utils.sanitize(table), sql, image=image, after=after)
