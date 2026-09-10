package main

import (
    "archive/zip"
    "bytes"
    "encoding/xml"
    "fmt"
    "io"
    "math"
    "path"
    "regexp"
    "strconv"
    "strings"
    "time"
    "unicode"
)

type Cell struct {
    Text string
    Num  float64
    IsNum bool
}

type SheetData struct {
    Name string
    Rows [][]Cell
}

type ImportRow struct {
    SourceRow      int
    Month          int
    Year           int
    PayerName      string
    PayerCPF       string
    BeneficiaryName string
    BeneficiaryCPF string
    PaymentDate    time.Time
    AmountCents    int64
    Description    string
}

type ImportIssue struct {
    SourceRow       int
    Month           int
    Year            int
    PayerName       string
    PayerCPFRaw     string
    BeneficiaryName string
    BeneficiaryCPFRaw string
    DateRaw         string
    AmountRaw       string
    Code            string
    Reason          string
    Content         string
}

type ImportPreview struct {
    Valid      []ImportRow
    Skipped    []ImportIssue
    Restricted []ImportIssue
    Errors     []ImportIssue
}

var monthsPT = map[string]int{
    "janeiro": 1,
    "fevereiro": 2,
    "marco": 3,
    "abril": 4,
    "maio": 5,
    "junho": 6,
    "julho": 7,
    "agosto": 8,
    "setembro": 9,
    "outubro": 10,
    "novembro": 11,
    "dezembro": 12,
}

var yearRE = regexp.MustCompile(`\b(20\d{2})\b`)

func normalizeText(s string) string {
    s = strings.ToLower(strings.TrimSpace(s))
    r := strings.NewReplacer(
        "á", "a", "à", "a", "ã", "a", "â", "a", "ä", "a",
        "é", "e", "è", "e", "ê", "e", "ë", "e",
        "í", "i", "ì", "i", "î", "i", "ï", "i",
        "ó", "o", "ò", "o", "õ", "o", "ô", "o", "ö", "o",
        "ú", "u", "ù", "u", "û", "u", "ü", "u",
        "ç", "c",
    )
    return r.Replace(s)
}

func cellString(c Cell) string {
    if strings.TrimSpace(c.Text) != "" {
        return strings.TrimSpace(c.Text)
    }
    if c.IsNum {
        if math.Abs(c.Num-math.Round(c.Num)) < 1e-9 {
            return strconv.FormatInt(int64(math.Round(c.Num)), 10)
        }
        return strconv.FormatFloat(c.Num, 'f', -1, 64)
    }
    return ""
}

func colIndex(ref string) int {
    n := 0
    for _, r := range ref {
        if r < 'A' || r > 'Z' {
            break
        }
        n = n*26 + int(r-'A'+1)
    }
    if n == 0 {
        return -1
    }
    return n - 1
}

func openZipEntry(files map[string]*zip.File, name string) ([]byte, error) {
    name = strings.TrimPrefix(path.Clean("/"+name), "/")
    f := files[name]
    if f == nil {
        return nil, fmt.Errorf("entrada XLSX ausente: %s", name)
    }
    rc, err := f.Open()
    if err != nil {
        return nil, err
    }
    defer rc.Close()
    return io.ReadAll(rc)
}

func readSharedStrings(files map[string]*zip.File) ([]string, error) {
    f := files["xl/sharedStrings.xml"]
    if f == nil {
        return nil, nil
    }
    raw, err := openZipEntry(files, "xl/sharedStrings.xml")
    if err != nil {
        return nil, err
    }
    dec := xml.NewDecoder(bytes.NewReader(raw))
    var out []string
    for {
        tok, err := dec.Token()
        if err == io.EOF {
            break
        }
        if err != nil {
            return nil, err
        }
        se, ok := tok.(xml.StartElement)
        if !ok || se.Name.Local != "si" {
            continue
        }
        var holder struct {
            T string `xml:"t"`
            R []struct{ T string `xml:"t"` } `xml:"r"`
        }
        if err := dec.DecodeElement(&holder, &se); err != nil {
            return nil, err
        }
        var b strings.Builder
        if holder.T != "" {
            b.WriteString(holder.T)
        }
        for _, r := range holder.R {
            b.WriteString(r.T)
        }
        out = append(out, b.String())
    }
    return out, nil
}

type workbookSheet struct {
    Name string
    RelID string
    State string
}

