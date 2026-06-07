load("@babelsuite/runtime", "task")

def _query(db, name, sql, image = "postgres:16", after = []):
    url = '"${POSTGRES_URL:-postgresql://${POSTGRES_USER:-postgres}:${POSTGRES_PASSWORD:-postgres}@' + db.name + ':5432/${POSTGRES_DB:-app}}"'
    return task.run(
        name     = name,
        image    = image,
        after    = [db] + after,
        commands = ["psql " + url + " -v ON_ERROR_STOP=1 -c " + utils.quoted(sql)],
    )

def connect(db, image = "postgres:16", after = []):
    url = '"${POSTGRES_URL:-postgresql://${POSTGRES_USER:-postgres}:${POSTGRES_PASSWORD:-postgres}@' + db.name + ':5432/${POSTGRES_DB:-app}}"'
    return task.run(
        name     = db.name + "-connect",
        image    = image,
        after    = [db] + after,
        commands = ["for i in $(seq 1 30); do psql " + url + " -v ON_ERROR_STOP=1 -c 'select 1;' && exit 0; sleep 2; done; exit 1"],
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
