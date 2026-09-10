package main

import (
    "archive/zip"
    "bytes"
    "encoding/xml"
    "fmt"
    "io"
    "math"
    "regexp"
    "sort"
    "strconv"
    "strings"
    "time"
)

type Cell struct { Value string; Numeric bool }
type Sheet struct { Name string; Rows [][]Cell }
type ImportRow struct {
    SourceRow int
    CompetenceMonth int
    CompetenceYear int
    PayerName string
    PayerCPFRaw string
    PayerCPF string
    BeneficiaryName string
    BeneficiaryCPFRaw string
    BeneficiaryCPF string
    PaymentDate string
    AmountCents int64
    Reason string
}
type Preview struct { Valid []ImportRow; Skipped []ImportRow; Restricted []ImportRow; Errors []ImportRow; Competences []string }

type xSI struct { T string `xml:"t"`; Runs []struct{T string `xml:"t"`} `xml:"r"` }
type xSST struct { SI []xSI `xml:"si"` }
type xCell struct { Ref string `xml:"r,attr"`; Type string `xml:"t,attr"`; V string `xml:"v"`; Inline xSI `xml:"is"` }
type xRow struct { R int `xml:"r,attr"`; Cells []xCell `xml:"c"` }
type xWorksheet struct { Rows []xRow `xml:"sheetData>row"` }
type xSheetRef struct { Name string `xml:"name,attr"`; State string `xml:"state,attr"`; RID string `xml:"id,attr"` }
type xWorkbook struct { Sheets []xSheetRef `xml:"sheets>sheet"` }
type xRel struct { ID string `xml:"Id,attr"`; Target string `xml:"Target,attr"` }
type xRels struct { Rels []xRel `xml:"Relationship"` }

var yearRE=regexp.MustCompile(`\b(20\d{2})\b`)
var digitsRE=regexp.MustCompile(`\D`)

func norm(s string) string {
    s=strings.ToLower(strings.TrimSpace(s))
    repl:=strings.NewReplacer("á","a","à","a","ã","a","â","a","ä","a","é","e","ê","e","è","e","í","i","ì","i","ó","o","ô","o","õ","o","ö","o","ú","u","ü","u","ç","c","–","-","—","-")
    s=repl.Replace(s)
    return strings.Join(strings.Fields(s)," ")
}
func onlyDigits(s string) string { return digitsRE.ReplaceAllString(s,"") }
func yearFrom(s string) int { m:=yearRE.FindStringSubmatch(s); if len(m)>1 { n,_:=strconv.Atoi(m[1]); return n }; return 0 }
func monthFrom(s string) int { n:=norm(s); names:=[]string{"janeiro","fevereiro","marco","abril","maio","junho","julho","agosto","setembro","outubro","novembro","dezembro"}; for i,x:=range names { if strings.Contains(n,x){return i+1} }; return 0 }

func cpfValid(raw string) bool {
    cpf:=onlyDigits(raw); if len(cpf)!=11 {return false}; same:=true; for i:=1;i<11;i++{if cpf[i]!=cpf[0]{same=false;break}}; if same{return false}
    nums:=make([]int,11); for i:=0;i<11;i++{nums[i]=int(cpf[i]-'0')}
    for length:=9; length<=10; length++ { total:=0; for i:=0;i<length;i++{total+=nums[i]*(length+1-i)}; d:=(total*10)%11; if d==10{d=0}; if d!=nums[length]{return false} }
    return true
}
func normalizeCPF(raw string, numeric bool) (string,bool) { d:=onlyDigits(raw); if len(d)==11&&cpfValid(d){return d,true}; if numeric&&len(d)==10 { c:="0"+d; if cpfValid(c){return c,true} }; return d,false }