func workbookSheets(files map[string]*zip.File) ([]workbookSheet, error) {
    raw, err := openZipEntry(files, "xl/workbook.xml")
    if err != nil {
        return nil, err
    }
    dec := xml.NewDecoder(bytes.NewReader(raw))
    var sheets []workbookSheet
    for {
        tok, err := dec.Token()
        if err == io.EOF {
            break
        }
        if err != nil {
            return nil, err
        }
        se, ok := tok.(xml.StartElement)
        if !ok || se.Name.Local != "sheet" {
            continue
        }
        var s workbookSheet
        for _, a := range se.Attr {
            switch a.Name.Local {
            case "name":
                s.Name = a.Value
            case "id":
                s.RelID = a.Value
            case "state":
                s.State = a.Value
            }
        }
        if s.State == "" || s.State == "visible" {
            sheets = append(sheets, s)
        }
    }
    return sheets, nil
}

func workbookRelationships(files map[string]*zip.File) (map[string]string, error) {
    raw, err := openZipEntry(files, "xl/_rels/workbook.xml.rels")
    if err != nil {
        return nil, err
    }
    dec := xml.NewDecoder(bytes.NewReader(raw))
    rels := map[string]string{}
    for {
        tok, err := dec.Token()
        if err == io.EOF {
            break
        }
        if err != nil {
            return nil, err
        }
        se, ok := tok.(xml.StartElement)
        if !ok || se.Name.Local != "Relationship" {
            continue
        }
        id, target := "", ""
        for _, a := range se.Attr {
            if a.Name.Local == "Id" { id = a.Value }
            if a.Name.Local == "Target" { target = a.Value }
        }
        if id != "" && target != "" {
            if strings.HasPrefix(target, "/") {
                rels[id] = strings.TrimPrefix(target, "/")
            } else {
                rels[id] = path.Clean("xl/" + target)
            }
        }
    }
    return rels, nil
}

func parseSheet(raw []byte, shared []string) ([][]Cell, error) {
    dec := xml.NewDecoder(bytes.NewReader(raw))
    rows := map[int]map[int]Cell{}
    maxRow, maxCol := -1, -1
    currentRow := -1

    for {
        tok, err := dec.Token()
        if err == io.EOF {
            break
        }
        if err != nil {
            return nil, err
        }
        se, ok := tok.(xml.StartElement)
        if !ok {
            continue
        }
        if se.Name.Local == "row" {
            currentRow++
            for _, a := range se.Attr {
                if a.Name.Local == "r" {
                    if n, e := strconv.Atoi(a.Value); e == nil && n > 0 {
                        currentRow = n - 1
                    }
                }
            }
            continue
        }
        if se.Name.Local != "c" {
            continue
        }
        ref, typ := "", ""
        for _, a := range se.Attr {
            if a.Name.Local == "r" { ref = a.Value }
            if a.Name.Local == "t" { typ = a.Value }
        }
        cidx := colIndex(ref)
        if cidx < 0 || currentRow < 0 {
            var skip any
            _ = dec.DecodeElement(&skip, &se)
            continue
        }
        var holder struct {
            V string `xml:"v"`
            IS struct{ T string `xml:"t"`; R []struct{ T string `xml:"t"` } `xml:"r"` } `xml:"is"`
        }
        if err := dec.DecodeElement(&holder, &se); err != nil {
            return nil, err
        }
        cell := Cell{}
        switch typ {
        case "s":
            idx, _ := strconv.Atoi(strings.TrimSpace(holder.V))
            if idx >= 0 && idx < len(shared) {
                cell.Text = shared[idx]
            }
        case "inlineStr":
            var b strings.Builder
            b.WriteString(holder.IS.T)
            for _, r := range holder.IS.R { b.WriteString(r.T) }
            cell.Text = b.String()
        case "str", "e":
            cell.Text = holder.V
        case "b":
            if strings.TrimSpace(holder.V) == "1" { cell.Text = "TRUE" } else { cell.Text = "FALSE" }
        default:
            rawV := strings.TrimSpace(holder.V)
            if rawV != "" {
                if n, e := strconv.ParseFloat(rawV, 64); e == nil {
                    cell.Num = n
                    cell.IsNum = true
                } else {
                    cell.Text = rawV
                }
            }
        }
        if rows[currentRow] == nil { rows[currentRow] = map[int]Cell{} }
        rows[currentRow][cidx] = cell
        if currentRow > maxRow { maxRow = currentRow }
        if cidx > maxCol { maxCol = cidx }
    }
    if maxRow < 0 || maxCol < 0 {
        return nil, nil
    }
    out := make([][]Cell, maxRow+1)
    for r := 0; r <= maxRow; r++ {
        out[r] = make([]Cell, maxCol+1)
        for c, cell := range rows[r] { out[r][c] = cell }
    }
    return out, nil
}

