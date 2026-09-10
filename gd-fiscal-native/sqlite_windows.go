//go:build windows

package main

import (
    "fmt"
    "syscall"
    "unsafe"
)

const (
    sqliteOK   = 0
    sqliteRow  = 100
    sqliteDone = 101
)

var (
    sqliteDLL = syscall.NewLazyDLL("winsqlite3.dll")
    pOpenV2 = sqliteDLL.NewProc("sqlite3_open_v2")
    pClose = sqliteDLL.NewProc("sqlite3_close")
    pExec = sqliteDLL.NewProc("sqlite3_exec")
    pPrepareV2 = sqliteDLL.NewProc("sqlite3_prepare_v2")
    pBindText = sqliteDLL.NewProc("sqlite3_bind_text")
    pBindInt64 = sqliteDLL.NewProc("sqlite3_bind_int64")
    pStep = sqliteDLL.NewProc("sqlite3_step")
    pFinalize = sqliteDLL.NewProc("sqlite3_finalize")
    pColumnText = sqliteDLL.NewProc("sqlite3_column_text")
    pColumnInt64 = sqliteDLL.NewProc("sqlite3_column_int64")
    pColumnCount = sqliteDLL.NewProc("sqlite3_column_count")
    pErrmsg = sqliteDLL.NewProc("sqlite3_errmsg")
    pLastInsertRowID = sqliteDLL.NewProc("sqlite3_last_insert_rowid")
    pChanges = sqliteDLL.NewProc("sqlite3_changes")
)

type DB struct { ptr uintptr }
type Stmt struct { db *DB; ptr uintptr; keep [][]byte }

func cString(s string) (*byte, error) { return syscall.BytePtrFromString(s) }

func goString(ptr uintptr) string {
    if ptr == 0 { return "" }
    b := make([]byte, 0, 64)
    for i := uintptr(0); ; i++ {
        c := *(*byte)(unsafe.Pointer(ptr + i))
        if c == 0 { break }
        b = append(b, c)
    }
    return string(b)
}

func openDB(path string) (*DB, error) {
    p, err := cString(path); if err != nil { return nil, err }
    var db uintptr
    // READWRITE | CREATE | FULLMUTEX
    rc, _, _ := pOpenV2.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&db)), 0x00000002|0x00000004|0x00010000, 0)
    if int(rc) != sqliteOK || db == 0 { return nil, fmt.Errorf("SQLite open falhou: rc=%d", rc) }
    d := &DB{ptr: db}
    if err := d.Exec("PRAGMA foreign_keys=ON; PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL;"); err != nil { d.Close(); return nil, err }
    return d, nil
}

func (d *DB) Close() { if d != nil && d.ptr != 0 { pClose.Call(d.ptr); d.ptr = 0 } }
func (d *DB) err() error { if d == nil || d.ptr == 0 { return fmt.Errorf("SQLite fechado") }; return fmt.Errorf("SQLite: %s", goString(call1(pErrmsg, d.ptr))) }
func call1(p *syscall.LazyProc, a uintptr) uintptr { r,_,_ := p.Call(a); return r }

func (d *DB) Exec(sql string) error {
    p, err := cString(sql); if err != nil { return err }
    rc, _, _ := pExec.Call(d.ptr, uintptr(unsafe.Pointer(p)), 0, 0, 0)
    if int(rc) != sqliteOK { return d.err() }
    return nil
}

func (d *DB) Prepare(sql string) (*Stmt, error) {
    p, err := cString(sql); if err != nil { return nil, err }
    var stmt uintptr
    rc, _, _ := pPrepareV2.Call(d.ptr, uintptr(unsafe.Pointer(p)), ^uintptr(0), uintptr(unsafe.Pointer(&stmt)), 0)
    if int(rc) != sqliteOK || stmt == 0 { return nil, d.err() }
    return &Stmt{db:d, ptr:stmt}, nil
}

func (s *Stmt) Finalize() { if s != nil && s.ptr != 0 { pFinalize.Call(s.ptr); s.ptr = 0; s.keep = nil } }
func (s *Stmt) BindText(i int, value string) error {
    b := append([]byte(value), 0); s.keep = append(s.keep, b)
    rc,_,_ := pBindText.Call(s.ptr, uintptr(i), uintptr(unsafe.Pointer(&b[0])), uintptr(len(value)), ^uintptr(0))
    if int(rc) != sqliteOK { return s.db.err() }; return nil
}
func (s *Stmt) BindInt64(i int, value int64) error {
    rc,_,_ := pBindInt64.Call(s.ptr, uintptr(i), uintptr(value))
    if int(rc) != sqliteOK { return s.db.err() }; return nil
}
func (s *Stmt) Step() (int,error) { rc,_,_ := pStep.Call(s.ptr); n:=int(rc); if n!=sqliteRow && n!=sqliteDone { return n,s.db.err() }; return n,nil }
func (s *Stmt) ColText(i int) string { r,_,_ := pColumnText.Call(s.ptr, uintptr(i)); return goString(r) }
func (s *Stmt) ColInt64(i int) int64 { r,_,_ := pColumnInt64.Call(s.ptr, uintptr(i)); return int64(r) }
func (s *Stmt) ColCount() int { r,_,_ := pColumnCount.Call(s.ptr); return int(r) }
func (d *DB) LastInsertID() int64 { r,_,_ := pLastInsertRowID.Call(d.ptr); return int64(r) }
func (d *DB) Changes() int64 { r,_,_ := pChanges.Call(d.ptr); return int64(r) }

func execBind(d *DB, sql string, args ...any) error {
    s, err := d.Prepare(sql); if err != nil { return err }; defer s.Finalize()
    for idx,a := range args {
        switch v := a.(type) {
        case string: err = s.BindText(idx+1,v)
        case int: err = s.BindInt64(idx+1,int64(v))
        case int64: err = s.BindInt64(idx+1,v)
        default: err = fmt.Errorf("tipo de bind não suportado: %T", a)
        }
        if err != nil { return err }
    }
    rc, err := s.Step(); if err != nil { return err }; if rc != sqliteDone { return fmt.Errorf("SQLite esperava DONE") }
    return nil
}

func queryRows(d *DB, sql string, args ...any) ([][]any,error) {
    s, err := d.Prepare(sql); if err != nil { return nil,err }; defer s.Finalize()
    for idx,a := range args {
        switch v := a.(type) { case string: err=s.BindText(idx+1,v); case int: err=s.BindInt64(idx+1,int64(v)); case int64: err=s.BindInt64(idx+1,v); default: err=fmt.Errorf("tipo bind") }
        if err != nil { return nil,err }
    }
    var out [][]any
    for { rc,err:=s.Step(); if err!=nil{return nil,err}; if rc==sqliteDone{break}; row:=make([]any,s.ColCount()); for i:=range row { row[i]=s.ColText(i) }; out=append(out,row) }
    return out,nil
}
