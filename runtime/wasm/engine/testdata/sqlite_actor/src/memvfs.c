#include "sqlite3.h"

#include <stdlib.h>
#include <string.h>

#define MEMVFS_MAX_FILES 64
#define MEMVFS_MAX_PATH 512

typedef struct MemBlob {
    int in_use;
    int refcount;
    int delete_on_close;
    sqlite3_int64 size;
    sqlite3_int64 cap;
    unsigned char *data;
    char name[MEMVFS_MAX_PATH];
} MemBlob;

typedef struct MemFile {
    sqlite3_file base;
    MemBlob *blob;
} MemFile;

static MemBlob g_files[MEMVFS_MAX_FILES];
static unsigned g_rng = 0xC0FFEEU;
static sqlite3_int64 g_now = 24405875; /* 10 * Julian day at Unix epoch */

static int memGrow(MemBlob *blob, sqlite3_int64 need) {
    sqlite3_int64 cap;
    unsigned char *data;
    if (need <= blob->cap) {
        return SQLITE_OK;
    }
    if (need < 0) {
        return SQLITE_FULL;
    }
    cap = blob->cap == 0 ? 4096 : blob->cap;
    while (cap < need) {
        if (cap > ((sqlite3_int64)1 << 40)) {
            return SQLITE_FULL;
        }
        cap *= 2;
    }
    data = (unsigned char *)realloc(blob->data, (size_t)cap);
    if (data == 0) {
        return SQLITE_NOMEM;
    }
    if (cap > blob->cap) {
        memset(data + blob->cap, 0, (size_t)(cap - blob->cap));
    }
    blob->data = data;
    blob->cap = cap;
    return SQLITE_OK;
}

static MemBlob *memFind(const char *name) {
    int i;
    if (name == 0 || name[0] == 0) {
        return 0;
    }
    for (i = 0; i < MEMVFS_MAX_FILES; i++) {
        if (g_files[i].in_use && strncmp(g_files[i].name, name, MEMVFS_MAX_PATH) == 0) {
            return &g_files[i];
        }
    }
    return 0;
}

static MemBlob *memAllocSlot(void) {
    int i;
    for (i = 0; i < MEMVFS_MAX_FILES; i++) {
        if (!g_files[i].in_use) {
            memset(&g_files[i], 0, sizeof(MemBlob));
            g_files[i].in_use = 1;
            return &g_files[i];
        }
    }
    return 0;
}

static void memRelease(MemBlob *blob) {
    if (blob == 0) {
        return;
    }
    blob->refcount--;
    if (blob->refcount > 0) {
        return;
    }
    if (blob->delete_on_close) {
        free(blob->data);
        memset(blob, 0, sizeof(MemBlob));
    }
}

static int memClose(sqlite3_file *file) {
    MemFile *mem = (MemFile *)file;
    memRelease(mem->blob);
    mem->blob = 0;
    return SQLITE_OK;
}

static int memRead(sqlite3_file *file, void *buf, int amt, sqlite3_int64 off) {
    MemBlob *blob = ((MemFile *)file)->blob;
    sqlite3_int64 avail;
    if (blob == 0 || amt < 0 || off < 0) {
        return SQLITE_IOERR_READ;
    }
    if (off >= blob->size) {
        memset(buf, 0, (size_t)amt);
        return SQLITE_IOERR_SHORT_READ;
    }
    avail = blob->size - off;
    if (avail >= amt) {
        memcpy(buf, blob->data + off, (size_t)amt);
        return SQLITE_OK;
    }
    memcpy(buf, blob->data + off, (size_t)avail);
    memset((unsigned char *)buf + avail, 0, (size_t)amt - (size_t)avail);
    return SQLITE_IOERR_SHORT_READ;
}

static int memWrite(sqlite3_file *file, const void *buf, int amt, sqlite3_int64 off) {
    MemBlob *blob = ((MemFile *)file)->blob;
    sqlite3_int64 end;
    int rc;
    if (blob == 0 || amt < 0 || off < 0) {
        return SQLITE_IOERR_WRITE;
    }
    end = off + amt;
    rc = memGrow(blob, end);
    if (rc != SQLITE_OK) {
        return rc;
    }
    memcpy(blob->data + off, buf, (size_t)amt);
    if (end > blob->size) {
        blob->size = end;
    }
    return SQLITE_OK;
}

static int memTruncate(sqlite3_file *file, sqlite3_int64 size) {
    MemBlob *blob = ((MemFile *)file)->blob;
    int rc;
    if (blob == 0 || size < 0) {
        return SQLITE_IOERR_TRUNCATE;
    }
    if (size > blob->size) {
        rc = memGrow(blob, size);
        if (rc != SQLITE_OK) {
            return rc;
        }
    }
    blob->size = size;
    return SQLITE_OK;
}

static int memSync(sqlite3_file *file, int flags) {
    (void)file;
    (void)flags;
    return SQLITE_OK;
}

static int memFileSize(sqlite3_file *file, sqlite3_int64 *size) {
    MemBlob *blob = ((MemFile *)file)->blob;
    if (blob == 0 || size == 0) {
        return SQLITE_IOERR_FSTAT;
    }
    *size = blob->size;
    return SQLITE_OK;
}

static int memLock(sqlite3_file *file, int lock) {
    (void)file;
    (void)lock;
    return SQLITE_OK;
}

static int memUnlock(sqlite3_file *file, int lock) {
    (void)file;
    (void)lock;
    return SQLITE_OK;
}

static int memCheckReservedLock(sqlite3_file *file, int *out) {
    (void)file;
    if (out) {
        *out = 0;
    }
    return SQLITE_OK;
}