func readXLSX(data []byte) ([]SheetData, error) {
    zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
    if err != nil { return nil, fmt.Errorf("arquivo XLSX inválido: %w", err) }
    files := map[string]*zip.File{}
    for _, f := range zr.File { files[path.Clean(f.Name)] = f }
    shared, err := readSharedStrings(files)
    if err != nil { return nil, err }
    sheets, err := workbookSheets(files)
    if err != nil { return nil, err }
    rels, err := workbookRelationships(files)
    if err != nil { return nil, err }
    var out []SheetData
    for _, s := range sheets {
        target := rels[s.RelID]
        if target == "" { continue }
        raw, err := openZipEntry(files, target)
        if err != nil { return nil, err }
        rows, err := parseSheet(raw, shared)
        if err != nil { return nil, err }
        out = append(out, SheetData{Name: s.Name, Rows: rows})
    }
    if len(out) == 0 { return nil, fmt.Errorf("nenhuma aba visível encontrada") }
    return out, nil
}

func yearFromText(s string) int {
    m := yearRE.FindStringSubmatch(s)
    if len(m) != 2 { return 0 }
    y, _ := strconv.Atoi(m[1])
    return y
}

func monthFromText(s string) int {
    n := normalizeText(s)
    for name, m := range monthsPT {
        if strings.Contains(n, name) { return m }
    }
    return 0
}

func globalYear(rows [][]Cell, title string) int {
    if y := yearFromText(title); y != 0 { return y }
    limit := len(rows); if limit > 20 { limit = 20 }
    for r := 0; r < limit; r++ {
        for _, c := range rows[r] {
            if y := yearFromText(cellString(c)); y != 0 { return y }
        }
    }
    return 0
}

func payerHeaderColumns(row []Cell) []int {
    var out []int
    for i, c := range row {
        t := normalizeText(cellString(c))
        if strings.Contains(t, "cpf") && (strings.Contains(t, "responsavel") || strings.Contains(t, "pagador")) {
            out = append(out, i)
        }
    }
    return out
}

func isNameHeader(s string) bool {
    t := normalizeText(s)
    return t == "nome" || (strings.Contains(t, "nome") && (strings.Contains(t, "responsavel") || strings.Contains(t, "pagador")))
}

func findNameColumn(row []Cell, payer, right int) int {
    start := payer - 10; if start < 0 { start = 0 }
    for i := payer-1; i >= start; i-- {
        if isNameHeader(cellString(row[i])) { return i }
    }
    end := payer+11; if end > right { end = right }; if end > len(row) { end = len(row) }
    for i := payer+1; i < end; i++ {
        if isNameHeader(cellString(row[i])) { return i }
    }
    return -1
}

func findColumn(row []Cell, start, end int, required string, forbidden string) int {
    if start < 0 { start = 0 }; if end > len(row) { end = len(row) }
    for i := start; i < end; i++ {
        t := normalizeText(cellString(row[i]))
        if strings.Contains(t, required) && (forbidden == "" || !strings.Contains(t, forbidden)) { return i }
    }
    return -1
}

func findBeneficiaryColumn(row []Cell, start, end int) int {
    if start < 0 { start = 0 }; if end > len(row) { end = len(row) }
    for i := start; i < end; i++ {
        t := normalizeText(cellString(row[i]))
        if strings.Contains(t, "paciente") || strings.Contains(t, "beneficiario") { return i }
    }
    return -1
}

type block struct {
    headerRow int
    start int
    end int
    nameCol int
    payerCol int
    beneficiaryCol int
    dateCol int
    amountCol int
    month int
    year int
}

func blockContext(rows [][]Cell, header, start, end int, title string, gy int) (int,int) {
    for r := header-1; r >= 0 && r >= header-4; r-- {
        e := end; if e > len(rows[r]) { e = len(rows[r]) }
        for c := start; c < e; c++ {
            s := cellString(rows[r][c])
            if m := monthFromText(s); m != 0 {
                y := yearFromText(s); if y == 0 { y = gy }
                return m,y
            }
        }
    }
    if m := monthFromText(title); m != 0 {
        y := yearFromText(title); if y == 0 { y = gy }
        return m,y
    }
    return 0,gy
}

