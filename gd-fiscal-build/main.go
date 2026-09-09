package main

import (
    "archive/zip"
    "bytes"
    "encoding/csv"
    "encoding/json"
    "encoding/xml"
    "errors"
    "fmt"
    "html"
    "io"
    "net"
    "net/http"
    "os"
    "os/exec"
    "path/filepath"
    "regexp"
    "runtime"
    "sort"
    "strconv"
    "strings"
    "sync"
    "time"
)

const (
    appTitle  = "GD Soluções — Fiscal Saúde"
    appVer    = "1.0.0-operacional"
    statusPending = "PENDENTE_REVISAO"
    statusReceita = "VALIDADO_RECEITA_SAUDE"
    statusNF      = "VALIDADO_NF"
)

var occupationCodes = map[string]string{
    "MEDICO": "225",
    "ODONTOLOGO_DENTISTA": "226",
    "FONOAUDIOLOGO": "230",
    "FISIOTERAPEUTA": "231",
    "TERAPEUTA_OCUPACIONAL": "232",
    "PSICOLOGO": "255",
}

var professionLabels = map[string]string{
    "MEDICO": "Médico",
    "ODONTOLOGO_DENTISTA": "Odontólogo/Dentista",
    "FONOAUDIOLOGO": "Fonoaudiólogo",
    "FISIOTERAPEUTA": "Fisioterapeuta",
    "TERAPEUTA_OCUPACIONAL": "Terapeuta Ocupacional",
    "PSICOLOGO": "Psicólogo",
}

type Professional struct {
    ID             int    `json:"id"`
    Name           string `json:"name"`
    CPF            string `json:"cpf"`
    ProfessionKey  string `json:"profession_key"`
    OccupationCode string `json:"occupation_code"`
    Registry       string `json:"registry"`
    Active         bool   `json:"active"`
}

type Payment struct {
    ID              int    `json:"id"`
    ProfessionalID  int    `json:"professional_id"`
    PayerName       string `json:"payer_name"`
    PayerCPF        string `json:"payer_cpf"`
    BeneficiaryName string `json:"beneficiary_name"`
    BeneficiaryCPF  string `json:"beneficiary_cpf"`
    PaymentDate     string `json:"payment_date"`
    AmountCents     int64  `json:"amount_cents"`
    Description     string `json:"description"`
    Status          string `json:"status"`
    SourceFile      string `json:"source_file"`
    CreatedAt       string `json:"created_at"`
}

type Store struct {
    Version            int            `json:"version"`
    NextProfessionalID int            `json:"next_professional_id"`
    NextPaymentID      int            `json:"next_payment_id"`
    Professionals      []Professional `json:"professionals"`
    Payments           []Payment      `json:"payments"`
}

type PreviewRecord struct {
    SourceRow       int
    PayerName       string
    PayerCPF        string
    BeneficiaryName string
    BeneficiaryCPF  string
    PaymentDate     string
    AmountCents     int64
    Month           int
    Year            int
}

type PreviewIssue struct {
    SourceRow int
    Name      string
    CPF       string
    Reason    string
    Month     int
    Year      int
}

type Preview struct {
    SourceFile string
    Valid      []PreviewRecord
    Issues     []PreviewIssue
}

type App struct {
    mu        sync.Mutex
    dataDir   string
    storePath string
    store     Store
    pending   *Preview
}

func defaultStore() Store {
    return Store{Version: 1, NextProfessionalID: 1, NextPaymentID: 1, Professionals: []Professional{}, Payments: []Payment{}}
}

func resolveDataDir() string {
    if override := strings.TrimSpace(os.Getenv("GD_FISCAL_DATA_DIR")); override != "" {
        return override
    }
    base := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
    if base == "" {
        if d, err := os.UserCacheDir(); err == nil {
            base = d
        }
    }
    if base == "" {
        base = "."
    }
    return filepath.Join(base, "GD Solucoes", "Fiscal Saude Core")
}

func newApp(dir string) (*App, error) {
    if err := os.MkdirAll(dir, 0o755); err != nil {
        return nil, err
    }
    a := &App{dataDir: dir, storePath: filepath.Join(dir, "gd_fiscal.json"), store: defaultStore()}
    if err := a.load(); err != nil {
        return nil, err
    }
    return a, nil
}

func (a *App) load() error {
    a.mu.Lock()
    defer a.mu.Unlock()
    s, err := loadStoreFile(a.storePath)
    if err != nil {
        return err
    }
    a.store = s
    return nil
}

func loadStoreFile(path string) (Store, error) {
    decode := func(p string) (Store, error) {
        raw, err := os.ReadFile(p)
        if err != nil {
            return Store{}, err
        }
        var s Store
        if err := json.Unmarshal(raw, &s); err != nil {
            return Store{}, err
        }
        normalizeStore(&s)
        return s, nil
    }
    s, err := decode(path)
    if err == nil {
        return s, nil
    }
    if errors.Is(err, os.ErrNotExist) {
        return defaultStore(), nil
    }
    backup := path + ".bak"
    if b, berr := decode(backup); berr == nil {
        return b, nil
    }
    return Store{}, fmt.Errorf("base local inválida: %w", err)
}

func normalizeStore(s *Store) {
    if s.Version == 0 {
        s.Version = 1
    }
    maxP, maxPay := 0, 0
    for i := range s.Professionals {
        if s.Professionals[i].ID > maxP { maxP = s.Professionals[i].ID }
    }
    for i := range s.Payments {
        if s.Payments[i].ID > maxPay { maxPay = s.Payments[i].ID }
    }
    if s.NextProfessionalID <= maxP { s.NextProfessionalID = maxP + 1 }
    if s.NextPaymentID <= maxPay { s.NextPaymentID = maxPay + 1 }
    if s.NextProfessionalID < 1 { s.NextProfessionalID = 1 }
    if s.NextPaymentID < 1 { s.NextPaymentID = 1 }
}

func saveStoreFile(path string, s Store) error {
    raw, err := json.MarshalIndent(s, "", "  ")
    if err != nil { return err }
    if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { return err }
    tmp := path + ".tmp"
    bak := path + ".bak"
    if err := os.WriteFile(tmp, raw, 0o600); err != nil { return err }
    _ = os.Remove(bak)
    if _, err := os.Stat(path); err == nil {
        if err := os.Rename(path, bak); err != nil {
            _ = os.Remove(tmp)
            return err
        }
    }
    if err := os.Rename(tmp, path); err != nil {
        if _, berr := os.Stat(bak); berr == nil { _ = os.Rename(bak, path) }
        return err
    }
    return nil
}

func (a *App) saveLocked() error { return saveStoreFile(a.storePath, a.store) }

func onlyDigits(s string) string {
    var b strings.Builder
    for _, r := range s {
        if r >= '0' && r <= '9' { b.WriteRune(r) }
    }
    return b.String()
}

func cpfValid(cpf string) bool {
    cpf = onlyDigits(cpf)
    if len(cpf) != 11 { return false }
    same := true
    for i := 1; i < 11; i++ { if cpf[i] != cpf[0] { same = false; break } }
    if same { return false }
    for length := 9; length <= 10; length++ {
        total := 0
        for i := 0; i < length; i++ { total += int(cpf[i]-'0') * (length + 1 - i) }
        digit := (total * 10) % 11
        if digit == 10 { digit = 0 }
        if digit != int(cpf[length]-'0') { return false }
    }
    return true
}