static int memFileControl(sqlite3_file *file, int op, void *arg) {
    (void)file;
    (void)op;
    (void)arg;
    return SQLITE_NOTFOUND;
}

static int memSectorSize(sqlite3_file *file) {
    (void)file;
    return 4096;
}

static int memDeviceCharacteristics(sqlite3_file *file) {
    (void)file;
    return SQLITE_IOCAP_ATOMIC | SQLITE_IOCAP_SAFE_APPEND | SQLITE_IOCAP_SEQUENTIAL
        | SQLITE_IOCAP_POWERSAFE_OVERWRITE | SQLITE_IOCAP_UNDELETABLE_WHEN_OPEN;
}

static const sqlite3_io_methods g_io = {
    1,
    memClose,
    memRead,
    memWrite,
    memTruncate,
    memSync,
    memFileSize,
    memLock,
    memUnlock,
    memCheckReservedLock,
    memFileControl,
    memSectorSize,
    memDeviceCharacteristics,
};

static int memOpen(
    sqlite3_vfs *vfs,
    const char *name,
    sqlite3_file *file,
    int flags,
    int *out_flags
) {
    MemFile *mem = (MemFile *)file;
    MemBlob *blob;
    (void)vfs;
    memset(mem, 0, sizeof(*mem));
    if (name != 0 && name[0] != 0) {
        blob = memFind(name);
    } else {
        blob = 0;
    }
    if (blob == 0) {
        if ((flags & SQLITE_OPEN_CREATE) == 0 && name != 0 && name[0] != 0) {
            return SQLITE_CANTOPEN;
        }
        blob = memAllocSlot();
        if (blob == 0) {
            return SQLITE_CANTOPEN;
        }
        if (name != 0 && name[0] != 0) {
            strncpy(blob->name, name, MEMVFS_MAX_PATH - 1);
            blob->name[MEMVFS_MAX_PATH - 1] = 0;
        }
        blob->delete_on_close = (flags & SQLITE_OPEN_DELETEONCLOSE) != 0 || name == 0 || name[0] == 0;
    }
    blob->refcount++;
    mem->blob = blob;
    mem->base.pMethods = &g_io;
    if (out_flags) {
        *out_flags = flags;
    }
    return SQLITE_OK;
}

static int memDelete(sqlite3_vfs *vfs, const char *name, int sync_dir) {
    MemBlob *blob;
    (void)vfs;
    (void)sync_dir;
    blob = memFind(name);
    if (blob == 0) {
        return SQLITE_OK;
    }
    if (blob->refcount > 0) {
        blob->delete_on_close = 1;
        return SQLITE_OK;
    }
    free(blob->data);
    memset(blob, 0, sizeof(MemBlob));
    return SQLITE_OK;
}

static int memAccess(sqlite3_vfs *vfs, const char *name, int flags, int *out) {
    (void)vfs;
    (void)flags;
    if (out == 0) {
        return SQLITE_IOERR;
    }
    *out = memFind(name) != 0;
    return SQLITE_OK;
}

static int memFullPathname(sqlite3_vfs *vfs, const char *name, int n_out, char *out) {
    (void)vfs;
    if (name == 0 || out == 0 || n_out <= 0) {
        return SQLITE_CANTOPEN;
    }
    strncpy(out, name, (size_t)n_out - 1);
    out[n_out - 1] = 0;
    return SQLITE_OK;
}

static void *memDlOpen(sqlite3_vfs *vfs, const char *path) {
    (void)vfs;
    (void)path;
    return 0;
}

static void memDlError(sqlite3_vfs *vfs, int n, char *msg) {
    (void)vfs;
    if (msg && n > 0) {
        strncpy(msg, "loadable extensions are not supported", (size_t)n - 1);
        msg[n - 1] = 0;
    }
}

static void (*memDlSym(sqlite3_vfs *vfs, void *handle, const char *sym))(void) {
    (void)vfs;
    (void)handle;
    (void)sym;
    return 0;
}

static void memDlClose(sqlite3_vfs *vfs, void *handle) {
    (void)vfs;
    (void)handle;
}

static int memRandomness(sqlite3_vfs *vfs, int n, char *out) {
    int i;
    (void)vfs;
    for (i = 0; i < n; i++) {
        g_rng = g_rng * 1664525U + 1013904223U;
        out[i] = (char)(g_rng >> 24);
    }
    return n;
}

static int memSleep(sqlite3_vfs *vfs, int microseconds) {
    (void)vfs;
    return microseconds;
}

static int memCurrentTime(sqlite3_vfs *vfs, double *now) {
    (void)vfs;
    g_now += 1;
    if (now) {
        *now = ((double)g_now) / 10.0;
    }
    return SQLITE_OK;
}

static int memGetLastError(sqlite3_vfs *vfs, int n, char *msg) {
    (void)vfs;
    (void)n;
    (void)msg;
    return 0;
}

static sqlite3_vfs g_vfs = {
    1,
    sizeof(MemFile),
    MEMVFS_MAX_PATH,
    0,
    "mem",
    0,
    memOpen,
    memDelete,
    memAccess,
    memFullPathname,
    memDlOpen,
    memDlError,
    memDlSym,
    memDlClose,
    memRandomness,
    memSleep,
    memCurrentTime,
    memGetLastError,
};

int sqlite3_os_init(void) {
    return sqlite3_vfs_register(&g_vfs, 1);
}

int sqlite3_os_end(void) {
    int i;
    sqlite3_vfs_unregister(&g_vfs);
    for (i = 0; i < MEMVFS_MAX_FILES; i++) {
        free(g_files[i].data);
        memset(&g_files[i], 0, sizeof(MemBlob));
    }
    return SQLITE_OK;
}