func zipFileBytes(z *zip.Reader,name string)([]byte,error){ for _,f:=range z.File{if strings.EqualFold(strings.ReplaceAll(f.Name,"\\","/"),name){r,e:=f.Open();if e!=nil{return nil,e};defer r.Close();return io.ReadAll(r)}};return nil,fmt.Errorf("arquivo XLSX ausente: %s",name)}
func parseShared(z *zip.Reader) []string { b,e:=zipFileBytes(z,"xl/sharedStrings.xml"); if e!=nil{return nil}; var s xSST; if xml.Unmarshal(b,&s)!=nil{return nil}; out:=make([]string,len(s.SI)); for i,si:=range s.SI { v:=si.T; for _,r:=range si.Runs{v+=r.T};out[i]=v}; return out }
func colIndex(ref string) int { n:=0; for _,r:=range ref { if r>='A'&&r<='Z'{n=n*26+int(r-'A'+1)} else if r>='a'&&r<='z'{n=n*26+int(r-'a'+1)} else {break} }; return n-1 }
func parseWorksheet(b []byte, shared []string) ([][]Cell,error){ var ws xWorksheet; if err:=xml.Unmarshal(b,&ws);err!=nil{return nil,err}; maxR,maxC:=0,0; for _,r:=range ws.Rows{rr:=r.R;if rr==0{continue};if rr>maxR{maxR=rr};for _,c:=range r.Cells{cc:=colIndex(c.Ref)+1;if cc>maxC{maxC=cc}}}; rows:=make([][]Cell,maxR);for i:=range rows{rows[i]=make([]Cell,maxC)}; for _,r:=range ws.Rows{ri:=r.R-1;if ri<0||ri>=len(rows){continue};for _,c:=range r.Cells{ci:=colIndex(c.Ref);if ci<0||ci>=maxC{continue};v:=c.V;numeric:=c.Type==""||c.Type=="n";switch c.Type{case "s":idx,_:=strconv.Atoi(v);if idx>=0&&idx<len(shared){v=shared[idx]};numeric=false;case "inlineStr":v=c.Inline.T;for _,rr:=range c.Inline.Runs{v+=rr.T};numeric=false;case "str":numeric=false};rows[ri][ci]=Cell{Value:strings.TrimSpace(v),Numeric:numeric}}}; return rows,nil }

func readXLSX(raw []byte)([]Sheet,error){ z,e:=zip.NewReader(bytes.NewReader(raw),int64(len(raw)));if e!=nil{return nil,fmt.Errorf("XLSX inválido: %w",e)};shared:=parseShared(z); var refs []xSheetRef; var relmap=map[string]string{}; if b,e:=zipFileBytes(z,"xl/workbook.xml");e==nil{var wb xWorkbook;_ = xml.Unmarshal(b,&wb);refs=wb.Sheets}; if b,e:=zipFileBytes(z,"xl/_rels/workbook.xml.rels");e==nil{var rs xRels;_ = xml.Unmarshal(b,&rs);for _,r:=range rs.Rels{relmap[r.ID]=r.Target}}
    var sheets []Sheet
    if len(refs)>0 { for i,rf:=range refs { if strings.EqualFold(rf.State,"hidden")||strings.EqualFold(rf.State,"veryHidden"){continue};target:=relmap[rf.RID];if target==""{target=fmt.Sprintf("worksheets/sheet%d.xml",i+1)};target=strings.TrimPrefix(strings.ReplaceAll(target,"\\","/"),"/");if !strings.HasPrefix(target,"xl/"){target="xl/"+target};b,e:=zipFileBytes(z,target);if e!=nil{continue};rows,e:=parseWorksheet(b,shared);if e!=nil{return nil,e};sheets=append(sheets,Sheet{Name:rf.Name,Rows:rows})} }
    if len(sheets)==0 { for _,f:=range z.File { n:=strings.ReplaceAll(f.Name,"\\","/");if strings.HasPrefix(n,"xl/worksheets/sheet")&&strings.HasSuffix(n,".xml"){r,_:=f.Open();b,_:=io.ReadAll(r);r.Close();rows,e:=parseWorksheet(b,shared);if e==nil{sheets=append(sheets,Sheet{Name:n,Rows:rows})}}};sort.Slice(sheets,func(i,j int)bool{return sheets[i].Name<sheets[j].Name}) }
    if len(sheets)==0{return nil,fmt.Errorf("XLSX sem planilhas legíveis")};return sheets,nil
}

