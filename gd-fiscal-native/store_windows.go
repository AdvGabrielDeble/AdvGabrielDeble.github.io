//go:build windows

package main

import (
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "os"
    "path/filepath"
    "strconv"
    "strings"
    "time"
)

type Professional struct { ID int64; Name,CPF,Registry,OccupationCode string; Active bool }
type Payment struct { ID,ProfessionalID int64; ProfessionalName,PayerName,PayerCPF,BeneficiaryName,BeneficiaryCPF,PaymentDate string; AmountCents int64; Status string }
type Restriction struct { ID,ProfessionalID int64; ProfessionalName string; Row int; Month,Year int; PayerName,PayerCPFRaw,BeneficiaryName,BeneficiaryCPFRaw,PaymentDateRaw,AmountRaw,Reason,Status string }

type Store struct { db *DB; path string }

func dataRoot() string { if v:=os.Getenv("GD_FISCAL_DATA_DIR");v!=""{return v}; base:=os.Getenv("LOCALAPPDATA");if base==""{base=os.TempDir()};return filepath.Join(base,"GD Solucoes","Fiscal Saude") }
func dbPath() string { return filepath.Join(dataRoot(),"fiscal_saude.sqlite3") }

func openStore()(*Store,error){root:=dataRoot();if err:=os.MkdirAll(root,0700);err!=nil{return nil,err};p:=dbPath();d,e:=openDB(p);if e!=nil{return nil,e};s:=&Store{db:d,path:p};if e=s.init();e!=nil{d.Close();return nil,e};return s,nil}
func (s *Store) Close(){if s!=nil&&s.db!=nil{s.db.Close()}}
func (s *Store) init()error{return s.db.Exec(`
CREATE TABLE IF NOT EXISTS app_meta(key TEXT PRIMARY KEY,value TEXT NOT NULL);
INSERT OR IGNORE INTO app_meta(key,value) VALUES('schema_version','1');
CREATE TABLE IF NOT EXISTS professionals(
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 name TEXT NOT NULL,
 cpf TEXT NOT NULL UNIQUE,
 registry TEXT NOT NULL DEFAULT '',
 occupation_code TEXT NOT NULL DEFAULT '255',
 active INTEGER NOT NULL DEFAULT 1,
 created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS payments(
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 professional_id INTEGER NOT NULL REFERENCES professionals(id),
 payer_name TEXT NOT NULL,
 payer_cpf TEXT NOT NULL,
 beneficiary_name TEXT NOT NULL,
 beneficiary_cpf TEXT NOT NULL,
 payment_date TEXT NOT NULL,
 amount_cents INTEGER NOT NULL,
 description TEXT NOT NULL DEFAULT 'Atendimento em saúde',
 status TEXT NOT NULL DEFAULT 'PENDENTE_REVISAO',
 source_hash TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_payments_prof_date ON payments(professional_id,payment_date,status);
CREATE TABLE IF NOT EXISTS import_restrictions(
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 professional_id INTEGER NOT NULL REFERENCES professionals(id),
 source_name TEXT NOT NULL DEFAULT '',
 source_row INTEGER,
 competence_month INTEGER,
 competence_year INTEGER,
 payer_name TEXT NOT NULL DEFAULT '',
 payer_cpf_raw TEXT NOT NULL DEFAULT '',
 beneficiary_name TEXT NOT NULL DEFAULT '',
 beneficiary_cpf_raw TEXT NOT NULL DEFAULT '',
 payment_date_raw TEXT NOT NULL DEFAULT '',
 amount_raw TEXT NOT NULL DEFAULT '',
 issue_reason TEXT NOT NULL DEFAULT '',
 status TEXT NOT NULL DEFAULT 'PENDENTE',
 created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
 resolved_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_restrictions_prof ON import_restrictions(professional_id,status,competence_year,competence_month);
`)}

