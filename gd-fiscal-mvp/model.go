package main

import (
    "fmt"
    "os"
    "path/filepath"
    "strconv"
    "strings"
    "time"
)

const appVersion = "0.9.1-MVP"

type Professional struct {
    ID int64
    Name string
    CPF string
    ProfessionKey string
    OccupationCode string
    Registry string
    RegistryState string
    Active bool
}

type Payment struct {
    ID int64
    ProfessionalID int64
    ProfessionalName string
    PayerName string
    PayerCPF string
    BeneficiaryName string
    BeneficiaryCPF string
    PaymentDate string
    AmountCents int64
    Description string
    Status string
}

type Restriction struct {
    ID int64
    ProfessionalID int64
    ProfessionalName string
    SourceName string
    SourceRow int
    Month int
    Year int
    PayerName string
    PayerCPFRaw string
    BeneficiaryName string
    BeneficiaryCPFRaw string
    PaymentDateRaw string
    AmountRaw string
    IssueCode string
    IssueReason string
    Status string
}

type Store struct { db *DB }

func defaultDataDir() (string,error) {
    if v:=strings.TrimSpace(os.Getenv("GD_FISCAL_DATA_DIR"));v!="" { return v,nil }
    base:=strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
    if base=="" { return "",fmt.Errorf("LOCALAPPDATA não disponível") }
    return filepath.Join(base,"GD Solucoes","Fiscal Saude"),nil
}

func OpenStore(dataDir string)(*Store,error){
    if dataDir=="" { var err error; dataDir,err=defaultDataDir();if err!=nil{return nil,err} }
    if err:=os.MkdirAll(dataDir,0700);err!=nil{return nil,err}
    db,err:=OpenDB(filepath.Join(dataDir,"fiscal_saude.sqlite3"));if err!=nil{return nil,err}
    s:=&Store{db:db}
    if err:=s.initSchema();err!=nil{db.Close();return nil,err}
    return s,nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) initSchema() error {
    return s.db.ExecScript(`
PRAGMA foreign_keys=ON;
PRAGMA journal_mode=WAL;
PRAGMA synchronous=FULL;
CREATE TABLE IF NOT EXISTS app_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
INSERT OR REPLACE INTO app_meta(key,value) VALUES('schema_version','1');
INSERT OR REPLACE INTO app_meta(key,value) VALUES('app_version','0.9.1-MVP');
CREATE TABLE IF NOT EXISTS professionals (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  cpf TEXT NOT NULL UNIQUE,
  profession_key TEXT NOT NULL,
  occupation_code TEXT NOT NULL,
  registry TEXT NOT NULL DEFAULT '',
  registry_state TEXT NOT NULL DEFAULT '',
  active INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS payments (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  professional_id INTEGER NOT NULL REFERENCES professionals(id),
  payer_name TEXT NOT NULL,
  payer_cpf TEXT NOT NULL,
  beneficiary_name TEXT NOT NULL,
  beneficiary_cpf TEXT NOT NULL,
  payment_date TEXT NOT NULL,
  amount_cents INTEGER NOT NULL,
  description TEXT NOT NULL DEFAULT 'SERVIÇO DE SAÚDE',
  status TEXT NOT NULL DEFAULT 'PENDENTE_REVISAO',
  source_name TEXT NOT NULL DEFAULT '',
  source_row INTEGER,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(professional_id,payer_cpf,beneficiary_cpf,payment_date,amount_cents)
);
CREATE INDEX IF NOT EXISTS idx_payments_prof_date ON payments(professional_id,payment_date,status);
CREATE TABLE IF NOT EXISTS import_restrictions (
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
  issue_code TEXT NOT NULL DEFAULT 'DADOS_INVALIDOS',
  issue_reason TEXT NOT NULL DEFAULT '',
  raw_content TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'PENDENTE',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  resolved_at TEXT,
  resolved_payment_id INTEGER REFERENCES payments(id),
  UNIQUE(professional_id,source_name,source_row,issue_code,issue_reason,status)
);
CREATE INDEX IF NOT EXISTS idx_restrictions_prof_status ON import_restrictions(professional_id,status,competence_year,competence_month);
`)
}

