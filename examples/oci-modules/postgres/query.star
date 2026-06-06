load("@babelsuite/runtime", "task")
load("_shared.star", "quoted", "sanitize_name", "sql_value", "sql_predicate")

def _query(db, name, sql, image = "postgres:16", after = []):
    url = '"${POSTGRES_URL:-postgresql://${POSTGRES_USER:-postgres}:${POSTGRES_PASSWORD:-postgres}@' + db.name + ':5432/${POSTGRES_DB:-app}}"'
    return task.run(
        name     = name,
        image    = image,
        after    = [db] + after,
        commands = ["psql " + url + " -v ON_ERROR_STOP=1 -c " + quoted(sql)],
    )

def connect(db, image = "postgres:16", after = []):
    return _query(db, db.name + "-connect", "select 1;", image=image, after=after)

def query(db, sql, name = None, image = "postgres:16", after = []):
    return _query(db, name or (db.name + "-query-" + sanitize_name(sql[:24])), sql, image=image, after=after)

def insert(db, table, values, image = "postgres:16", after = []):
    columns = []
    vals = []
    for key, value in values.items():
        columns.append(str(key))
        vals.append(sql_value(value))
    sql = "insert into " + table + " (" + ", ".join(columns) + ") values (" + ", ".join(vals) + ");"
    return _query(db, db.name + "-insert-" + sanitize_name(table), sql, image=image, after=after)

def select(db, table, columns = ["*"], where = None, image = "postgres:16", after = []):
    sql = "select " + ", ".join(columns) + " from " + table
    if where != None:
        sql += " where " + sql_predicate(where)
    sql += ";"
    return _query(db, db.name + "-select-" + sanitize_name(table), sql, image=image, after=after)

def delete(db, table, where, image = "postgres:16", after = []):
    sql = "delete from " + table + " where " + sql_predicate(where) + ";"
    return _query(db, db.name + "-delete-" + sanitize_name(table), sql, image=image, after=after)

def upsert(db, table, values, conflict_columns, image = "postgres:16", after = []):
    columns = []
    vals = []
    assignments = []
    for key, value in values.items():
        columns.append(str(key))
        vals.append(sql_value(value))
        assignments.append(str(key) + " = excluded." + str(key))
    sql = (
        "insert into " + table
        + " (" + ", ".join(columns) + ")"
        + " values (" + ", ".join(vals) + ")"
        + " on conflict (" + ", ".join(conflict_columns) + ")"
        + " do update set " + ", ".join(assignments) + ";"
    )
    return _query(db, db.name + "-upsert-" + sanitize_name(table), sql, image=image, after=after)
