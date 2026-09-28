wit_bindgen::generate!({ path: "actor.wit", world: "actor" });

use rusqlite::{Connection, Error as SqlError, OptionalExtension};
use std::ffi::CStr;
use std::fmt::Write as _;

unsafe extern "C" {
    fn sqlite3_os_init() -> i32;
    fn sqlite3_os_end() -> i32;
}

#[used]
static KEEP_MEMVFS_INIT: unsafe extern "C" fn() -> i32 = sqlite3_os_init;
#[used]
static KEEP_MEMVFS_END: unsafe extern "C" fn() -> i32 = sqlite3_os_end;

const MAX_LOAD: i64 = 1_000_000;

enum Outcome {
    Reply(String),
    Stop,
}

struct Database {
    conn: Option<Connection>,
}

impl Database {
    fn new() -> Self {
        Self { conn: None }
    }

    fn handle(&mut self, topic: &str) -> Result<Outcome, String> {
        if topic == "stop" {
            self.conn = None;
            return Ok(Outcome::Stop);
        }
        if topic == "sum" {
            return self.sum().map(Outcome::Reply);
        }
        if topic == "stats" {
            return self.stats().map(Outcome::Reply);
        }
        if let Some(rest) = topic.strip_prefix("load:") {
            require_single_field(rest, "load expects load:N")?;
            let n = parse_i64(rest, "load count")?;
            return self.load(n).map(Outcome::Reply);
        }
        if let Some(rest) = topic.strip_prefix("get:") {
            require_single_field(rest, "get expects get:ID")?;
            let id = parse_i64(rest, "id")?;
            return self.get(id).map(Outcome::Reply);
        }
        if let Some(rest) = topic.strip_prefix("put:") {
            let (id_s, value_s) = split_put(rest)?;
            let id = parse_i64(id_s, "id")?;
            let value = parse_i64(value_s, "value")?;
            return self.put(id, value).map(Outcome::Reply);
        }
        Err(format!("unknown topic: {topic}"))
    }

    fn load(&mut self, n: i64) -> Result<String, String> {
        if n < 0 {
            return Err(format!("load count {n} is negative"));
        }
        if n > MAX_LOAD {
            return Err(format!("load count {n} exceeds maximum {MAX_LOAD}"));
        }
        let mut conn = open_memory()?;
        conn.execute_batch(
            "PRAGMA journal_mode=MEMORY;
             PRAGMA temp_store=MEMORY;
             PRAGMA synchronous=OFF;
             CREATE TABLE docs(
                 id INTEGER PRIMARY KEY,
                 value INTEGER,
                 body TEXT
             );",
        )
        .map_err(sql_err("create docs table"))?;
        {
            let tx = conn.transaction().map_err(sql_err("begin load transaction"))?;
            {
                let mut stmt = tx
                    .prepare("INSERT INTO docs(id, value, body) VALUES (?1, ?2, ?3)")
                    .map_err(sql_err("prepare insert"))?;
                let mut body = String::with_capacity(64);
                for id in 0..n {
                    let value = id.checked_mul(3).ok_or_else(|| format!("value overflow for id {id}"))?;
                    body.clear();
                    write_body(id, &mut body)?;
                    stmt.execute((id, value, body.as_str()))
                        .map_err(sql_err("insert document"))?;
                }
            }
            tx.commit().map_err(sql_err("commit load transaction"))?;
        }
        conn.execute(
            "CREATE INDEX docs_value_idx ON docs(value)",
            [],
        )
        .map_err(sql_err("create value index"))?;
        self.conn = Some(conn);
        Ok(format!("loaded:{n}"))
    }

    fn connection(&self) -> Result<&Connection, String> {
        self.conn
            .as_ref()
            .ok_or_else(|| String::from("database not loaded"))
    }

    fn get(&self, id: i64) -> Result<String, String> {
        let conn = self.connection()?;
        let mut stmt = conn
            .prepare_cached("SELECT value FROM docs WHERE id = ?1")
            .map_err(sql_err("prepare get"))?;
        let value: i64 = stmt
            .query_row((id,), |row| row.get(0))
            .optional()
            .map_err(sql_err("get document"))?
            .ok_or_else(|| format!("id {id} not found"))?;
        Ok(format!("value:{id}:{value}"))
    }

    fn put(&self, id: i64, value: i64) -> Result<String, String> {
        let conn = self.connection()?;
        let mut stmt = conn
            .prepare_cached("UPDATE docs SET value = ?1 WHERE id = ?2")
            .map_err(sql_err("prepare put"))?;
        let changed = stmt
            .execute((value, id))
            .map_err(sql_err("update document"))?;
        if changed == 0 {
            return Err(format!("id {id} not found"));
        }
        Ok(format!("updated:{id}:{value}"))
    }