func normalizeCPF(raw string, numeric bool) (string, bool) {
    digits := onlyDigits(raw)
    if len(digits) == 11 && cpfValid(digits) { return digits, true }
    if numeric && len(digits) == 10 {
        c := "0" + digits
        if cpfValid(c) { return c, true }
    }
    return "", false
}

func normalizeText(s string) string {
    s = strings.ToLower(strings.TrimSpace(s))
    repl := strings.NewReplacer(
        "á","a","à","a","ã","a","â","a","ä","a",
        "é","e","è","e","ê","e","ë","e",
        "í","i","ì","i","î","i","ï","i",
        "ó","o","ò","o","õ","o","ô","o","ö","o",
        "ú","u","ù","u","û","u","ü","u",
        "ç","c","–","-","—","-",
    )
    return strings.Join(strings.Fields(repl.Replace(s)), " ")
}

var monthNames = map[string]int{
    "janeiro":1,"fevereiro":2,"marco":3,"abril":4,"maio":5,"junho":6,
    "julho":7,"agosto":8,"setembro":9,"outubro":10,"novembro":11,"dezembro":12,
}

func monthFromText(s string) int {
    n := normalizeText(s)
    for name, m := range monthNames { if strings.Contains(n, name) { return m } }
    return 0
}

var yearRE = regexp.MustCompile(`\b(20\d{2})\b`)
func yearFromText(s string) int {
    m := yearRE.FindStringSubmatch(s)
    if len(m) == 2 { y, _ := strconv.Atoi(m[1]); return y }
    return 0
}

type XCell struct { Text string; Numeric bool }
type XSheet struct { Name string; Rows [][]XCell }

type workbookXML struct { Sheets []workbookSheet `xml:"sheets>sheet"` }
type workbookSheet struct { Name string `xml:"name,attr"`; State string `xml:"state,attr"`; RID string `xml:"id,attr"` }
type relsXML struct { Relationships []relXML `xml:"Relationship"` }
type relXML struct { ID string `xml:"Id,attr"`; Target string `xml:"Target,attr"` }
type sharedXML struct { Items []sharedItem `xml:"si"` }
type sharedItem struct { T string `xml:"t"`; Runs []sharedRun `xml:"r"` }
type sharedRun struct { T string `xml:"t"` }
type worksheetXML struct { Rows []sheetRow `xml:"sheetData>row"` }
type sheetRow struct { R int `xml:"r,attr"`; Cells []sheetCell `xml:"c"` }
type sheetCell struct { Ref string `xml:"r,attr"`; Type string `xml:"t,attr"`; V string `xml:"v"`; Inline inlineString `xml:"is"` }
type inlineString struct { T string `xml:"t"`; Runs []sharedRun `xml:"r"` }

func readZipFile(files map[string]*zip.File, name string) ([]byte, error) {
    f := files[strings.TrimPrefix(filepath.ToSlash(name), "/")]
    if f == nil { return nil, fmt.Errorf("xlsx: arquivo interno ausente: %s", name) }
    rc, err := f.Open(); if err != nil { return nil, err }; defer rc.Close()
    return io.ReadAll(rc)
}

func parseXLSX(raw []byte) ([]XSheet, error) {
    zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw))); if err != nil { return nil, fmt.Errorf("xlsx inválido: %w", err) }
    files := map[string]*zip.File{}
    for _, f := range zr.File { files[filepath.ToSlash(f.Name)] = f }

    var wb workbookXML
    wbRaw, err := readZipFile(files, "xl/workbook.xml"); if err != nil { return nil, err }
    if err := xml.Unmarshal(wbRaw, &wb); err != nil { return nil, err }
    var rels relsXML
    relRaw, err := readZipFile(files, "xl/_rels/workbook.xml.rels"); if err != nil { return nil, err }
    if err := xml.Unmarshal(relRaw, &rels); err != nil { return nil, err }
    targets := map[string]string{}
    for _, r := range rels.Relationships {
        t := filepath.ToSlash(strings.TrimPrefix(r.Target, "/"))
        if !strings.HasPrefix(t, "xl/") { t = "xl/" + t }
        targets[r.ID] = t
    }
    shared := []string{}
    if _, ok := files["xl/sharedStrings.xml"]; ok {
        ssRaw, _ := readZipFile(files, "xl/sharedStrings.xml")
        var ss sharedXML
        if xml.Unmarshal(ssRaw, &ss) == nil {
            for _, it := range ss.Items {
                text := it.T
                if text == "" { for _, r := range it.Runs { text += r.T } }
                shared = append(shared, text)
            }
        }
    }
    sheets := []XSheet{}
    for _, meta := range wb.Sheets {
        if strings.EqualFold(meta.State, "hidden") || strings.EqualFold(meta.State, "veryHidden") { continue }
        target := targets[meta.RID]
        if target == "" { continue }
        data, err := readZipFile(files, target); if err != nil { return nil, err }
        var ws worksheetXML
        if err := xml.Unmarshal(data, &ws); err != nil { return nil, err }
        maxRow, maxCol := 0, 0
        for _, r := range ws.Rows {
            rr := r.R; if rr == 0 { rr = maxRow + 1 }
            if rr > maxRow { maxRow = rr }
            for _, c := range r.Cells { if col := cellColumn(c.Ref); col > maxCol { maxCol = col } }
        }
        matrix := make([][]XCell, maxRow)
        for i := range matrix { matrix[i] = make([]XCell, maxCol) }
        seqRow := 0
        for _, r := range ws.Rows {
            rowNum := r.R; if rowNum == 0 { seqRow++; rowNum = seqRow } else { seqRow = rowNum }
            for _, c := range r.Cells {
                col := cellColumn(c.Ref); if col < 1 || rowNum < 1 || rowNum > len(matrix) || col > maxCol { continue }
                txt, numeric := decodeSheetCell(c, shared)
                matrix[rowNum-1][col-1] = XCell{Text: txt, Numeric: numeric}
            }
        }
        sheets = append(sheets, XSheet{Name: meta.Name, Rows: matrix})
    }
    if len(sheets) == 0 { return nil, errors.New("xlsx sem planilhas visíveis") }
    return sheets, nil
}

func decodeSheetCell(c sheetCell, shared []string) (string, bool) {
    switch c.Type {
    case "s":
        idx, _ := strconv.Atoi(strings.TrimSpace(c.V)); if idx >= 0 && idx < len(shared) { return shared[idx], false }
        return "", false
    case "inlineStr":
        text := c.Inline.T; if text == "" { for _, r := range c.Inline.Runs { text += r.T } }; return text, false
    case "str", "e", "b", "d":
        return c.V, false
    default:
        return strings.TrimSpace(c.V), strings.TrimSpace(c.V) != ""
    }
}