func cell(rows [][]Cell,r,c int) Cell { if r<0||r>=len(rows)||c<0||c>=len(rows[r]){return Cell{}};return rows[r][c] }
func rowNonEmpty(rows [][]Cell,r,start,end int) bool { if r<0||r>=len(rows){return false};if end>len(rows[r]){end=len(rows[r])};for c:=start;c<end;c++{if strings.TrimSpace(rows[r][c].Value)!=""{return true}};return false }
func findYear(rows [][]Cell,name string)int{if y:=yearFrom(name);y>0{return y};lim:=20;if len(rows)<lim{lim=len(rows)};for r:=0;r<lim;r++{for _,c:=range rows[r]{if y:=yearFrom(c.Value);y>0{return y}}};return 0}
func nameHeader(v string)bool{n:=norm(v);return n=="nome"||(strings.Contains(n,"nome")&&(strings.Contains(n,"responsavel")||strings.Contains(n,"pagador")))}
func payerHeader(v string)bool{n:=norm(v);return strings.Contains(n,"cpf")&&(strings.Contains(n,"responsavel")||strings.Contains(n,"pagador"))}
func findAroundName(row []Cell,payer int)int{for c:=payer-1;c>=0&&c>=payer-10;c--{if nameHeader(row[c].Value){return c}};for c:=payer+1;c<len(row)&&c<=payer+10;c++{if nameHeader(row[c].Value){return c}};return -1}
func findContains(row []Cell,start,end int,need string,forbid string)int{if end>len(row){end=len(row)};for c:=start;c<end;c++{n:=norm(row[c].Value);if strings.Contains(n,need)&&(forbid==""||!strings.Contains(n,forbid)){return c}};return -1}
func blockContext(rows [][]Cell,header,start,end int,name string,globalYear int)(int,int){for r:=header-1;r>=0&&r>=header-5;r--{for c:=start;c<end&&c<len(rows[r]);c++{if m:=monthFrom(rows[r][c].Value);m>0{y:=yearFrom(rows[r][c].Value);if y==0{y=globalYear};return m,y}}};m:=monthFrom(name);y:=yearFrom(name);if y==0{y=globalYear};return m,y}

func parseDate(raw string,numeric bool,month,year int)(string,bool){s:=strings.TrimSpace(raw);if s==""{return "",false};for _,layout:=range []string{"2006-01-02","02/01/2006","2/1/2006"}{if t,e:=time.Parse(layout,s);e==nil{return t.Format("2006-01-02"),true}};if numeric {f,e:=strconv.ParseFloat(strings.ReplaceAll(s,",","."),64);if e==nil { if f>=1&&f<=31&&month>0&&year>0 {d:=int(math.Round(f));t:=time.Date(year,time.Month(month),d,0,0,0,0,time.UTC);if t.Month()==time.Month(month)&&t.Day()==d{return t.Format("2006-01-02"),true}}; if f>1000 {base:=time.Date(1899,12,30,0,0,0,0,time.UTC);t:=base.AddDate(0,0,int(math.Floor(f)));return t.Format("2006-01-02"),true} }};return "",false}
func parseAmount(raw string)(int64,bool){s:=strings.TrimSpace(strings.ReplaceAll(raw,"R$",""));s=strings.ReplaceAll(s," ","");if s==""{return 0,false};if strings.Contains(s,","){s=strings.ReplaceAll(s,".","");s=strings.ReplaceAll(s,",",".")};f,e:=strconv.ParseFloat(s,64);if e!=nil||f<=0{return 0,false};return int64(math.Round(f*100)),true}
func parseParty(raw string,numeric bool)(string,string,bool){d,ok:=normalizeCPF(raw,numeric);if !ok{return strings.TrimSpace(raw),d,false};name:=strings.TrimSpace(raw);for _,sep:=range []string{d,formatCPF(d)}{name=strings.ReplaceAll(name,sep,"")};name=strings.Trim(name," -–—;,:/");return name,d,true}
func formatCPF(c string)string{if len(c)!=11{return c};return c[0:3]+"."+c[3:6]+"."+c[6:9]+"-"+c[9:11]}

