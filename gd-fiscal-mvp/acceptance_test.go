//go:build windows

package main

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func xmlEsc(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;", "'", "&apos;")
	return r.Replace(s)
}

func colName(n int) string {
	s := ""
	for n > 0 {
		n--
		s = string(rune('A'+n%26)) + s
		n /= 26
	}
	return s
}

type fixtureCell struct {
	row, col int
	text     string
	num      *float64
}

func fnum(v float64) *float64 { return &v }

func makeXLSX(cells []fixtureCell) []byte {
	rows := map[int][]fixtureCell{}
	max := 0
	for _, c := range cells {
		rows[c.row] = append(rows[c.row], c)
		if c.row > max {
			max = c.row
		}
	}
	var sheet strings.Builder
	sheet.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	for r := 1; r <= max; r++ {
		cs := rows[r]
		if len(cs) == 0 {
			continue
		}
		fmt.Fprintf(&sheet, `<row r="%d">`, r)
		for _, c := range cs {
			ref := fmt.Sprintf("%s%d", colName(c.col), r)
			if c.num != nil {
				fmt.Fprintf(&sheet, `<c r="%s"><v>%s</v></c>`, ref, strconv.FormatFloat(*c.num, 'f', -1, 64))
			} else {
				fmt.Fprintf(&sheet, `<c r="%s" t="inlineStr"><is><t>%s</t></is></c>`, ref, xmlEsc(c.text))
			}
		}
		sheet.WriteString(`</row>`)
	}
	sheet.WriteString(`</sheetData></worksheet>`)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, content string) {
		w, _ := zw.Create(name)
		_, _ = io.WriteString(w, content)
	}
	add("xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Plan1" sheetId="1" r:id="rId1"/></sheets></workbook>`)
	add("xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`)
	add("xl/worksheets/sheet1.xml", sheet.String())
	_ = zw.Close()
	return buf.Bytes()
}

func cpfSeed(seed int) string {
	base := fmt.Sprintf("%09d", 100000000+seed)
	digits := make([]int, 0, 11)
	for _, r := range base {
		digits = append(digits, int(r-'0'))
	}
	total := 0
	for i := 0; i < 9; i++ {
		total += digits[i] * (10 - i)
	}
	d := 11 - total%11
	if d >= 10 {
		d = 0
	}
	digits = append(digits, d)
	total = 0
	for i := 0; i < 10; i++ {
		total += digits[i] * (11 - i)
	}
	d = 11 - total%11
	if d >= 10 {
		d = 0
	}
	digits = append(digits, d)
	var b strings.Builder
	for _, x := range digits {
		b.WriteByte(byte('0' + x))
	}
	return b.String()
}

func julyFixture(invalidExtra bool) []byte {
	cells := []fixtureCell{{1, 1, "Carnê Leão - Livro Caixa - 2026", nil}, {83, 1, "JULHO", nil}, {84, 1, "Nome", nil}, {84, 7, "CPF - Responsável", nil}, {84, 10, "CPF e Nome - Paciente - Se necessário", nil}, {84, 16, "Data", nil}, {84, 18, "Valor", nil}}
	for i := 0; i < 26; i++ {
		row := 85 + i
		amount := 400.0
		if i == 25 {
			amount = 870
		}
		cells = append(cells, fixtureCell{row, 1, fmt.Sprintf("Pagador Julho %d", i+1), nil}, fixtureCell{row, 7, cpfSeed(i+1), nil}, fixtureCell{row, 16, "", fnum(float64(i + 1))}, fixtureCell{row, 18, "", fnum(amount)})
	}
	names := []string{"Henrique", "Elisandra", "Pâmela", "Maria Fernanda"}
	for i, n := range names {
		row := 111 + i
		cells = append(cells, fixtureCell{row, 1, n, nil}, fixtureCell{row, 7, cpfSeed(40+i), nil})
	}
	// O movimento inválido precisa estar antes da linha de totalização; linhas
	// posteriores ao total pertencem fora do bloco e não devem ser importadas.
	if invalidExtra {
		cells = append(cells, fixtureCell{115, 1, "Paciente CPF Inválido", nil}, fixtureCell{115, 7, "12345678901", nil}, fixtureCell{115, 16, "", fnum(15)}, fixtureCell{115, 18, "", fnum(250)})
		cells = append(cells, fixtureCell{116, 1, "Valor Total Mês", nil}, fixtureCell{116, 18, "", fnum(10870)})
	} else {
		cells = append(cells, fixtureCell{115, 1, "Valor Total Mês", nil}, fixtureCell{115, 18, "", fnum(10870)})
	}
	return makeXLSX(cells)
}

