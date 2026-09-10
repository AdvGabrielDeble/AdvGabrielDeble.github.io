//go:build windows

package main

import (
    "fmt"
    "strconv"
    "syscall"
    "unsafe"
)

const (
    sqliteOK   = 0
    sqliteRow  = 100
    sqliteDone = 101

    sqliteOpenReadWrite = 0x00000002
    sqliteOpenCreate    = 0x00000004
    sqliteOpenFullMutex = 0x00010000
)

var (
    sqliteDLL            = syscall.NewLazyDLL("winsqlite3.dll")
    sqliteOpenV2         = sqliteDLL.NewProc("sqlite3_open_v2")
    sqliteCloseV2        = sqliteDLL.NewProc("sqlite3_close_v2")
    sqliteErrmsg         = sqliteDLL.NewProc("sqlite3_errmsg")
    sqliteExec           = sqliteDLL.NewProc("sqlite3_exec")
    sqliteFree           = sqliteDLL.NewProc("sqlite3_free")
    sqlitePrepareV2      = sqliteDLL.NewProc("sqlite3_prepare_v2")
    sqliteStep           = sqliteDLL.NewProc("sqlite3_step")
    sqliteFinalize       = sqliteDLL.NewProc("sqlite3_finalize")
    sqliteBindText       = sqliteDLL.NewProc("sqlite3_bind_text")
    sqliteBindInt64      = sqliteDLL.NewProc("sqlite3_bind_int64")
    sqliteBindNull       = sqliteDLL.NewProc("sqlite3_bind_null")
    sqliteColumnCount    = sqliteDLL.NewProc("sqlite3_column_count")
    sqliteColumnName     = sqliteDLL.NewProc("sqlite3_column_name")
    sqliteColumnText     = sqliteDLL.NewProc("sqlite3_column_text")
    sqliteColumnBytes    = sqliteDLL.NewProc("sqlite3_column_bytes")
    sqliteLastInsertID   = sqliteDLL.NewProc("sqlite3_last_insert_rowid")
    sqliteChanges        = sqliteDLL.NewProc("sqlite3_changes")
    sqliteBusyTimeout    = sqliteDLL.NewProc("sqlite3_busy_timeout")
)

type DB struct {
    handle uintptr
}

func cString(ptr uintptr) string {
    if ptr == 0 {
        return ""
    }
    p := ptr
    n := 0
    for {
        b := *(*byte)(unsafe.Pointer(p + uintptr(n)))
        if b == 0 {
            break
        }
        n++
    }
    if n == 0 {
        return ""
    }
    return string(unsafe.Slice((*byte)(unsafe.Pointer(ptr)), n))
}

func bytePtr(s string) (*byte, error) {
    return syscall.BytePtrFromString(s)
}

func OpenDB(path string) (*DB, error) {
    p, err := bytePtr(path)
    if err != nil {
        return nil, err
    }
    var handle uintptr
    rc, _, _ := sqliteOpenV2.Call(
        uintptr(unsafe.Pointer(p)),
        uintptr(unsafe.Pointer(&handle)),
        uintptr(sqliteOpenReadWrite|sqliteOpenCreate|sqliteOpenFullMutex),
        0,
    )
    if int(rc) != sqliteOK || handle == 0 {
        return nil, fmt.Errorf("sqlite3_open_v2 falhou: rc=%d", rc)
    }
    db := &DB{handle: handle}
    sqliteBusyTimeout.Call(handle, 15000)
    return db, nil
}

func (d *DB) Close() error {
    if d == nil || d.handle == 0 {
        return nil
    }
    rc, _, _ := sqliteCloseV2.Call(d.handle)
    if int(rc) != sqliteOK {
        return fmt.Errorf("sqlite3_close_v2 falhou: %s", d.errmsg())
    }
    d.handle = 0
    return nil
}

func (d *DB) errmsg() string {
    if d == nil || d.handle == 0 {
        return "banco fechado"
    }
    p, _, _ := sqliteErrmsg.Call(d.handle)
    return cString(p)
}

func (d *DB) ExecScript(sql string) error {
    p, err := bytePtr(sql)
    if err != nil {
        return err
    }
    var errMsg uintptr
    rc, _, _ := sqliteExec.Call(
        d.handle,
        uintptr(unsafe.Pointer(p)),
        0,
        0,
        uintptr(unsafe.Pointer(&errMsg)),
    )
    if int(rc) != sqliteOK {
        msg := d.errmsg()
        if errMsg != 0 {
            msg = cString(errMsg)
            sqliteFree.Call(errMsg)
        }
        return fmt.Errorf("sqlite exec: %s", msg)
    }
    return nil
}