func cellColumn(ref string) int {
    n := 0
    for _, r := range ref {
        if r >= 'A' && r <= 'Z' { n = n*26 + int(r-'A'+1) } else if r >= 'a' && r <= 'z' { n = n*26 + int(r-'a'+1) } else { break }
    }
    return n
}

func cellText(row []XCell, col int) string { if col < 0 || col >= len(row) { return "" }; return strings.TrimSpace(row[col].Text) }
func cellAt(row []XCell, col int) XCell { if col < 0 || col >= len(row) { return XCell{} }; return row[col] }

func parseWorkbookPreview(fileName string, raw []byte) (Preview, error) {
    sheets, err := parseXLSX(raw); if err != nil { return Preview{}, err }
    out := Preview{SourceFile: fileName}
    for _, s := range sheets { parseSheetBlocks(s, &out) }
    sort.Slice(out.Valid, func(i,j int) bool { if out.Valid[i].PaymentDate == out.Valid[j].PaymentDate { return out.Valid[i].SourceRow < out.Valid[j].SourceRow }; return out.Valid[i].PaymentDate < out.Valid[j].PaymentDate })
    return out, nil
}

type block struct { headerRow, startCol, endCol, nameCol, payerCol, beneficiaryCol, dateCol, amountCol, month, year int }

func parseSheetBlocks(s XSheet, out *Preview) {
    globalYear := yearFromText(s.Name)
    if globalYear == 0 {
        limit := len(s.Rows); if limit > 20 { limit = 20 }
        for r := 0; r < limit && globalYear == 0; r++ { for _, c := range s.Rows[r] { if y := yearFromText(c.Text); y != 0 { globalYear = y; break } } }
    }
    blocks := detectBlocks(s.Rows, globalYear, s.Name)
    for _, b := range blocks {
        for r := b.headerRow + 1; r < len(s.Rows); r++ {
            row := s.Rows[r]
            if repeatedHeader(row, b) || totalRow(row, b) { break }
            if !segmentHasData(row, b.startCol, b.endCol) { continue }
            payerName := cellText(row, b.nameCol)
            payerCell := cellAt(row, b.payerCol)
            payerCPF, ok := normalizeCPF(payerCell.Text, payerCell.Numeric)
            if payerName == "" { _, inferred := extractCPFAndName(payerCell.Text, payerCell.Numeric); payerName = inferred }
            benName, benCPF := payerName, payerCPF
            benRaw := cellAt(row, b.beneficiaryCol)
            if strings.TrimSpace(benRaw.Text) != "" {
                c, n := extractCPFAndName(benRaw.Text, benRaw.Numeric)
                if c != "" { benCPF = c }
                if n != "" { benName = n }
            }
            dateISO, dateOK := parseDateCell(cellAt(row, b.dateCol), b.month, b.year)
            cents, amountOK := parseAmountCell(cellAt(row, b.amountCol))
            reason := []string{}
            if payerName == "" { reason = append(reason, "nome do responsável ausente") }
            if !ok { reason = append(reason, "CPF do responsável ausente ou inválido") }
            if benCPF == "" || !cpfValid(benCPF) { reason = append(reason, "CPF do paciente/beneficiário ausente ou inválido") }
            if !dateOK { reason = append(reason, "data ausente ou inválida") }
            if !amountOK { reason = append(reason, "valor ausente ou inválido") }
            if len(reason) > 0 {
                out.Issues = append(out.Issues, PreviewIssue{SourceRow:r+1, Name:payerName, CPF:payerCPF, Reason:strings.Join(reason, "; "), Month:b.month, Year:b.year})
                continue
            }
            out.Valid = append(out.Valid, PreviewRecord{SourceRow:r+1, PayerName:payerName, PayerCPF:payerCPF, BeneficiaryName:benName, BeneficiaryCPF:benCPF, PaymentDate:dateISO, AmountCents:cents, Month:b.month, Year:b.year})
        }
    }
}

func detectBlocks(rows [][]XCell, globalYear int, sheetName string) []block {
    out := []block{}
    for r, row := range rows {
        payers := []int{}
        for c, cell := range row {
            n := normalizeText(cell.Text)
            if strings.Contains(n,"cpf") && (strings.Contains(n,"responsavel") || strings.Contains(n,"pagador")) { payers = append(payers,c) }
        }
        for pos, payerCol := range payers {
            next := len(row); if pos+1 < len(payers) { next = payers[pos+1] }
            nameCol := -1
            for c := payerCol-1; c >= 0 && c >= payerCol-10; c-- { n:=normalizeText(cellText(row,c)); if n=="nome" || (strings.Contains(n,"nome")&&(strings.Contains(n,"responsavel")||strings.Contains(n,"pagador"))) { nameCol=c; break } }
            start := payerCol-10; if start<0 { start=0 }; if nameCol>=0 { start=nameCol }
            end := next
            benCol,dateCol,amountCol := -1,-1,-1
            for c:=start; c<end && c<len(row); c++ {
                n:=normalizeText(cellText(row,c))
                if benCol<0 && (strings.Contains(n,"paciente")||strings.Contains(n,"beneficiario")) { benCol=c }
                if dateCol<0 && strings.Contains(n,"data") && !strings.Contains(n,"nascimento") { dateCol=c }
                if amountCol<0 && strings.Contains(n,"valor") && !strings.Contains(n,"total") { amountCol=c }
            }
            if nameCol<0 || dateCol<0 || amountCol<0 { continue }
            month, year := 0, globalYear
            for rr:=r-1; rr>=0 && rr>=r-5 && month==0; rr-- {
                for c:=start; c<end && c<len(rows[rr]); c++ { if m:=monthFromText(rows[rr][c].Text); m!=0 { month=m; if y:=yearFromText(rows[rr][c].Text); y!=0 { year=y }; break } }
            }
            if month==0 { month=monthFromText(sheetName) }
            if year==0 { year=yearFromText(sheetName) }
            if month==0 || year==0 { continue }
            out=append(out, block{headerRow:r,startCol:start,endCol:end,nameCol:nameCol,payerCol:payerCol,beneficiaryCol:benCol,dateCol:dateCol,amountCol:amountCol,month:month,year:year})
        }
    }
    return out
}

func segmentHasData(row []XCell, start,end int) bool { if end>len(row){end=len(row)}; for c:=start;c<end;c++{if strings.TrimSpace(row[c].Text)!=""{return true}}; return false }
func repeatedHeader(row []XCell,b block) bool { n:=normalizeText(cellText(row,b.payerCol)); return strings.Contains(n,"cpf")&&(strings.Contains(n,"responsavel")||strings.Contains(n,"pagador")) }
func totalRow(row []XCell,b block) bool { end:=b.endCol;if end>len(row){end=len(row)};for c:=b.startCol;c<end;c++{n:=normalizeText(row[c].Text);if strings.Contains(n,"valor total mes")||strings.Contains(n,"total do mes"){return true}};return false }