func (s *Store) AddProfessional(name,cpf,professionKey,registry,state string)(int64,error){
    name=strings.TrimSpace(name);if name==""{return 0,fmt.Errorf("nome é obrigatório")}
    ncpf,err:=normalizeCPF(cpf);if err!=nil{return 0,err}
    professionKey=strings.ToUpper(strings.TrimSpace(professionKey));code:=occupationCodes[professionKey]
    if code==""{return 0,fmt.Errorf("profissão não suportada")}
    registry,err=normalizeRegistry(registry);if err!=nil{return 0,err}
    state=strings.ToUpper(strings.TrimSpace(state));if state==""{return 0,fmt.Errorf("UF do registro é obrigatória")}
    id,err:=s.db.Exec(`INSERT INTO professionals(name,cpf,profession_key,occupation_code,registry,registry_state) VALUES(?,?,?,?,?,?)`,name,ncpf,professionKey,code,registry,state)
    if err!=nil{return 0,fmt.Errorf("não foi possível cadastrar profissional: %w",err)}
    return id,nil
}

func professionalFromRow(r map[string]string) Professional {
    id,_:=strconv.ParseInt(r["id"],10,64)
    return Professional{ID:id,Name:r["name"],CPF:r["cpf"],ProfessionKey:r["profession_key"],OccupationCode:r["occupation_code"],Registry:r["registry"],RegistryState:r["registry_state"],Active:r["active"]!="0"}
}

func (s *Store) ListProfessionals()([]Professional,error){
    rows,err:=s.db.Query(`SELECT id,name,cpf,profession_key,occupation_code,registry,registry_state,active FROM professionals ORDER BY active DESC,name`);if err!=nil{return nil,err}
    out:=make([]Professional,0,len(rows));for _,r:=range rows{out=append(out,professionalFromRow(r))};return out,nil
}
func (s *Store) Professional(id int64)(Professional,error){
    r,err:=s.db.QueryOne(`SELECT id,name,cpf,profession_key,occupation_code,registry,registry_state,active FROM professionals WHERE id=?`,id);if err!=nil{return Professional{},err};if r==nil{return Professional{},fmt.Errorf("profissional não encontrado")};return professionalFromRow(r),nil
}

func (s *Store) ImportPreview(profID int64,source string,p ImportPreview)(created,duplicates,restrictions int,errorOut error){
    if _,err:=s.Professional(profID);err!=nil{return 0,0,0,err}
    if _,err:=s.db.Exec("BEGIN IMMEDIATE");err!=nil{return 0,0,0,err}
    committed:=false
    defer func(){if !committed{_,_=s.db.Exec("ROLLBACK")}}()
    for _,r:=range p.Valid{
        _,err:=s.db.Exec(`INSERT OR IGNORE INTO payments(professional_id,payer_name,payer_cpf,beneficiary_name,beneficiary_cpf,payment_date,amount_cents,description,status,source_name,source_row) VALUES(?,?,?,?,?,?,?,?, 'PENDENTE_REVISAO',?,?)`,profID,r.PayerName,r.PayerCPF,r.BeneficiaryName,r.BeneficiaryCPF,r.PaymentDate.Format("2006-01-02"),r.AmountCents,sanitizeDescription(r.Description),source,r.SourceRow)
        if err!=nil{return created,duplicates,restrictions,err}
        if s.db.Changes()==0{duplicates++}else{created++}
    }
    for _,i:=range p.Restricted{
        _,err:=s.db.Exec(`INSERT OR IGNORE INTO import_restrictions(professional_id,source_name,source_row,competence_month,competence_year,payer_name,payer_cpf_raw,beneficiary_name,beneficiary_cpf_raw,payment_date_raw,amount_raw,issue_code,issue_reason,raw_content,status) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,'PENDENTE')`,profID,source,i.SourceRow,i.Month,i.Year,i.PayerName,i.PayerCPFRaw,i.BeneficiaryName,i.BeneficiaryCPFRaw,i.DateRaw,i.AmountRaw,i.Code,i.Reason,i.Content)
        if err!=nil{return created,duplicates,restrictions,err}
        if s.db.Changes()>0{restrictions++}
    }
    if _,err:=s.db.Exec("COMMIT");err!=nil{return created,duplicates,restrictions,err}
    committed=true
    return created,duplicates,restrictions,nil
}

