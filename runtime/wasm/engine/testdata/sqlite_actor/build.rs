fn main() {
    println!("cargo:rerun-if-changed=src/memvfs.c");
    let include = std::env::var("DEP_SQLITE3_INCLUDE").expect(
        "DEP_SQLITE3_INCLUDE missing; libsqlite3-sys bundled build must run first",
    );
    let mut build = cc::Build::new();
    build.file("src/memvfs.c");
    build.include(include);
    build.flag_if_supported("-fPIC");
    build.flag_if_supported("-std=c99");
    build.warnings(false);
    build.compile("memvfs");
}