func extractCPFAndName(raw string, numeric bool) (string,string) {
    cpf, ok := normalizeCPF(raw,numeric)
    if !ok {
        re:=regexp.MustCompile(`(?:\d[.\-\s]*){10}\d`)
        if m:=re.FindString(raw);m!=""{cpf,_=normalizeCPF(m,false)}
    }
    name:=strings.TrimSpace(raw)
    if cpf!="" {
        digitsPattern:=regexp.MustCompile(`(?:\d[.\-\s]*){10}\d`)
        name=strings.Trim(strings.TrimSpace(digitsPattern.ReplaceAllString(name," ")),"-–—;,:/ ")
    }
    if numeric { name="" }
    return cpf,name
}

func parseDateCell(c XCell, month,year int) (string,bool) {
    raw:=strings.TrimSpace(c.Text);if raw==""||month==0||year==0{return "",false}
    if c.Numeric {
        f,err:=strconv.ParseFloat(raw,64);if err==nil {
            day:=int(f)
            if f==float64(day)&&day>=1&&day<=31 { d:=time.Date(year,time.Month(month),day,0,0,0,0,time.Local);if int(d.Month())==month&&d.Day()==day{return d.Format("2006-01-02"),true} }
            if f>1000 { whole:=int(f);d:=time.Date(1899,12,30,0,0,0,0,time.UTC).AddDate(0,0,whole);return d.Format("2006-01-02"),true }
        }
    }
    for _,layout:=range []string{"2006-01-02","02/01/2006","2/1/2006","02-01-2006"}{if d,err:=time.Parse(layout,raw);err==nil{return d.Format("2006-01-02"),true}}
    if d,err:=strconv.Atoi(raw);err==nil&&d>=1&&d<=31{dt:=time.Date(year,time.Month(month),d,0,0,0,0,time.Local);if int(dt.Month())==month{return dt.Format("2006-01-02"),true}}
    return "",false
}

func parseAmountCell(c XCell)(int64,bool){raw:=strings.TrimSpace(strings.ReplaceAll(c.Text,"R$",""));raw=strings.ReplaceAll(raw," ","");if raw==""{return 0,false};if strings.Contains(raw,","){raw=strings.ReplaceAll(raw,".","");raw=strings.ReplaceAll(raw,",",".")};f,err:=strconv.ParseFloat(raw,64);if err!=nil||f<=0{return 0,false};return int64(f*100+0.5),true}

func defaultDescription(p Professional) string {
    switch p.ProfessionKey {case "PSICOLOGO":return "Atendimento psicológico";case "MEDICO":return "Atendimento médico";case "ODONTOLOGO_DENTISTA":return "Atendimento odontológico";case "FONOAUDIOLOGO":return "Atendimento fonoaudiológico";case "FISIOTERAPEUTA":return "Atendimento fisioterapêutico";case "TERAPEUTA_OCUPACIONAL":return "Atendimento de terapia ocupacional"};return "Atendimento profissional de saúde"
}

func (a *App) professionalByID(id int)(Professional,bool){for _,p:=range a.store.Professionals{if p.ID==id{return p,true}};return Professional{},false}
func (a *App) duplicatePayment(profID int,r PreviewRecord)bool{for _,p:=range a.store.Payments{if p.ProfessionalID==profID&&p.PayerCPF==r.PayerCPF&&p.BeneficiaryCPF==r.BeneficiaryCPF&&p.PaymentDate==r.PaymentDate&&p.AmountCents==r.AmountCents{return true}};return false}

func (a *App) confirmPreview(profID int) (int,int,error) {
    a.mu.Lock();defer a.mu.Unlock();if a.pending==nil{return 0,0,errors.New("não há prévia ativa")};prof,ok:=a.professionalByID(profID);if !ok{return 0,0,errors.New("profissional não encontrado")}
    created,skipped:=0,0
    for _,r:=range a.pending.Valid{if a.duplicatePayment(profID,r){skipped++;continue};a.store.Payments=append(a.store.Payments,Payment{ID:a.store.NextPaymentID,ProfessionalID:profID,PayerName:r.PayerName,PayerCPF:r.PayerCPF,BeneficiaryName:r.BeneficiaryName,BeneficiaryCPF:r.BeneficiaryCPF,PaymentDate:r.PaymentDate,AmountCents:r.AmountCents,Description:defaultDescription(prof),Status:statusPending,SourceFile:a.pending.SourceFile,CreatedAt:time.Now().Format(time.RFC3339)});a.store.NextPaymentID++;created++}
    if err:=a.saveLocked();err!=nil{return 0,0,err};a.pending=nil;return created,skipped,nil
}

func (a *App) upsertProfessional(id int,name,cpf,profession,registry string,active bool)(int,error){name=strings.TrimSpace(name);if name==""{return 0,errors.New("nome do profissional é obrigatório")};cpf=onlyDigits(cpf);if !cpfValid(cpf){return 0,errors.New("CPF do profissional inválido")};code,ok:=occupationCodes[profession];if !ok{return 0,errors.New("profissão inválida")};registry=strings.TrimSpace(registry);if strings.ContainsAny(registry,";\r\n\""){return 0,errors.New("registro profissional contém caractere incompatível")};if len(registry)>15{return 0,errors.New("registro profissional excede 15 caracteres")}
    a.mu.Lock();defer a.mu.Unlock();for _,p:=range a.store.Professionals{if p.CPF==cpf&&p.ID!=id{return 0,errors.New("já existe profissional com este CPF")}}
    if id==0{id=a.store.NextProfessionalID;a.store.NextProfessionalID++;a.store.Professionals=append(a.store.Professionals,Professional{ID:id,Name:name,CPF:cpf,ProfessionKey:profession,OccupationCode:code,Registry:registry,Active:true})}else{found:=false;for i:=range a.store.Professionals{if a.store.Professionals[i].ID==id{a.store.Professionals[i].Name=name;a.store.Professionals[i].CPF=cpf;a.store.Professionals[i].ProfessionKey=profession;a.store.Professionals[i].OccupationCode=code;a.store.Professionals[i].Registry=registry;a.store.Professionals[i].Active=active;found=true;break}};if !found{return 0,errors.New("profissional não encontrado")}}
    if err:=a.saveLocked();err!=nil{return 0,err};return id,nil
}

func receitaRow(prof Professional,p Payment)([]string,error){if !cpfValid(prof.CPF)||!cpfValid(p.PayerCPF)||!cpfValid(p.BeneficiaryCPF){return nil,errors.New("CPF inválido na escrituração")};if p.Status!=statusReceita{return nil,errors.New("lançamento não validado para Receita Saúde")};if strings.TrimSpace(p.Description)==""||strings.ContainsAny(p.Description,";\r\n\""){return nil,errors.New("descrição fiscal inválida")};d,err:=time.Parse("2006-01-02",p.PaymentDate);if err!=nil{return nil,err};amount:=fmt.Sprintf("%.2f",float64(p.AmountCents)/100);amount=strings.ReplaceAll(amount,".",",");return []string{d.Format("02/01/2006"),"R01.001.001",prof.OccupationCode,amount,"",p.Description,"PF",p.PayerCPF,p.BeneficiaryCPF,"","","","","S",prof.CPF,prof.Registry},nil}