func TestMVPFinalGate(t *testing.T) {
	p, err := PreviewXLSX(julyFixture(false))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Valid) != 26 || len(p.Skipped) != 4 || len(p.Restricted) != 0 {
		t.Fatalf("preview inesperada: valid=%d skipped=%d restricted=%d", len(p.Valid), len(p.Skipped), len(p.Restricted))
	}
	var sum int64
	for _, r := range p.Valid {
		sum += r.AmountCents
		if r.Month != 7 || r.Year != 2026 {
			t.Fatalf("competência inválida: %02d/%d", r.Month, r.Year)
		}
	}
	if sum != 1087000 {
		t.Fatalf("total=%d", sum)
	}

	bad, err := PreviewXLSX(julyFixture(true))
	if err != nil {
		t.Fatal(err)
	}
	if len(bad.Restricted) != 1 || bad.Restricted[0].Code != "CPF_INVALIDO" {
		t.Fatalf("CPF inválido não virou pendência: %+v", bad.Restricted)
	}

	root := t.TempDir()
	s, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.AddProfessional("Profissional A", "52998224725", "PSICOLOGO", "07/54321", "RS")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	pros, err := s.ListProfessionals()
	if err != nil || len(pros) != 1 || pros[0].ID != a {
		t.Fatalf("profissional não persistiu: %v %+v", err, pros)
	}
	created, dups, res, err := s.ImportPreview(a, "julho-a.xlsx", p)
	if err != nil {
		t.Fatal(err)
	}
	if created != 26 || dups != 0 || res != 0 {
		t.Fatalf("import A: %d %d %d", created, dups, res)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	count, total, err := s.CountAndSum(a)
	if err != nil || count != 26 || total != 1087000 {
		t.Fatalf("persistência A count=%d total=%d err=%v", count, total, err)
	}

	b, err := s.AddProfessional("Profissional B", "16899535009", "PSICOLOGO", "07/65432", "RS")
	if err != nil {
		t.Fatal(err)
	}
	bp := ImportPreview{Valid: []ImportRow{{SourceRow: 2, Month: 7, Year: 2026, PayerName: "Paciente B", PayerCPF: "11144477735", BeneficiaryName: "Paciente B", BeneficiaryCPF: "11144477735", PaymentDate: time.Date(2026, 7, 20, 0, 0, 0, 0, time.Local), AmountCents: 33300, Description: "SERVIÇO DE SAÚDE"}}}
	created, _, _, err = s.ImportPreview(b, "julho-b.xlsx", bp)
	if err != nil || created != 1 {
		t.Fatalf("import B: %d %v", created, err)
	}
	ca, ta, _ := s.CountAndSum(a)
	cb, tb, _ := s.CountAndSum(b)
	if ca != 26 || ta != 1087000 || cb != 1 || tb != 33300 {
		t.Fatalf("isolamento falhou A=%d/%d B=%d/%d", ca, ta, cb, tb)
	}

	if n, err := s.ValidateMonth(a, 2026, 7); err != nil || n != 26 {
		t.Fatalf("validação A: %d %v", n, err)
	}
	if n, err := s.ValidateMonth(b, 2026, 7); err != nil || n != 1 {
		t.Fatalf("validação B: %d %v", n, err)
	}
	csvA, n, totalA, err := s.ExportProfessional(a, 2026, 7)
	if err != nil {
		t.Fatal(err)
	}
	if n != 26 || totalA != 1087000 {
		t.Fatalf("export A meta %d/%d", n, totalA)
	}
	cr := csv.NewReader(bytes.NewReader(csvA))
	cr.Comma = ';'
	records, err := cr.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 26 {
		t.Fatalf("csv A linhas=%d", len(records))
	}
	var csvTotal int64
	for _, r := range records {
		if len(r) != 16 {
			t.Fatalf("campos=%d", len(r))
		}
		if r[14] != "52998224725" {
			t.Fatalf("mistura profissional A: %s", r[14])
		}
		c, err := parseBRAmount(r[3])
		if err != nil {
			t.Fatal(err)
		}
		csvTotal += c
	}
	if csvTotal != 1087000 {
		t.Fatalf("csv total=%d", csvTotal)
	}
	csvB, n, totalB, err := s.ExportProfessional(b, 2026, 7)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || totalB != 33300 || bytes.Contains(csvB, []byte("52998224725")) {
		t.Fatalf("export B contaminado")
	}

	pbad := ImportPreview{Restricted: bad.Restricted}
	_, _, res, err = s.ImportPreview(a, "cpf-invalido.xlsx", pbad)
	if err != nil || res != 1 {
		t.Fatalf("pendência persistente %d %v", res, err)
	}
	rr, err := s.ListRestrictions(a)
	if err != nil || len(rr) != 1 || rr[0].PayerCPFRaw != "12345678901" {
		t.Fatalf("restrição não persistiu: %v %+v", err, rr)
	}

	dbFile := filepath.Join(root, "fiscal_saude.sqlite3")
	raw, err := os.ReadFile(dbFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 16 || string(raw[:16]) != "SQLite format 3\x00" {
		t.Fatalf("arquivo não é SQLite real")
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	pros, err = s.ListProfessionals()
	if err != nil || len(pros) != 2 {
		t.Fatalf("reabertura final profissionais=%d err=%v", len(pros), err)
	}
	ca, ta, _ = s.CountAndSum(a)
	cb, tb, _ = s.CountAndSum(b)
	if ca != 26 || ta != 1087000 || cb != 1 || tb != 33300 {
		t.Fatalf("reabertura final perdeu dados")
	}
}