func previewXLSX(raw []byte) (Preview,error){sheets,e:=readXLSX(raw);if e!=nil{return Preview{},e};p:=Preview{};comp:=map[string]bool{};for _,sh:=range sheets{rows:=sh.Rows;globalYear:=findYear(rows,sh.Name);for hr,row:=range rows{var payers []int;for c,x:=range row{if payerHeader(x.Value){payers=append(payers,c)}};if len(payers)==0{continue};names:=make([]int,len(payers));for i,pc:=range payers{names[i]=findAroundName(row,pc)};for pos,pc:=range payers{start,end:=0,len(row);if len(payers)>1{if names[pos]>=0&&names[pos]<pc{start=names[pos]}else{start=pc-10;if start<0{start=0}};if pos+1<len(payers){if names[pos+1]>=0&&names[pos+1]>pc{end=names[pos+1]}else{end=payers[pos+1]}}};dateCol:=findContains(row,start,end,"data","nascimento");amountCol:=findContains(row,start,end,"valor","");if dateCol<0||amountCol<0{continue};benefCol:=findContains(row,start,end,"paciente","");if benefCol<0{benefCol=findContains(row,start,end,"beneficiario","")};m,y:=blockContext(rows,hr,start,end,sh.Name,globalYear);if m>0&&y>0{comp[fmt.Sprintf("%02d/%04d",m,y)]=true};for rr:=hr+1;rr<len(rows);rr++{if !rowNonEmpty(rows,rr,start,end){continue};total:=false;repeat:=false;for c:=start;c<end&&c<len(rows[rr]);c++{n:=norm(rows[rr][c].Value);if strings.Contains(n,"valor total mes")||strings.Contains(n,"total do mes"){total=true};if payerHeader(rows[rr][c].Value){repeat=true}};if total||repeat{break};nc:=names[pos];payerName:="";if nc>=0{payerName=cell(rows,rr,nc).Value};payerCell:=cell(rows,rr,pc);benefCell:=cell(rows,rr,benefCol);dateCell:=cell(rows,rr,dateCol);amountCell:=cell(rows,rr,amountCol);if strings.TrimSpace(payerName)==""&&strings.TrimSpace(payerCell.Value)==""&&strings.TrimSpace(benefCell.Value)==""&&strings.TrimSpace(dateCell.Value)==""&&strings.TrimSpace(amountCell.Value)==""{continue};ir:=ImportRow{SourceRow:rr+1,CompetenceMonth:m,CompetenceYear:y,PayerName:strings.TrimSpace(payerName),PayerCPFRaw:payerCell.Value,BeneficiaryCPFRaw:benefCell.Value};dateBlank:=strings.TrimSpace(dateCell.Value)=="";amountBlank:=strings.TrimSpace(amountCell.Value)=="";if dateBlank&&amountBlank{ir.Reason="Sem atendimento no período";p.Skipped=append(p.Skipped,ir);continue};cpf,cpfOK:=normalizeCPF(payerCell.Value,payerCell.Numeric);ir.PayerCPF=cpf;if ir.PayerName==""{_,nm:=extractCPFName(payerCell.Value);ir.PayerName=nm};if !cpfOK{ir.Reason="CPF do responsável ausente ou inválido";p.Restricted=append(p.Restricted,ir);continue};if ir.PayerName==""{ir.Reason="Nome do responsável ausente";p.Restricted=append(p.Restricted,ir);continue};if strings.TrimSpace(benefCell.Value)==""{ir.BeneficiaryName=ir.PayerName;ir.BeneficiaryCPF=ir.PayerCPF}else{bn,bc,bok:=parseParty(benefCell.Value,benefCell.Numeric);ir.BeneficiaryName=bn;ir.BeneficiaryCPF=bc;if !bok{ir.Reason="CPF do beneficiário inválido";p.Restricted=append(p.Restricted,ir);continue};if ir.BeneficiaryName==""{ir.BeneficiaryName=ir.PayerName}};pd,dok:=parseDate(dateCell.Value,dateCell.Numeric,m,y);if !dok{ir.Reason="Data de pagamento inválida";p.Restricted=append(p.Restricted,ir);continue};ir.PaymentDate=pd;amt,aok:=parseAmount(amountCell.Value);if !aok{ir.Reason="Valor inválido";p.Restricted=append(p.Restricted,ir);continue};ir.AmountCents=amt;p.Valid=append(p.Valid,ir)}}}}
    keys:=make([]string,0,len(comp));for k:=range comp{keys=append(keys,k)};sort.Strings(keys);p.Competences=keys;return p,nil}
func extractCPFName(raw string)(string,string){d:=onlyDigits(raw);if len(d)>=11{d=d[:11]};name:=strings.TrimSpace(raw);if len(d)==11{name=strings.ReplaceAll(name,d,"");name=strings.ReplaceAll(name,formatCPF(d),"")};return d,strings.Trim(name," -–—;,:/")}
