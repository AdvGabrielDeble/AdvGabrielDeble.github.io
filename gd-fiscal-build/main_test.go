package main

import (
    "bytes"
    "encoding/csv"
    "testing"
)

func TestGoldJulyImportAudit(t *testing.T) {
    raw, err := syntheticJulyXLSX()
    if err != nil { t.Fatal(err) }
    p, err := parseWorkbookPreview("TABELA-ESTRUTURA-JULHO.xlsx", raw)
    if err != nil { t.Fatal(err) }
    if got := len(p.Valid); got != 26 { t.Fatalf("validos: got %d want 26", got) }
    if got := len(p.Issues); got != 4 { t.Fatalf("incompletos: got %d want 4", got) }
    var total int64
    for _, r := range p.Valid { total += r.AmountCents }
    if total != 1_087_000 { t.Fatalf("total cents: got %d want 1087000", total) }
}

func TestPersistenceIsolationAndExportContract(t *testing.T) {
    raw, err := syntheticJulyXLSX()
    if err != nil { t.Fatal(err) }
    preview, err := parseWorkbookPreview("julho.xlsx", raw)
    if err != nil { t.Fatal(err) }

    dir := t.TempDir()
    a, err := newApp(dir)
    if err != nil { t.Fatal(err) }
    idA, err := a.upsertProfessional(0, "Profissional A", "52998224725", "PSICOLOGO", "07/11111", true)
    if err != nil { t.Fatal(err) }
    idB, err := a.upsertProfessional(0, "Profissional B", "16899535009", "PSICOLOGO", "07/22222", true)
    if err != nil { t.Fatal(err) }

    a.pending = &preview
    if n, _, err := a.confirmPreview(idA); err != nil || n != 26 { t.Fatalf("import A: n=%d err=%v", n, err) }
    a.pending = &preview
    if n, _, err := a.confirmPreview(idB); err != nil || n != 26 { t.Fatalf("import B: n=%d err=%v", n, err) }

    a.mu.Lock()
    for i := range a.store.Payments {
        if a.store.Payments[i].ProfessionalID == idA { a.store.Payments[i].Status = statusReceita }
    }
    if err := a.saveLocked(); err != nil { a.mu.Unlock(); t.Fatal(err) }
    a.mu.Unlock()

    reopened, err := newApp(dir)
    if err != nil { t.Fatal(err) }
    if len(reopened.store.Professionals) != 2 { t.Fatalf("profissionais apos restart: %d", len(reopened.store.Professionals)) }
    if len(reopened.store.Payments) != 52 { t.Fatalf("lancamentos apos restart: %d", len(reopened.store.Payments)) }

    profA, ok := reopened.professionalByID(idA)
    if !ok { t.Fatal("profissional A ausente") }
    out, count, err := buildExport(profA, reopened.store.Payments, 2026, 7)
    if err != nil { t.Fatal(err) }
    if count != 26 { t.Fatalf("export count: %d", count) }

    rd := csv.NewReader(bytes.NewReader(out)); rd.Comma = ';'
    rows, err := rd.ReadAll(); if err != nil { t.Fatal(err) }
    if len(rows) != 26 { t.Fatalf("csv rows: %d", len(rows)) }
    for i, row := range rows {
        if len(row) != 16 { t.Fatalf("row %d fields=%d", i, len(row)) }
        if row[1] != "R01.001.001" { t.Fatalf("row %d code=%s", i, row[1]) }
        if row[14] != "52998224725" { t.Fatalf("row %d profissional misturado=%s", i, row[14]) }
    }
}

func TestPackagedSelfAuditLogic(t *testing.T) {
    if err := runSelfTest(); err != nil { t.Fatal(err) }
}