func (s *Store) ListPayments(profID int64)([]Payment,error){
    rows,err:=s.db.Query(`SELECT p.id,p.professional_id,pr.name AS professional_name,p.payer_name,p.payer_cpf,p.beneficiary_name,p.beneficiary_cpf,p.payment_date,p.amount_cents,p.description,p.status FROM payments p JOIN professionals pr ON pr.id=p.professional_id WHERE (?=0 OR p.professional_id=?) ORDER BY p.payment_date DESC,p.id DESC`,profID,profID);if err!=nil{return nil,err}
    out:=make([]Payment,0,len(rows));for _,r:=range rows{ id,_:=strconv.ParseInt(r["id"],10,64);pid,_:=strconv.ParseInt(r["professional_id"],10,64);amount,_:=strconv.ParseInt(r["amount_cents"],10,64);out=append(out,Payment{ID:id,ProfessionalID:pid,ProfessionalName:r["professional_name"],PayerName:r["payer_name"],PayerCPF:r["payer_cpf"],BeneficiaryName:r["beneficiary_name"],BeneficiaryCPF:r["beneficiary_cpf"],PaymentDate:r["payment_date"],AmountCents:amount,Description:r["description"],Status:r["status"]})};return out,nil
}

func (s *Store) CountAndSum(profID int64)(int64,int64,error){
    r,err:=s.db.QueryOne(`SELECT COUNT(*) AS total,COALESCE(SUM(amount_cents),0) AS amount FROM payments WHERE professional_id=?`,profID);if err!=nil{return 0,0,err};if r==nil{return 0,0,nil};t,_:=strconv.ParseInt(r["total"],10,64);a,_:=strconv.ParseInt(r["amount"],10,64);return t,a,nil
}

func (s *Store) ValidateMonth(profID int64,year,month int)(int64,error){
    prefix:=fmt.Sprintf("%04d-%02d-",year,month)
    _,err:=s.db.Exec(`UPDATE payments SET status='VALIDADO_RECEITA_SAUDE',updated_at=CURRENT_TIMESTAMP WHERE professional_id=? AND payment_date LIKE ? AND status='PENDENTE_REVISAO'`,profID,prefix+"%");if err!=nil{return 0,err};return s.db.Changes(),nil
}

func (s *Store) ListRestrictions(profID int64)([]Restriction,error){
    rows,err:=s.db.Query(`SELECT r.id,r.professional_id,p.name AS professional_name,r.source_name,r.source_row,r.competence_month,r.competence_year,r.payer_name,r.payer_cpf_raw,r.beneficiary_name,r.beneficiary_cpf_raw,r.payment_date_raw,r.amount_raw,r.issue_code,r.issue_reason,r.status FROM import_restrictions r JOIN professionals p ON p.id=r.professional_id WHERE r.status='PENDENTE' AND (?=0 OR r.professional_id=?) ORDER BY r.id DESC`,profID,profID);if err!=nil{return nil,err}
    out:=make([]Restriction,0,len(rows));for _,r:=range rows{parse:=func(k string)int64{n,_:=strconv.ParseInt(r[k],10,64);return n};out=append(out,Restriction{ID:parse("id"),ProfessionalID:parse("professional_id"),ProfessionalName:r["professional_name"],SourceName:r["source_name"],SourceRow:int(parse("source_row")),Month:int(parse("competence_month")),Year:int(parse("competence_year")),PayerName:r["payer_name"],PayerCPFRaw:r["payer_cpf_raw"],BeneficiaryName:r["beneficiary_name"],BeneficiaryCPFRaw:r["beneficiary_cpf_raw"],PaymentDateRaw:r["payment_date_raw"],AmountRaw:r["amount_raw"],IssueCode:r["issue_code"],IssueReason:r["issue_reason"],Status:r["status"]})};return out,nil
}