func buildExport(prof Professional,payments []Payment,year,month int)([]byte,int,error){var buf bytes.Buffer;w:=csv.NewWriter(&buf);w.Comma=';';w.UseCRLF=true;count:=0;for _,p:=range payments{if p.ProfessionalID!=prof.ID||p.Status!=statusReceita{continue};d,err:=time.Parse("2006-01-02",p.PaymentDate);if err!=nil{continue};if d.Year()!=year||int(d.Month())!=month{continue};row,err:=receitaRow(prof,p);if err!=nil{return nil,0,err};if len(row)!=16{return nil,0,errors.New("linha de exportação fora do contrato de 16 campos")};if err:=w.Write(row);err!=nil{return nil,0,err};count++};w.Flush();if err:=w.Error();err!=nil{return nil,0,err};if count==0{return nil,0,errors.New("não há lançamentos validados para este profissional/competência")};return buf.Bytes(),count,nil}

func brMoney(c int64)string{return fmt.Sprintf("R$ %.2f",float64(c)/100)}
func esc(s string)string{return html.EscapeString(s)}
func intParam(r *http.Request,name string)int{v,_:=strconv.Atoi(r.FormValue(name));return v}

func page(title,body string)string{return `<!doctype html><html lang="pt-br"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>`+esc(title)+`</title><style>
:root{--g:#0D4E3A;--gold:#B88B30;--bg:#f5f7f6;--line:#dfe6e2;--txt:#16251f}. *{box-sizing:border-box}body{margin:0;font:15px system-ui,-apple-system,Segoe UI,Arial;background:var(--bg);color:var(--txt)}header{background:var(--g);color:white;padding:18px 28px;display:flex;align-items:center;justify-content:space-between}header b{font-size:20px}nav a{color:white;text-decoration:none;margin-left:18px}main{max-width:1180px;margin:28px auto;padding:0 18px}.card{background:white;border:1px solid var(--line);border-radius:12px;padding:20px;margin:0 0 18px;box-shadow:0 2px 9px #0000000a}h1,h2{margin-top:0}h1{font-size:26px}h2{font-size:19px;color:var(--g)}label{font-weight:650;display:block;margin:9px 0 5px}input,select{width:100%;padding:10px;border:1px solid #cdd8d2;border-radius:8px;background:white}button,.btn{display:inline-block;border:0;background:var(--g);color:white;padding:10px 15px;border-radius:8px;text-decoration:none;font-weight:650;cursor:pointer}.btn.alt{background:var(--gold)}table{width:100%;border-collapse:collapse}th,td{padding:9px 8px;border-bottom:1px solid var(--line);text-align:left;vertical-align:top}th{font-size:12px;text-transform:uppercase;color:#53665e}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:12px}.ok{background:#edf8f2;border-left:4px solid var(--g);padding:12px}.warn{background:#fff7e8;border-left:4px solid var(--gold);padding:12px}.bad{background:#fff0f0;border-left:4px solid #a33;padding:12px}.num{font-variant-numeric:tabular-nums}.small{font-size:12px;color:#65766e}.pill{padding:3px 8px;border-radius:999px;background:#edf2ef;font-size:12px;white-space:nowrap}.actions{display:flex;gap:10px;flex-wrap:wrap;align-items:end}.scroll{overflow:auto}</style></head><body><header><b>GD Soluções — Fiscal Saúde</b><nav><a href="/">Início</a><a href="/professionals">Profissionais</a><a href="/import">Importar Excel</a><a href="/payments">Lançamentos</a><a href="/export">Exportar</a></nav></header><main>`+body+`</main></body></html>`}

func writeHTML(w http.ResponseWriter,title,body string){w.Header().Set("Content-Type","text/html; charset=utf-8");_,_=io.WriteString(w,page(title,body))}
func redirectMsg(w http.ResponseWriter,r *http.Request,path,msg string){http.Redirect(w,r,path+"?msg="+urlQueryEscape(msg),http.StatusSeeOther)}
func urlQueryEscape(s string)string{repl:=strings.NewReplacer("%","%25"," ","%20","?","%3F","&","%26","=","%3D","#","%23");return repl.Replace(s)}
func messageBox(r *http.Request)string{m:=r.URL.Query().Get("msg");if m==""{return ""};return `<div class="ok">`+esc(m)+`</div><br>`}

func (a *App) routes() http.Handler {
    mux:=http.NewServeMux()
    mux.HandleFunc("/healthz",func(w http.ResponseWriter,r *http.Request){w.Header().Set("Content-Type","text/plain");_,_=io.WriteString(w,"ok")})
    mux.HandleFunc("/",a.handleHome)
    mux.HandleFunc("/professionals",a.handleProfessionals)
    mux.HandleFunc("/professionals/save",a.handleProfessionalSave)
    mux.HandleFunc("/import",a.handleImport)
    mux.HandleFunc("/import/preview",a.handleImportPreview)
    mux.HandleFunc("/import/confirm",a.handleImportConfirm)
    mux.HandleFunc("/payments",a.handlePayments)
    mux.HandleFunc("/payments/save",a.handlePaymentsSave)
    mux.HandleFunc("/export",a.handleExport)
    mux.HandleFunc("/export/file",a.handleExportFile)
    return mux
}

func (a *App) handleHome(w http.ResponseWriter,r *http.Request){if r.URL.Path!="/"{http.NotFound(w,r);return};a.mu.Lock();pc,lc:=len(a.store.Professionals),len(a.store.Payments);validated:=0;for _,p:=range a.store.Payments{if p.Status==statusReceita{validated++}};a.mu.Unlock();body:=messageBox(r)+`<div class="card"><h1>Escrituração fiscal local</h1><p>Fluxo operacional: cadastre o profissional, importe a planilha Excel, revise/classifique os lançamentos e exporte a escrituração por profissional e competência.</p><div class="grid"><div><b>Profissionais</b><div style="font-size:30px">`+strconv.Itoa(pc)+`</div></div><div><b>Lançamentos salvos</b><div style="font-size:30px">`+strconv.Itoa(lc)+`</div></div><div><b>Validados Receita Saúde</b><div style="font-size:30px">`+strconv.Itoa(validated)+`</div></div></div></div><div class="card"><div class="actions"><a class="btn" href="/professionals">Adicionar profissional</a><a class="btn alt" href="/import">Importar Excel</a><a class="btn" href="/export">Exportar escrituração</a></div><p class="small">Dados salvos localmente. Versão `+appVer+`.</p></div>`;writeHTML(w,"Início",body)}