func (d *DB) prepare(sql string) (uintptr, error) {
    p, err := bytePtr(sql)
    if err != nil {
        return 0, err
    }
    var stmt uintptr
    rc, _, _ := sqlitePrepareV2.Call(
        d.handle,
        uintptr(unsafe.Pointer(p)),
        ^uintptr(0),
        uintptr(unsafe.Pointer(&stmt)),
        0,
    )
    if int(rc) != sqliteOK || stmt == 0 {
        return 0, fmt.Errorf("sqlite prepare: %s", d.errmsg())
    }
    return stmt, nil
}

func bindValue(stmt uintptr, index int, value any) error {
    var rc uintptr
    switch v := value.(type) {
    case nil:
        rc, _, _ = sqliteBindNull.Call(stmt, uintptr(index))
    case int:
        rc, _, _ = sqliteBindInt64.Call(stmt, uintptr(index), uintptr(int64(v)))
    case int64:
        rc, _, _ = sqliteBindInt64.Call(stmt, uintptr(index), uintptr(v))
    case uint:
        rc, _, _ = sqliteBindInt64.Call(stmt, uintptr(index), uintptr(int64(v)))
    case bool:
        n := int64(0)
        if v {
            n = 1
        }
        rc, _, _ = sqliteBindInt64.Call(stmt, uintptr(index), uintptr(n))
    default:
        s := fmt.Sprint(v)
        p, err := bytePtr(s)
        if err != nil {
            return err
        }
        rc, _, _ = sqliteBindText.Call(
            stmt,
            uintptr(index),
            uintptr(unsafe.Pointer(p)),
            ^uintptr(0),
            ^uintptr(0), // SQLITE_TRANSIENT
        )
    }
    if int(rc) != sqliteOK {
        return fmt.Errorf("sqlite bind %d falhou: rc=%d", index, rc)
    }
    return nil
}

func (d *DB) Exec(sql string, args ...any) (int64, error) {
    stmt, err := d.prepare(sql)
    if err != nil {
        return 0, err
    }
    defer sqliteFinalize.Call(stmt)
    for i, arg := range args {
        if err := bindValue(stmt, i+1, arg); err != nil {
            return 0, err
        }
    }
    rc, _, _ := sqliteStep.Call(stmt)
    if int(rc) != sqliteDone && int(rc) != sqliteRow {
        return 0, fmt.Errorf("sqlite step: %s", d.errmsg())
    }
    id, _, _ := sqliteLastInsertID.Call(d.handle)
    return int64(id), nil
}

func (d *DB) Changes() int64 {
    n, _, _ := sqliteChanges.Call(d.handle)
    return int64(n)
}

func (d *DB) Query(sql string, args ...any) ([]map[string]string, error) {
    stmt, err := d.prepare(sql)
    if err != nil {
        return nil, err
    }
    defer sqliteFinalize.Call(stmt)
    for i, arg := range args {
        if err := bindValue(stmt, i+1, arg); err != nil {
            return nil, err
        }
    }
    var rows []map[string]string
    for {
        rc, _, _ := sqliteStep.Call(stmt)
        switch int(rc) {
        case sqliteDone:
            return rows, nil
        case sqliteRow:
            count, _, _ := sqliteColumnCount.Call(stmt)
            row := make(map[string]string, int(count))
            for i := 0; i < int(count); i++ {
                np, _, _ := sqliteColumnName.Call(stmt, uintptr(i))
                tp, _, _ := sqliteColumnText.Call(stmt, uintptr(i))
                nb, _, _ := sqliteColumnBytes.Call(stmt, uintptr(i))
                name := cString(np)
                if tp == 0 || nb == 0 {
                    row[name] = ""
                } else {
                    row[name] = string(unsafe.Slice((*byte)(unsafe.Pointer(tp)), int(nb)))
                }
            }
            rows = append(rows, row)
        default:
            return nil, fmt.Errorf("sqlite query step rc=%d: %s", rc, d.errmsg())
        }
    }
}

func (d *DB) QueryOne(sql string, args ...any) (map[string]string, error) {
    rows, err := d.Query(sql, args...)
    if err != nil {
        return nil, err
    }
    if len(rows) == 0 {
        return nil, nil
    }
    return rows[0], nil
}

func intField(row map[string]string, key string) int64 {
    n, _ := strconv.ParseInt(row[key], 10, 64)
    return n
}