func (s *Store) ResolveRestriction(id int64,payerName,payerCPF,beneficiaryName,beneficiaryCPF,dateRaw,amountRaw string)error{
    issue,err:=s.db.QueryOne(`SELECT * FROM import_restrictions WHERE id=? AND status='PENDENTE'`,id);if err!=nil{return err};if issue==nil{return fmt.Errorf("pendência não localizada")}
    pcpf,err:=normalizeCPF(payerCPF);if err!=nil{return fmt.Errorf("CPF do pagador inválido")}
    bcpf,err:=normalizeCPF(beneficiaryCPF);if err!=nil{return fmt.Errorf("CPF do beneficiário inválido")}
    payerName=strings.TrimSpace(payerName);beneficiaryName=strings.TrimSpace(beneficiaryName);if payerName==""||beneficiaryName==""{return fmt.Errorf("nome do pagador e beneficiário são obrigatórios")}
    var dt time.Time
    for _,layout:=range []string{"02/01/2006","2006-01-02"}{if t,e:=time.ParseInLocation(layout,strings.TrimSpace(dateRaw),time.Local);e==nil{dt=t;break}}
    if dt.IsZero(){return fmt.Errorf("data inválida")}
    cents,err:=parseBRAmount(amountRaw);if err!=nil{return err}
    profID,_:=strconv.ParseInt(issue["professional_id"],10,64)
    if _,err:=s.db.Exec("BEGIN IMMEDIATE");err!=nil{return err};committed:=false;defer func(){if !committed{_,_=s.db.Exec("ROLLBACK")}}()
    pid,err:=s.db.Exec(`INSERT OR IGNORE INTO payments(professional_id,payer_name,payer_cpf,beneficiary_name,beneficiary_cpf,payment_date,amount_cents,description,status,source_name,source_row) VALUES(?,?,?,?,?,?,?,'SERVIÇO DE SAÚDE','PENDENTE_REVISAO',?,?)`,profID,payerName,pcpf,beneficiaryName,bcpf,dt.Format("2006-01-02"),cents,issue["source_name"],issue["source_row"]);if err!=nil{return err}
    if s.db.Changes()==0{row,e:=s.db.QueryOne(`SELECT id FROM payments WHERE professional_id=? AND payer_cpf=? AND beneficiary_cpf=? AND payment_date=? AND amount_cents=?`,profID,pcpf,bcpf,dt.Format("2006-01-02"),cents);if e!=nil{return e};if row!=nil{pid,_=strconv.ParseInt(row["id"],10,64)}}
    if _,err:=s.db.Exec(`UPDATE import_restrictions SET status='RESOLVIDO',resolved_at=CURRENT_TIMESTAMP,resolved_payment_id=? WHERE id=?`,pid,id);err!=nil{return err}
    if _,err:=s.db.Exec("COMMIT");err!=nil{return err};committed=true;return nil
}

func (s *Store) ExportProfessional(profID int64,year,month int)([]byte,int,int64,error){
    prof,err:=s.Professional(profID);if err!=nil{return nil,0,0,err}
    prefix:=fmt.Sprintf("%04d-%02d-",year,month)
    rows,err:=s.db.Query(`SELECT id,payer_cpf,beneficiary_cpf,payment_date,amount_cents,description FROM payments WHERE professional_id=? AND status='VALIDADO_RECEITA_SAUDE' AND payment_date LIKE ? ORDER BY payment_date,id`,profID,prefix+"%");if err!=nil{return nil,0,0,err}
    if len(rows)==0{return nil,0,0,fmt.Errorf("não há lançamentos validados para esta competência")};if len(rows)>1000{return nil,0,0,fmt.Errorf("máximo de 1.000 linhas por arquivo")}
    fiscal:=make([][]string,0,len(rows));ids:=make([]int64,0,len(rows));total:=int64(0)
    for _,r:=range rows{dt,err:=time.ParseInLocation("2006-01-02",r["payment_date"],time.Local);if err!=nil{return nil,0,0,err};amount,_:=strconv.ParseInt(r["amount_cents"],10,64);row,err:=receitaRow(dt,prof.OccupationCode,amount,r["description"],r["payer_cpf"],r["beneficiary_cpf"],prof.CPF,prof.Registry);if err!=nil{return nil,0,0,err};fiscal=append(fiscal,row);id,_:=strconv.ParseInt(r["id"],10,64);ids=append(ids,id);total+=amount}
    data,err:=rowsToCSV(fiscal);if err!=nil{return nil,0,0,err}
    if _,err:=s.db.Exec("BEGIN IMMEDIATE");err!=nil{return nil,0,0,err};committed:=false;defer func(){if !committed{_,_=s.db.Exec("ROLLBACK")}}()
    for _,id:=range ids{if _,err:=s.db.Exec(`UPDATE payments SET status='EXPORTADO_CSV',updated_at=CURRENT_TIMESTAMP WHERE id=?`,id);err!=nil{return nil,0,0,err}}
    if _,err:=s.db.Exec("COMMIT");err!=nil{return nil,0,0,err};committed=true
    return data,len(rows),total,nil
}