func (a *App) handleProfessionals(w http.ResponseWriter,r *http.Request){a.mu.Lock();ps:=append([]Professional(nil),a.store.Professionals...);a.mu.Unlock();sort.Slice(ps,func(i,j int)bool{return ps[i].Name<ps[j].Name});var rows strings.Builder;for _,p:=range ps{rows.WriteString(`<tr><td>`+esc(p.Name)+`</td><td class="num">`+esc(p.CPF)+`</td><td>`+esc(professionLabels[p.ProfessionKey])+`</td><td>`+esc(p.Registry)+`</td><td>`);if p.Active{rows.WriteString(`<span class="pill">Ativo</span>`)}else{rows.WriteString(`<span class="pill">Inativo</span>`)};rows.WriteString(`</td><td><a href="/professionals?id=`+strconv.Itoa(p.ID)+`">Editar</a></td></tr>`)}
    edit:=Professional{Active:true};if id:=intParam(r,"id");id>0{for _,p:=range ps{if p.ID==id{edit=p;break}}};opts:="";keys:=[]string{"PSICOLOGO","MEDICO","ODONTOLOGO_DENTISTA","FONOAUDIOLOGO","FISIOTERAPEUTA","TERAPEUTA_OCUPACIONAL"};for _,k:=range keys{sel:="";if edit.ProfessionKey==k{sel=" selected"};opts+=`<option value="`+k+`"`+sel+`>`+esc(professionLabels[k])+`</option>`};checked:="";if edit.Active{checked=" checked"};body:=messageBox(r)+`<div class="card"><h1>Profissionais</h1><div class="scroll"><table><thead><tr><th>Nome</th><th>CPF</th><th>Profissão</th><th>Registro</th><th>Status</th><th></th></tr></thead><tbody>`+rows.String()+`</tbody></table></div></div><div class="card"><h2>`+map[bool]string{true:"Editar profissional",false:"Adicionar profissional"}[edit.ID>0]+`</h2><form method="post" action="/professionals/save"><input type="hidden" name="id" value="`+strconv.Itoa(edit.ID)+`"><div class="grid"><div><label>Nome</label><input name="name" required value="`+esc(edit.Name)+`"></div><div><label>CPF</label><input name="cpf" required value="`+esc(edit.CPF)+`"></div><div><label>Profissão</label><select name="profession" required><option value="">Selecione</option>`+opts+`</select></div><div><label>Registro profissional</label><input name="registry" value="`+esc(edit.Registry)+`"></div></div><label><input style="width:auto" type="checkbox" name="active" value="1"`+checked+`> Ativo</label><button type="submit">Salvar profissional</button></form></div>`;writeHTML(w,"Profissionais",body)}

func (a *App) handleProfessionalSave(w http.ResponseWriter,r *http.Request){if r.Method!="POST"{http.Error(w,"método inválido",405);return};id:=intParam(r,"id");active:=r.FormValue("active")=="1"||id==0;if _,err:=a.upsertProfessional(id,r.FormValue("name"),r.FormValue("cpf"),r.FormValue("profession"),r.FormValue("registry"),active);err!=nil{redirectMsg(w,r,"/professionals","Erro: "+err.Error());return};redirectMsg(w,r,"/professionals","Profissional salvo.")}

func (a *App) handleImport(w http.ResponseWriter,r *http.Request){a.mu.Lock();p:=a.pending;ps:=append([]Professional(nil),a.store.Professionals...);a.mu.Unlock();body:=messageBox(r)+`<div class="card"><h1>Importar planilha Excel</h1><form method="post" action="/import/preview" enctype="multipart/form-data"><label>Arquivo .xlsx</label><input type="file" name="spreadsheet" accept=".xlsx" required><br><br><button type="submit">Ler planilha e gerar prévia</button></form><p class="small">Mês e ano são identificados automaticamente nos blocos da planilha. Nenhum preenchimento manual é necessário.</p></div>`
    if p!=nil{sum:=int64(0);for _,r:=range p.Valid{sum+=r.AmountCents};body+=`<div class="card"><h2>Prévia — `+esc(p.SourceFile)+`</h2><div class="grid"><div><b>Importáveis</b><div style="font-size:28px">`+strconv.Itoa(len(p.Valid))+`</div></div><div><b>Incompletos/erro</b><div style="font-size:28px">`+strconv.Itoa(len(p.Issues))+`</div></div><div><b>Total importável</b><div style="font-size:28px">`+esc(brMoney(sum))+`</div></div></div>`;if len(p.Valid)>0{body+=`<div class="scroll"><table><thead><tr><th>Linha</th><th>Responsável</th><th>CPF</th><th>Data</th><th>Valor</th><th>Competência</th></tr></thead><tbody>`;for _,x:=range p.Valid{body+=`<tr><td>`+strconv.Itoa(x.SourceRow)+`</td><td>`+esc(x.PayerName)+`</td><td>`+esc(x.PayerCPF)+`</td><td>`+esc(x.PaymentDate)+`</td><td>`+esc(brMoney(x.AmountCents))+`</td><td>`+fmt.Sprintf("%02d/%d",x.Month,x.Year)+`</td></tr>`};body+=`</tbody></table></div>`};if len(p.Issues)>0{body+=`<div class="warn"><b>Linhas não importadas</b><ul>`;for _,x:=range p.Issues{body+=`<li>Linha `+strconv.Itoa(x.SourceRow)+`: `+esc(x.Name)+` — `+esc(x.Reason)+`</li>`};body+=`</ul></div>`};if len(p.Valid)>0{body+=`<form method="post" action="/import/confirm"><label>Profissional de destino</label><select name="professional_id" required><option value="">Selecione</option>`;for _,pr:=range ps{if pr.Active{body+=`<option value="`+strconv.Itoa(pr.ID)+`">`+esc(pr.Name)+` — `+esc(professionLabels[pr.ProfessionKey])+`</option>`}};body+=`</select><br><br><button type="submit">Confirmar e salvar lançamentos</button></form>`};body+=`</div>`};writeHTML(w,"Importar Excel",body)}

func (a *App) handleImportPreview(w http.ResponseWriter,r *http.Request){if r.Method!="POST"{http.Error(w,"método inválido",405);return};r.Body=http.MaxBytesReader(w,r.Body,25<<20);f,h,err:=r.FormFile("spreadsheet");if err!=nil{redirectMsg(w,r,"/import","Erro: selecione um arquivo .xlsx");return};defer f.Close();raw,err:=io.ReadAll(f);if err!=nil{redirectMsg(w,r,"/import","Erro: "+err.Error());return};p,err:=parseWorkbookPreview(h.Filename,raw);if err!=nil{redirectMsg(w,r,"/import","Erro: "+err.Error());return};a.mu.Lock();a.pending=&p;a.mu.Unlock();redirectMsg(w,r,"/import",fmt.Sprintf("Prévia pronta: %d importáveis; %d incompletos/erro.",len(p.Valid),len(p.Issues)))}
func (a *App) handleImportConfirm(w http.ResponseWriter,r *http.Request){if r.Method!="POST"{http.Error(w,"método inválido",405);return};created,skipped,err:=a.confirmPreview(intParam(r,"professional_id"));if err!=nil{redirectMsg(w,r,"/import","Erro: "+err.Error());return};redirectMsg(w,r,"/payments",fmt.Sprintf("Importação salva: %d lançamentos; %d duplicados ignorados.",created,skipped))}