func (s *Store) AddProfessional(name,cpf,registry string)(int64,error){name=strings.TrimSpace(name);cpf=onlyDigits(cpf);registry=strings.TrimSpace(registry);if name==""{return 0,fmt.Errorf("nome do profissional é obrigatório")};if !cpfValid(cpf){return 0,fmt.Errorf("CPF do profissional inválido")};if err:=execBind(s.db,"INSERT INTO professionals(name,cpf,registry,occupation_code,active) VALUES(?,?,?,'255',1)",name,cpf,registry);err!=nil{return 0,err};return s.db.LastInsertID(),nil}
func (s *Store) Professionals()([]Professional,error){rows,e:=queryRows(s.db,"SELECT id,name,cpf,registry,occupation_code,active FROM professionals ORDER BY active DESC,name");if e!=nil{return nil,e};out:=make([]Professional,0,len(rows));for _,r:=range rows{if len(r)<6{continue};id,_:=strconv.ParseInt(r[0].(string),10,64);out=append(out,Professional{ID:id,Name:r[1].(string),CPF:r[2].(string),Registry:r[3].(string),OccupationCode:r[4].(string),Active:r[5].(string)!="0"})};return out,nil}
func (s *Store) Professional(id int64)(Professional,error){rows,e:=queryRows(s.db,"SELECT id,name,cpf,registry,occupation_code,active FROM professionals WHERE id=?",id);if e!=nil||len(rows)==0{if e!=nil{return Professional{},e};return Professional{},fmt.Errorf("profissional não encontrado")};r:=rows[0];return Professional{ID:id,Name:r[1].(string),CPF:r[2].(string),Registry:r[3].(string),OccupationCode:r[4].(string),Active:r[5].(string)!="0"},nil}

func fiscalFingerprint(prof int64,r ImportRow)string{raw:=fmt.Sprintf("%d\x1f%s\x1f%s\x1f%s\x1f%d",prof,r.PayerCPF,r.BeneficiaryCPF,r.PaymentDate,r.AmountCents);h:=sha256.Sum256([]byte(raw));return hex.EncodeToString(h[:])}
func (s *Store) Import(prof int64,source string,p Preview)(created,restricted,skippedDup int,error error){if _,e:=s.Professional(prof);e!=nil{return 0,0,0,e};if e:=s.db.Exec("BEGIN IMMEDIATE");e!=nil{return 0,0,0,e};ok:=false;defer func(){if !ok{_ = s.db.Exec("ROLLBACK")}}();for _,r:=range p.Valid{hash:=fiscalFingerprint(prof,r);rows,e:=queryRows(s.db,"SELECT id FROM payments WHERE professional_id=? AND source_hash=? AND status!='CANCELADO' LIMIT 1",prof,hash);if e!=nil{return 0,0,0,e};if len(rows)>0{skippedDup++;continue};e=execBind(s.db,`INSERT INTO payments(professional_id,payer_name,payer_cpf,beneficiary_name,beneficiary_cpf,payment_date,amount_cents,description,status,source_hash) VALUES(?,?,?,?,?,?,?,'Atendimento em saúde','PENDENTE_REVISAO',?)`,prof,r.PayerName,r.PayerCPF,r.BeneficiaryName,r.BeneficiaryCPF,r.PaymentDate,r.AmountCents,hash);if e!=nil{return 0,0,0,e};created++};for _,r:=range p.Restricted{e:=execBind(s.db,`INSERT INTO import_restrictions(professional_id,source_name,source_row,competence_month,competence_year,payer_name,payer_cpf_raw,beneficiary_name,beneficiary_cpf_raw,payment_date_raw,amount_raw,issue_reason,status) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,'PENDENTE')`,prof,source,r.SourceRow,r.CompetenceMonth,r.CompetenceYear,r.PayerName,r.PayerCPFRaw,r.BeneficiaryName,r.BeneficiaryCPFRaw,r.PaymentDate,fmt.Sprintf("%.2f",float64(r.AmountCents)/100),r.Reason);if e!=nil{return 0,0,0,e};restricted++};if e:=s.db.Exec("COMMIT");e!=nil{return 0,0,0,e};ok=true;return}