func detectBlocks(rows [][]Cell, title string) []block {
    gy := globalYear(rows,title)
    var out []block
    for r, row := range rows {
        payers := payerHeaderColumns(row)
        if len(payers)==0 { continue }
        names := make([]int,len(payers))
        for i,p := range payers { names[i] = findNameColumn(row,p,len(row)) }
        for pos,p := range payers {
            start,end := 0,len(row)
            if len(payers)>1 {
                if names[pos]>=0 && names[pos]<p { start=names[pos] } else { start=p-10; if start<0 {start=0} }
                if pos+1<len(payers) {
                    nextName,nextP := names[pos+1],payers[pos+1]
                    if nextName>p { end=nextName } else { end=nextP }
                }
            }
            ben := findBeneficiaryColumn(row,start,end)
            dc := findColumn(row,start,end,"data","nascimento")
            ac := findColumn(row,start,end,"valor","")
            if dc<0 || ac<0 { continue }
            m,y := blockContext(rows,r,start,end,title,gy)
            out=append(out,block{r,start,end,names[pos],p,ben,dc,ac,m,y})
        }
    }
    return out
}

func isTotalRow(row []Cell,b block) bool {
    end:=b.end; if end>len(row){end=len(row)}
    for c:=b.start;c<end;c++ {
        if strings.Contains(normalizeText(cellString(row[c])),"valor total mes") { return true }
    }
    return false
}

func isRepeatedHeader(row []Cell,b block) bool {
    if b.payerCol>=len(row){return false}
    t:=normalizeText(cellString(row[b.payerCol]))
    return strings.Contains(t,"cpf")&&(strings.Contains(t,"responsavel")||strings.Contains(t,"pagador"))
}

func rowContent(row []Cell,start,end int) string {
    if end>len(row){end=len(row)}
    var vals []string
    for c:=start;c<end;c++ {
        s:=strings.TrimSpace(cellString(row[c])); if s!="" { vals=append(vals,s) }
    }
    return strings.Join(vals," | ")
}

func excelDate(n float64) (time.Time,error) {
    if n <= 0 { return time.Time{},fmt.Errorf("data inválida") }
    days:=int(math.Floor(n))
    frac:=n-float64(days)
    base:=time.Date(1899,12,30,0,0,0,0,time.Local)
    t:=base.AddDate(0,0,days).Add(time.Duration(frac*24*float64(time.Hour)))
    if t.Year()<2000 || t.Year()>2100 { return time.Time{},fmt.Errorf("data inválida") }
    return t,nil
}

func parseDateCell(c Cell,month,year int)(time.Time,error){
    if c.IsNum {
        n:=c.Num
        if month>0&&year>0&&n>=1&&n<=31&&math.Abs(n-math.Round(n))<1e-9 {
            d:=int(math.Round(n)); t:=time.Date(year,time.Month(month),d,0,0,0,0,time.Local)
            if t.Year()!=year||int(t.Month())!=month||t.Day()!=d { return time.Time{},fmt.Errorf("data inválida") }
            return t,nil
        }
        return excelDate(n)
    }
    s:=strings.TrimSpace(c.Text); if s=="" { return time.Time{},fmt.Errorf("data vazia") }
    if n,err:=strconv.Atoi(s);err==nil&&month>0&&year>0&&n>=1&&n<=31 {
        t:=time.Date(year,time.Month(month),n,0,0,0,0,time.Local)
        if t.Day()==n&&int(t.Month())==month{return t,nil}
    }
    for _,layout:=range []string{"02/01/2006","2006-01-02","02-01-2006","02.01.2006"}{
        if t,err:=time.ParseInLocation(layout,s,time.Local);err==nil{return t,nil}
    }
    return time.Time{},fmt.Errorf("data inválida")
}

func parseAmountCell(c Cell)(int64,error){
    if c.IsNum {
        if c.Num<=0{return 0,fmt.Errorf("valor inválido")}
        return int64(c.Num*100+0.5),nil
    }
    return parseBRAmount(c.Text)
}