func (a *App) filteredPayments(r *http.Request)([]Payment,[]Professional,int,int,int){a.mu.Lock();payments:=append([]Payment(nil),a.store.Payments...);ps:=append([]Professional(nil),a.store.Professionals...);a.mu.Unlock();profID:=intParam(r,"professional_id");year:=intParam(r,"year");month:=intParam(r,"month");if year==0{year=time.Now().Year()};if month==0{month=int(time.Now().Month())};out:=[]Payment{};for _,p:=range payments{if profID>0&&p.ProfessionalID!=profID{continue};d,err:=time.Parse("2006-01-02",p.PaymentDate);if err!=nil{continue};if year>0&&d.Year()!=year{continue};if month>0&&int(d.Month())!=month{continue};out=append(out,p)};sort.Slice(out,func(i,j int)bool{return out[i].PaymentDate<out[j].PaymentDate});return out,ps,profID,year,month}
func profName(ps []Professional,id int)string{for _,p:=range ps{if p.ID==id{return p.Name}};return ""}
func selectProf(ps []Professional,selected int)string{out:=`<option value="0">Todos</option>`;for _,p:=range ps{sel:="";if p.ID==selected{sel=" selected"};out+=`<option value="`+strconv.Itoa(p.ID)+`"`+sel+`>`+esc(p.Name)+`</option>`};return out}
func monthOptions(selected int)string{names:=[]string{"","Janeiro","Fevereiro","Março","Abril","Maio","Junho","Julho","Agosto","Setembro","Outubro","Novembro","Dezembro"};out:="";for i:=1;i<=12;i++{sel:="";if i==selected{sel=" selected"};out+=`<option value="`+strconv.Itoa(i)+`"`+sel+`>`+names[i]+`</option>`};return out}

func (a *App) handlePayments(w http.ResponseWriter,r *http.Request){items,ps,profID,year,month:=a.filteredPayments(r);body:=messageBox(r)+`<div class="card"><h1>Lançamentos</h1><form method="get"><div class="grid"><div><label>Profissional</label><select name="professional_id">`+selectProf(ps,profID)+`</select></div><div><label>Ano</label><input name="year" type="number" value="`+strconv.Itoa(year)+`"></div><div><label>Mês</label><select name="month">`+monthOptions(month)+`</select></div></div><br><button>Filtrar</button></form></div><div class="card"><form method="post" action="/payments/save"><input type="hidden" name="back_prof" value="`+strconv.Itoa(profID)+`"><input type="hidden" name="back_year" value="`+strconv.Itoa(year)+`"><input type="hidden" name="back_month" value="`+strconv.Itoa(month)+`"><div class="scroll"><table><thead><tr><th>Data</th><th>Profissional</th><th>Responsável</th><th>CPF</th><th>Valor</th><th>Descrição fiscal</th><th>Classificação</th></tr></thead><tbody>`;for _,p:=range items{selP,selR,selN:="","","";switch p.Status{case statusPending:selP=" selected";case statusReceita:selR=" selected";case statusNF:selN=" selected"};body+=`<tr><td>`+esc(p.PaymentDate)+`</td><td>`+esc(profName(ps,p.ProfessionalID))+`</td><td>`+esc(p.PayerName)+`</td><td>`+esc(p.PayerCPF)+`</td><td>`+esc(brMoney(p.AmountCents))+`</td><td><input name="desc_`+strconv.Itoa(p.ID)+`" value="`+esc(p.Description)+`"></td><td><select name="status_`+strconv.Itoa(p.ID)+`"><option value="`+statusPending+`"`+selP+`>Pendente</option><option value="`+statusReceita+`"`+selR+`>Receita Saúde</option><option value="`+statusNF+`"`+selN+`>Nota Fiscal</option></select></td></tr>`};body+=`</tbody></table></div><br><button type="submit">Salvar classificações</button></form></div>`;writeHTML(w,"Lançamentos",body)}

func (a *App) handlePaymentsSave(w http.ResponseWriter,r *http.Request){if r.Method!="POST"{http.Error(w,"método inválido",405);return};_ = r.ParseForm();a.mu.Lock();for i:=range a.store.Payments{id:=a.store.Payments[i].ID;status:=r.FormValue("status_"+strconv.Itoa(id));if status==""{continue};if status!=statusPending&&status!=statusReceita&&status!=statusNF{continue};desc:=strings.TrimSpace(r.FormValue("desc_"+strconv.Itoa(id)));if status==statusReceita&&(desc==""||strings.ContainsAny(desc,";\r\n\"")){a.mu.Unlock();redirectMsg(w,r,"/payments","Erro: descrição fiscal obrigatória e sem ponto e vírgula/aspas para Receita Saúde.");return};a.store.Payments[i].Status=status;a.store.Payments[i].Description=desc};err:=a.saveLocked();a.mu.Unlock();if err!=nil{redirectMsg(w,r,"/payments","Erro: "+err.Error());return};path:=fmt.Sprintf("/payments?professional_id=%s&year=%s&month=%s",r.FormValue("back_prof"),r.FormValue("back_year"),r.FormValue("back_month"));redirectMsg(w,r,path,"Classificações salvas.")}

func (a *App) handleExport(w http.ResponseWriter,r *http.Request){a.mu.Lock();ps:=append([]Professional(nil),a.store.Professionals...);a.mu.Unlock();body:=messageBox(r)+`<div class="card"><h1>Exportar escrituração — Receita Saúde</h1><p>O arquivo inclui somente lançamentos classificados como <b>Receita Saúde</b>, do profissional e da competência escolhidos.</p><form method="get" action="/export/file"><div class="grid"><div><label>Profissional</label><select name="professional_id" required><option value="">Selecione</option>`;for _,p:=range ps{if p.Active{body+=`<option value="`+strconv.Itoa(p.ID)+`">`+esc(p.Name)+`</option>`}};body+=`</select></div><div><label>Ano</label><input name="year" type="number" value="`+strconv.Itoa(time.Now().Year())+`" required></div><div><label>Mês</label><select name="month" required>`+monthOptions(int(time.Now().Month()))+`</select></div></div><br><button type="submit">Gerar CSV da escrituração</button></form><p class="small">Contrato: sem cabeçalho; separador “;”; 16 campos; uma competência e um profissional por arquivo.</p></div>`;writeHTML(w,"Exportar",body)}
func (a *App) handleExportFile(w http.ResponseWriter,r *http.Request){profID,year,month:=intParam(r,"professional_id"),intParam(r,"year"),intParam(r,"month");a.mu.Lock();prof,ok:=a.professionalByID(profID);payments:=append([]Payment(nil),a.store.Payments...);a.mu.Unlock();if !ok{redirectMsg(w,r,"/export","Erro: profissional não encontrado");return};raw,count,err:=buildExport(prof,payments,year,month);if err!=nil{redirectMsg(w,r,"/export","Erro: "+err.Error());return};name:=fmt.Sprintf("RECEITA_SAUDE_%d_%02d_PROF_%d.csv",year,month,profID);w.Header().Set("Content-Type","text/csv; charset=utf-8");w.Header().Set("Content-Disposition",`attachment; filename="`+name+`"`);w.Header().Set("X-GD-Row-Count",strconv.Itoa(count));_,_=w.Write(raw)}

func openBrowser(url string){if os.Getenv("GD_FISCAL_NO_BROWSER")!=""{return};switch runtime.GOOS{case "windows":_ = exec.Command("rundll32","url.dll,FileProtocolHandler",url).Start();case "darwin":_ = exec.Command("open",url).Start();default:_ = exec.Command("xdg-open",url).Start()}}