func (s *Store) Payments(prof int64)([]Payment,error){sql:=`SELECT p.id,p.professional_id,pr.name,p.payer_name,p.payer_cpf,p.beneficiary_name,p.beneficiary_cpf,p.payment_date,p.amount_cents,p.status FROM payments p JOIN professionals pr ON pr.id=p.professional_id`;var args []any;if prof>0{sql+=" WHERE p.professional_id=?";args=append(args,prof)};sql+=" ORDER BY p.payment_date DESC,p.id DESC";rows,e:=queryRows(s.db,sql,args...);if e!=nil{return nil,e};out:=make([]Payment,0,len(rows));for _,r:=range rows{if len(r)<10{continue};id,_:=strconv.ParseInt(r[0].(string),10,64);pid,_:=strconv.ParseInt(r[1].(string),10,64);amt,_:=strconv.ParseInt(r[8].(string),10,64);out=append(out,Payment{ID:id,ProfessionalID:pid,ProfessionalName:r[2].(string),PayerName:r[3].(string),PayerCPF:r[4].(string),BeneficiaryName:r[5].(string),BeneficiaryCPF:r[6].(string),PaymentDate:r[7].(string),AmountCents:amt,Status:r[9].(string)})};return out,nil}
func (s *Store) Validate(prof int64,month,year int)(int64,error){prefix:=fmt.Sprintf("%04d-%02d-%%",year,month);st,e:=s.db.Prepare("UPDATE payments SET status='VALIDADO_RECEITA_SAUDE' WHERE professional_id=? AND payment_date LIKE ? AND status='PENDENTE_REVISAO'");if e!=nil{return 0,e};defer st.Finalize();_ = st.BindInt64(1,prof);_ = st.BindText(2,prefix);if _,e=st.Step();e!=nil{return 0,e};return s.db.Changes(),nil}
func (s *Store) Export(prof int64,month,year int)(string,int,int64,error){pr,e:=s.Professional(prof);if e!=nil{return "",0,0,e};prefix:=fmt.Sprintf("%04d-%02d-%%",year,month);rows,e:=queryRows(s.db,`SELECT id,payer_cpf,beneficiary_cpf,payment_date,amount_cents,description FROM payments WHERE professional_id=? AND payment_date LIKE ? AND status='VALIDADO_RECEITA_SAUDE' ORDER BY payment_date,id LIMIT 1000`,prof,prefix);if e!=nil{return "",0,0,e};if len(rows)==0{return "",0,0,fmt.Errorf("nenhum lançamento validado para a competência")};var b strings.Builder;var ids []int64;var total int64;for _,r:=range rows{id,_:=strconv.ParseInt(r[0].(string),10,64);amt,_:=strconv.ParseInt(r[4].(string),10,64);date:=r[3].(string);t,_:=time.Parse("2006-01-02",date);value:=fmt.Sprintf("%.2f",float64(amt)/100);value=strings.ReplaceAll(value,".",",");desc:=cleanCSV(r[5].(string));fields:=[]string{t.Format("02/01/2006"),"R01.001.001",pr.OccupationCode,value,"",desc,"PF",r[1].(string),r[2].(string),"","","","","S",pr.CPF,cleanCSV(pr.Registry)};b.WriteString(strings.Join(fields,";"));b.WriteString("\r\n");ids=append(ids,id);total+=amt};if e:=s.db.Exec("BEGIN IMMEDIATE");e!=nil{return "",0,0,e};ok:=false;defer func(){if !ok{_ = s.db.Exec("ROLLBACK")}}();for _,id:=range ids{if e:=execBind(s.db,"UPDATE payments SET status='EXPORTADO_CSV' WHERE id=? AND status='VALIDADO_RECEITA_SAUDE'",id);e!=nil{return "",0,0,e}};if e:=s.db.Exec("COMMIT");e!=nil{return "",0,0,e};ok=true;return b.String(),len(rows),total,nil}
func cleanCSV(s string)string{s=strings.ReplaceAll(s,";",",");s=strings.ReplaceAll(s,"\r"," ");s=strings.ReplaceAll(s,"\n"," ");s=strings.ReplaceAll(s,"\"","'");return strings.TrimSpace(s)}