    fn sum(&self) -> Result<String, String> {
        let conn = self.connection()?;
        let mut stmt = conn
            .prepare_cached("SELECT COUNT(*), COALESCE(SUM(value), 0) FROM docs")
            .map_err(sql_err("prepare sum"))?;
        let (count, total): (i64, i64) = stmt
            .query_row([], |row| Ok((row.get(0)?, row.get(1)?)))
            .map_err(sql_err("sum documents"))?;
        Ok(format!("sum:{count}:{total}"))
    }

    fn stats(&self) -> Result<String, String> {
        let conn = self.connection()?;
        let used = unsafe { rusqlite::ffi::sqlite3_memory_used() };
        let highwater = unsafe { rusqlite::ffi::sqlite3_memory_highwater(0) };
        let page_count: i64 = pragma_i64(conn, "PRAGMA page_count")?;
        let page_size: i64 = pragma_i64(conn, "PRAGMA page_size")?;
        Ok(format!("stats:{used}:{highwater}:{page_count}:{page_size}"))
    }
}

fn pragma_i64(conn: &Connection, sql: &'static str) -> Result<i64, String> {
    let mut stmt = conn
        .prepare_cached(sql)
        .map_err(sql_err("prepare pragma"))?;
    stmt.query_row([], |row| row.get(0))
        .map_err(sql_err("read pragma"))
}

fn open_memory() -> Result<Connection, String> {
    let mut ptr = std::ptr::null_mut();
    let filename = c":memory:";
    let vfs = c"mem";
    let flags = rusqlite::ffi::SQLITE_OPEN_READWRITE
        | rusqlite::ffi::SQLITE_OPEN_CREATE
        | rusqlite::ffi::SQLITE_OPEN_MEMORY
        | rusqlite::ffi::SQLITE_OPEN_NOMUTEX;
    let rc = unsafe { rusqlite::ffi::sqlite3_open_v2(filename.as_ptr(), &mut ptr, flags, vfs.as_ptr()) };
    if rc != rusqlite::ffi::SQLITE_OK {
        let msg = sqlite_errmsg(ptr);
        unsafe {
            rusqlite::ffi::sqlite3_close(ptr);
        }
        return Err(format!("open in-memory database: {msg} (code {rc})"));
    }
    unsafe { Connection::from_handle_owned(ptr) }.map_err(sql_err("wrap sqlite handle"))
}

fn sqlite_errmsg(ptr: *mut rusqlite::ffi::sqlite3) -> String {
    if ptr.is_null() {
        return String::from("null sqlite handle");
    }
    let raw = unsafe { rusqlite::ffi::sqlite3_errmsg(ptr) };
    if raw.is_null() {
        return String::from("unknown sqlite error");
    }
    unsafe { CStr::from_ptr(raw) }.to_string_lossy().into_owned()
}

fn write_body(id: i64, body: &mut String) -> Result<(), String> {
    write!(body, "document {id} group {} indexing workload", id % 100)
        .map_err(|_| String::from("failed to write document body"))
}

fn require_single_field(rest: &str, message: &str) -> Result<(), String> {
    if rest.contains(':') {
        return Err(String::from(message));
    }
    Ok(())
}

fn split_put(rest: &str) -> Result<(&str, &str), String> {
    let (id, value) = rest
        .split_once(':')
        .ok_or_else(|| String::from("put expects put:ID:VALUE"))?;
    if value.contains(':') {
        return Err(String::from("put expects put:ID:VALUE"));
    }
    Ok((id, value))
}

fn parse_i64(raw: &str, what: &str) -> Result<i64, String> {
    if raw.is_empty() {
        return Err(format!("{what} is empty"));
    }
    if raw.as_bytes()[0] == b'+' {
        return Err(format!("{what} is not a valid integer"));
    }
    raw.parse::<i64>()
        .map_err(|_| format!("{what} is not a valid integer"))
}

fn sql_err(op: &'static str) -> impl Fn(SqlError) -> String {
    move |err| format!("{op}: {err}")
}

struct Actor;

impl Guest for Actor {
    fn run() -> Result<(), String> {
        use wippy::actor::process;
        let mut db = Database::new();
        loop {
            let message = process::receive()?;
            match db.handle(&message.topic) {
                Ok(Outcome::Stop) => return Ok(()),
                Ok(Outcome::Reply(text)) => {
                    send_text(&message.from, "result", &text)?;
                }
                Err(text) => {
                    send_text(&message.from, "error", &text)?;
                }
            }
        }
    }
}

fn send_text(target: &str, topic: &str, text: &str) -> Result<(), String> {
    use wippy::actor::process;
    process::send(
        target,
        topic,
        &[process::Payload {
            format: String::from("text"),
            data: text.as_bytes().to_vec(),
        }],
    )?;
    Ok(())
}

export!(Actor);