func existingInstance(dataDir string)string{raw,err:=os.ReadFile(filepath.Join(dataDir,"runtime","port.txt"));if err!=nil{return ""};port:=strings.TrimSpace(string(raw));if port==""{return ""};url:="http://127.0.0.1:"+port;client:=http.Client{Timeout:600*time.Millisecond};resp,err:=client.Get(url+"/healthz");if err==nil{defer resp.Body.Close();if resp.StatusCode==200{return url}};return ""}

func runServer() error {
    dataDir:=resolveDataDir();if err:=os.MkdirAll(filepath.Join(dataDir,"runtime"),0o755);err!=nil{return err}
    lockPath:=filepath.Join(dataDir,"runtime","app.lock");lf,err:=os.OpenFile(lockPath,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0o600);if err!=nil{if url:=existingInstance(dataDir);url!=""{openBrowser(url);return nil};_ = os.Remove(lockPath);lf,err=os.OpenFile(lockPath,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0o600)};if err!=nil{return err};_,_=fmt.Fprintf(lf,"%d",os.Getpid());_ = lf.Close();defer os.Remove(lockPath)
    a,err:=newApp(dataDir);if err!=nil{return err}
    addr:="127.0.0.1:0";if p:=strings.TrimSpace(os.Getenv("GD_FISCAL_PORT"));p!=""{addr="127.0.0.1:"+p};ln,err:=net.Listen("tcp",addr);if err!=nil{return err};defer ln.Close();port:=ln.Addr().(*net.TCPAddr).Port;portFile:=filepath.Join(dataDir,"runtime","port.txt");_ = os.WriteFile(portFile,[]byte(strconv.Itoa(port)),0o600);defer os.Remove(portFile);url:="http://127.0.0.1:"+strconv.Itoa(port);openBrowser(url);srv:=&http.Server{Handler:a.routes(),ReadHeaderTimeout:10*time.Second};return srv.Serve(ln)
}

func syntheticJulyXLSX()([]byte,error){var sheet strings.Builder;sheet.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`);row:=func(n int,cells string){sheet.WriteString(`<row r="`+strconv.Itoa(n)+`">`+cells+`</row>`)};inline:=func(ref,val string)string{return `<c r="`+ref+`" t="inlineStr"><is><t>`+html.EscapeString(val)+`</t></is></c>`};num:=func(ref,val string)string{return `<c r="`+ref+`"><v>`+val+`</v></c>`};row(1,inline("A1","Carnê Leão - Livro Caixa - 2026"));row(83,inline("A83","JULHO"));row(84,inline("A84","Nome")+inline("G84","CPF - Responsável")+inline("J84","CPF e Nome - Paciente - Se necessário")+inline("P84","Data")+inline("R84","Valor"));amounts:=make([]float64,26);for i:=0;i<25;i++{amounts[i]=400};amounts[25]=870;for i,amt:=range amounts{rr:=85+i;row(rr,inline("A"+strconv.Itoa(rr),fmt.Sprintf("Pagador Julho %d",i+1))+inline("G"+strconv.Itoa(rr),"52998224725")+num("P"+strconv.Itoa(rr),strconv.Itoa(i+1))+num("R"+strconv.Itoa(rr),fmt.Sprintf("%.2f",amt)))};for i,name:=range []string{"Henrique","Elisandra","Pâmela","Maria Fernanda"}{rr:=111+i;row(rr,inline("A"+strconv.Itoa(rr),name)+inline("G"+strconv.Itoa(rr),"52998224725"))};row(121,inline("A121","Valor Total Mês")+num("R121","10870"));sheet.WriteString(`</sheetData></worksheet>`)
    var buf bytes.Buffer;zw:=zip.NewWriter(&buf);add:=func(name,content string)error{w,err:=zw.Create(name);if err!=nil{return err};_,err=io.WriteString(w,content);return err};if err:=add("[Content_Types].xml",`<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`);err!=nil{return nil,err};if err:=add("_rels/.rels",`<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`);err!=nil{return nil,err};if err:=add("xl/workbook.xml",`<?xml version="1.0"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Plan1" sheetId="1" r:id="rId1"/></sheets></workbook>`);err!=nil{return nil,err};if err:=add("xl/_rels/workbook.xml.rels",`<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`);err!=nil{return nil,err};if err:=add("xl/worksheets/sheet1.xml",sheet.String());err!=nil{return nil,err};if err:=zw.Close();err!=nil{return nil,err};return buf.Bytes(),nil}

func runSelfTest() error {
    raw,err:=syntheticJulyXLSX();if err!=nil{return err};preview,err:=parseWorkbookPreview("auditoria-julho.xlsx",raw);if err!=nil{return err};if len(preview.Valid)!=26||len(preview.Issues)!=4{return fmt.Errorf("auditoria parser: esperado 26/4, obtido %d/%d",len(preview.Valid),len(preview.Issues))};sum:=int64(0);for _,r:=range preview.Valid{sum+=r.AmountCents};if sum!=1087000{return fmt.Errorf("auditoria parser: total %d",sum)}
    dir,err:=os.MkdirTemp("","gd-fiscal-selftest-");if err!=nil{return err};defer os.RemoveAll(dir);a,err:=newApp(dir);if err!=nil{return err};idA,err:=a.upsertProfessional(0,"Profissional A","52998224725","PSICOLOGO","07/11111",true);if err!=nil{return err};idB,err:=a.upsertProfessional(0,"Profissional B","16899535009","PSICOLOGO","07/22222",true);if err!=nil{return err};a.pending=&preview;if n,_,err:=a.confirmPreview(idA);err!=nil||n!=26{return fmt.Errorf("auditoria import A: n=%d err=%v",n,err)};a.pending=&preview;if n,_,err:=a.confirmPreview(idB);err!=nil||n!=26{return fmt.Errorf("auditoria import B: n=%d err=%v",n,err)};a.mu.Lock();for i:=range a.store.Payments{if a.store.Payments[i].ProfessionalID==idA{a.store.Payments[i].Status=statusReceita}};if err:=a.saveLocked();err!=nil{a.mu.Unlock();return err};a.mu.Unlock();b,err:=newApp(dir);if err!=nil{return err};if len(b.store.Professionals)!=2||len(b.store.Payments)!=52{return fmt.Errorf("auditoria persistência falhou")};profA,_:=b.professionalByID(idA);csvRaw,count,err:=buildExport(profA,b.store.Payments,2026,7);if err!=nil{return err};if count!=26{return fmt.Errorf("auditoria export: %d linhas",count)};rd:=csv.NewReader(bytes.NewReader(csvRaw));rd.Comma=';';rows,err:=rd.ReadAll();if err!=nil{return err};if len(rows)!=26{return fmt.Errorf("auditoria csv: %d",len(rows))};for _,row:=range rows{if len(row)!=16||row[1]!="R01.001.001"||row[14]!=profA.CPF{return errors.New("auditoria csv: contrato inválido")};if row[14]=="16899535009"{return errors.New("auditoria isolamento profissional falhou")}};return nil
}

func main(){if len(os.Args)>1&&os.Args[1]=="--self-test"{if err:=runSelfTest();err!=nil{os.Exit(1)};os.Exit(0)};if err:=runServer();err!=nil{os.Exit(2)}}