func (s *Store) Restrictions()([]Restriction,error){rows,e:=queryRows(s.db,`SELECT r.id,r.professional_id,p.name,r.source_row,r.competence_month,r.competence_year,r.payer_name,r.payer_cpf_raw,r.beneficiary_name,r.beneficiary_cpf_raw,r.payment_date_raw,r.amount_raw,r.issue_reason,r.status FROM import_restrictions r JOIN professionals p ON p.id=r.professional_id WHERE r.status='PENDENTE' ORDER BY r.id DESC`);if e!=nil{return nil,e};out:=[]Restriction{};for _,r:=range rows{id,_:=strconv.ParseInt(r[0].(string),10,64);pid,_:=strconv.ParseInt(r[1].(string),10,64);row,_:=strconv.Atoi(r[3].(string));m,_:=strconv.Atoi(r[4].(string));y,_:=strconv.Atoi(r[5].(string));out=append(out,Restriction{ID:id,ProfessionalID:pid,ProfessionalName:r[2].(string),Row:row,Month:m,Year:y,PayerName:r[6].(string),PayerCPFRaw:r[7].(string),BeneficiaryName:r[8].(string),BeneficiaryCPFRaw:r[9].(string),PaymentDateRaw:r[10].(string),AmountRaw:r[11].(string),Reason:r[12].(string),Status:r[13].(string)})};return out,nil}
func (s *Store) ResolveRestriction(id int64,payerName,payerCPF,benefName,benefCPF,dateRaw,amountRaw string,same bool)error{payerName=strings.TrimSpace(payerName);payerCPF=onlyDigits(payerCPF);if payerName==""||!cpfValid(payerCPF){return fmt.Errorf("nome/CPF do pagador inválidos")};if same{benefName=payerName;benefCPF=payerCPF}else{benefName=strings.TrimSpace(benefName);benefCPF=onlyDigits(benefCPF);if benefName==""||!cpfValid(benefCPF){return fmt.Errorf("nome/CPF do beneficiário inválidos")}};pd,ok:=parseDate(dateRaw,false,0,0);if !ok{return fmt.Errorf("data inválida")};amt,ok:=parseAmount(amountRaw);if !ok{return fmt.Errorf("valor inválido")};rows,e:=queryRows(s.db,"SELECT professional_id FROM import_restrictions WHERE id=? AND status='PENDENTE'",id);if e!=nil||len(rows)==0{if e!=nil{return e};return fmt.Errorf("pendência não localizada")};prof,_:=strconv.ParseInt(rows[0][0].(string),10,64);ir:=ImportRow{PayerName:payerName,PayerCPF:payerCPF,BeneficiaryName:benefName,BeneficiaryCPF:benefCPF,PaymentDate:pd,AmountCents:amt};hash:=fiscalFingerprint(prof,ir);if e:=s.db.Exec("BEGIN IMMEDIATE");e!=nil{return e};success:=false;defer func(){if !success{_ = s.db.Exec("ROLLBACK")}}();if e:=execBind(s.db,`INSERT INTO payments(professional_id,payer_name,payer_cpf,beneficiary_name,beneficiary_cpf,payment_date,amount_cents,description,status,source_hash) VALUES(?,?,?,?,?,?,?,'Atendimento em saúde','PENDENTE_REVISAO',?)`,prof,payerName,payerCPF,benefName,benefCPF,pd,amt,hash);e!=nil{return e};if e:=execBind(s.db,"UPDATE import_restrictions SET status='RESOLVIDO',resolved_at=CURRENT_TIMESTAMP WHERE id=?",id);e!=nil{return e};if e:=s.db.Exec("COMMIT");e!=nil{return e};success=true;return nil}

func (s *Store) Counts()(int,int,int,error){p,e:=queryRows(s.db,"SELECT COUNT(*) FROM professionals WHERE active=1");if e!=nil{return 0,0,0,e};q,e:=queryRows(s.db,"SELECT COUNT(*) FROM payments");if e!=nil{return 0,0,0,e};r,e:=queryRows(s.db,"SELECT COUNT(*) FROM import_restrictions WHERE status='PENDENTE'");if e!=nil{return 0,0,0,e};a,_:=strconv.Atoi(p[0][0].(string));b,_:=strconv.Atoi(q[0][0].(string));c,_:=strconv.Atoi(r[0][0].(string));return a,b,c,nil}