func extractCPFName(c Cell)(string,string,string){
    raw:=cellString(c)
    digits:=onlyDigits(raw)
    name:=strings.TrimSpace(raw)
    if len(digits)>=11 {
        // Prefer the first 11-digit run after stripping punctuation.
        if len(digits)>11 { digits=digits[:11] }
        // Remove numeric/punctuation-heavy tokens from name conservatively.
        fields:=strings.FieldsFunc(name,func(r rune)bool{return unicode.IsSpace(r)||r=='-'||r=='/'||r==';'||r==','})
        var keep []string
        for _,f:=range fields { if len(onlyDigits(f))<8 { keep=append(keep,f) } }
        name=strings.TrimSpace(strings.Join(keep," "))
        if validCPF(digits){return digits,name,raw}
        return "",name,raw
    }
    if c.IsNum && len(digits)==10 {
        candidate:="0"+digits
        if validCPF(candidate){return candidate,"",raw}
    }
    return "",name,raw
}

func classifyIssue(reason string) string {
    n:=normalizeText(reason)
    switch {
    case strings.Contains(n,"cpf"): return "CPF_INVALIDO"
    case strings.Contains(n,"data"): return "DATA_INVALIDA"
    case strings.Contains(n,"valor"): return "VALOR_INVALIDO"
    case strings.Contains(n,"nome"): return "NOME_AUSENTE"
    default:return "DADOS_INVALIDOS"
    }
}

func PreviewXLSX(data []byte)(ImportPreview,error){
    sheets,err:=readXLSX(data); if err!=nil{return ImportPreview{},err}
    p:=ImportPreview{}
    recognized:=0
    for _,sheet:=range sheets{
        blocks:=detectBlocks(sheet.Rows,sheet.Name)
        recognized+=len(blocks)
        for _,b:=range blocks{
            for r:=b.headerRow+1;r<len(sheet.Rows);r++{
                row:=sheet.Rows[r]
                if isTotalRow(row,b)||isRepeatedHeader(row,b){break}
                end:=b.end;if end>len(row){end=len(row)}
                has:=false
                for c:=b.start;c<end;c++{if strings.TrimSpace(cellString(row[c]))!=""{has=true;break}}
                if !has{continue}
                get:=func(idx int)Cell{if idx>=0&&idx<len(row){return row[idx]};return Cell{}}
                name:=strings.TrimSpace(cellString(get(b.nameCol)))
                payerCPF,parsedName,payerRaw:=extractCPFName(get(b.payerCol))
                if name==""{name=parsedName}
                benCPF,benName,benRaw:="","",""
                if b.beneficiaryCol>=0 { benCPF,benName,benRaw=extractCPFName(get(b.beneficiaryCol)) }
                if strings.TrimSpace(benRaw)=="" { benCPF=payerCPF;benName=name;benRaw=payerRaw }
                dateCell,amountCell:=get(b.dateCol),get(b.amountCol)
                dateRaw,amountRaw:=cellString(dateCell),cellString(amountCell)
                common:=ImportIssue{SourceRow:r+1,Month:b.month,Year:b.year,PayerName:name,PayerCPFRaw:payerRaw,BeneficiaryName:benName,BeneficiaryCPFRaw:benRaw,DateRaw:dateRaw,AmountRaw:amountRaw,Content:rowContent(row,b.start,b.end)}
                if strings.TrimSpace(dateRaw)==""&&strings.TrimSpace(amountRaw)=="" {
                    common.Code="SEM_ATENDIMENTO";common.Reason="Sem atendimento no período";p.Skipped=append(p.Skipped,common);continue
                }
                var reasons []string
                if name=="" { reasons=append(reasons,"nome do responsável ausente") }
                if payerCPF=="" { reasons=append(reasons,"CPF do responsável ausente ou inválido") }
                if benCPF=="" { reasons=append(reasons,"CPF do beneficiário ausente ou inválido") }
                d,derr:=parseDateCell(dateCell,b.month,b.year);if derr!=nil{reasons=append(reasons,derr.Error())}
                amount,aerr:=parseAmountCell(amountCell);if aerr!=nil{reasons=append(reasons,aerr.Error())}
                if len(reasons)>0 {
                    common.Reason=strings.Join(reasons,"; ");common.Code=classifyIssue(common.Reason);p.Restricted=append(p.Restricted,common);continue
                }
                if benName==""{benName=name}
                p.Valid=append(p.Valid,ImportRow{SourceRow:r+1,Month:b.month,Year:b.year,PayerName:name,PayerCPF:payerCPF,BeneficiaryName:benName,BeneficiaryCPF:benCPF,PaymentDate:d,AmountCents:amount,Description:"SERVIÇO DE SAÚDE"})
            }
        }
    }
    if recognized==0 { return p,fmt.Errorf("cabeçalho não reconhecido na planilha") }
    return p,nil
}