#[cfg(test)]
mod tests {
    use super::{Database, Outcome, MAX_LOAD};

    fn reply(db: &mut Database, topic: &str) -> String {
        match db.handle(topic) {
            Ok(Outcome::Reply(text)) => text,
            Ok(Outcome::Stop) => panic!("unexpected stop for {topic}"),
            Err(err) => panic!("unexpected error for {topic}: {err}"),
        }
    }

    fn error(db: &mut Database, topic: &str) -> String {
        match db.handle(topic) {
            Err(err) => err,
            other => panic!("expected error for {topic}, got {other:?}"),
        }
    }

    impl std::fmt::Debug for Outcome {
        fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
            match self {
                Outcome::Reply(text) => write!(f, "Reply({text})"),
                Outcome::Stop => write!(f, "Stop"),
            }
        }
    }

    #[test]
    fn load_get_put_sum_stats_stop() {
        let mut db = Database::new();
        assert_eq!(reply(&mut db, "load:3"), "loaded:3");
        assert_eq!(reply(&mut db, "get:0"), "value:0:0");
        assert_eq!(reply(&mut db, "get:1"), "value:1:3");
        assert_eq!(reply(&mut db, "get:2"), "value:2:6");
        assert_eq!(reply(&mut db, "sum"), "sum:3:9");
        assert_eq!(reply(&mut db, "put:1:9"), "updated:1:9");
        assert_eq!(reply(&mut db, "get:1"), "value:1:9");
        assert_eq!(reply(&mut db, "sum"), "sum:3:15");
        let stats = reply(&mut db, "stats");
        let parts: Vec<&str> = stats.split(':').collect();
        assert_eq!(parts.len(), 5);
        assert_eq!(parts[0], "stats");
        for part in &parts[1..] {
            part.parse::<i64>().expect("stats field");
        }
        let page_count: i64 = parts[3].parse().unwrap();
        let page_size: i64 = parts[4].parse().unwrap();
        assert!(page_count >= 1);
        assert!(page_size >= 512);
        let body: String = db
            .connection()
            .unwrap()
            .query_row("SELECT body FROM docs WHERE id = 2", [], |row| row.get(0))
            .unwrap();
        assert_eq!(body, "document 2 group 2 indexing workload");
        match db.handle("stop") {
            Ok(Outcome::Stop) => {}
            other => panic!("expected stop, got {other:?}"),
        }
        let err = error(&mut db, "sum");
        assert!(err.contains("not loaded"), "{err}");
    }

    #[test]
    fn load_resets_and_enforces_bounds() {
        let mut db = Database::new();
        assert_eq!(reply(&mut db, "load:2"), "loaded:2");
        assert_eq!(reply(&mut db, "put:0:100"), "updated:0:100");
        assert_eq!(reply(&mut db, "load:1"), "loaded:1");
        assert_eq!(reply(&mut db, "get:0"), "value:0:0");
        let missing = error(&mut db, "get:1");
        assert!(missing.contains("not found"), "{missing}");
        let too_big = error(&mut db, &format!("load:{}", MAX_LOAD + 1));
        assert!(too_big.contains("exceeds maximum"), "{too_big}");
        let negative = error(&mut db, "load:-1");
        assert!(negative.contains("negative"), "{negative}");
        let empty = error(&mut db, "load:");
        assert!(empty.contains("empty"), "{empty}");
        assert_eq!(reply(&mut db, "load:0"), "loaded:0");
        assert_eq!(reply(&mut db, "sum"), "sum:0:0");
        assert_eq!(MAX_LOAD, 1_000_000);
    }

    #[test]
    fn rejects_invalid_topics_and_missing_rows() {
        let mut db = Database::new();
        let unknown = error(&mut db, "probe");
        assert!(unknown.contains("unknown topic"), "{unknown}");
        let before = error(&mut db, "get:0");
        assert!(before.contains("not loaded"), "{before}");
        assert_eq!(reply(&mut db, "load:1"), "loaded:1");
        let missing_get = error(&mut db, "get:9");
        assert!(missing_get.contains("not found"), "{missing_get}");
        let missing_put = error(&mut db, "put:9:1");
        assert!(missing_put.contains("not found"), "{missing_put}");
        let bad_put = error(&mut db, "put:1");
        assert!(bad_put.contains("put:ID:VALUE"), "{bad_put}");
        let extra = error(&mut db, "get:1:2");
        assert!(extra.contains("get:ID"), "{extra}");
        let plus = error(&mut db, "get:+1");
        assert!(plus.contains("not a valid integer"), "{plus}");
        let group = db
            .connection()
            .unwrap()
            .query_row("SELECT body FROM docs WHERE id = 0", [], |row| {
                row.get::<_, String>(0)
            })
            .unwrap();
        assert_eq!(group, "document 0 group 0 indexing workload");
    }
}
