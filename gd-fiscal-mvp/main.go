package main

import (
    "context"
    "fmt"
    "html/template"
    "io"
    "net"
    "net/http"
    "os"
    "os/exec"
    "path/filepath"
    "strconv"
    "strings"
    "sync"
    "time"
)

type previewEntry struct {
    ProfessionalID int64
    SourceName string
    Preview ImportPreview
    Created time.Time
}

type App struct {
    store *Store
    csrf string
    previews map[string]previewEntry
    mu sync.Mutex
    server *http.Server
    dataDir string
}

const baseHTML = `<!doctype html><html lang="pt-BR"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>{{.Title}} · GD Soluções Fiscal Saúde</title><style>
:root{--green:#0D4E3A;--gold:#B88B30;--ink:#16221d;--muted:#66736d;--line:#d9e0dc;--bg:#f6f8f7;--danger:#8d2525;--warn:#8a5b00}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--ink);font:15px/1.45 "Segoe UI",Arial,sans-serif}header{background:#fff;border-bottom:1px solid var(--line);position:sticky;top:0;z-index:5}.head{max-width:1180px;margin:auto;padding:16px 22px;display:flex;gap:24px;align-items:center}.brand{font-weight:800;color:var(--green);font-size:20px;white-space:nowrap}.brand small{display:block;color:var(--gold);font-weight:600;font-size:11px;letter-spacing:.12em;text-transform:uppercase}nav{display:flex;gap:8px;flex-wrap:wrap}nav a{color:var(--green);text-decoration:none;padding:8px 10px;border-radius:8px;font-weight:600}nav a:hover{background:#eef5f2}main{max-width:1180px;margin:28px auto;padding:0 22px 60px}h1{font-size:28px;color:var(--green);margin:0 0 20px}h2{font-size:19px;color:var(--green);margin:0 0 14px}.panel{background:#fff;border:1px solid var(--line);border-radius:14px;padding:20px;margin:0 0 18px;box-shadow:0 2px 10px rgba(0,0,0,.025)}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(230px,1fr));gap:14px}.card{border:1px solid var(--line);border-radius:12px;padding:18px;background:#fff}.card strong{font-size:28px;color:var(--green);display:block}.muted{color:var(--muted)}.ok{padding:11px 14px;background:#eaf6f0;border:1px solid #b9ddca;color:#155c3d;border-radius:10px;margin-bottom:16px}.err{padding:11px 14px;background:#fff0f0;border:1px solid #efc1c1;color:var(--danger);border-radius:10px;margin-bottom:16px}.warn{padding:11px 14px;background:#fff8e8;border:1px solid #ead5a6;color:var(--warn);border-radius:10px;margin-bottom:16px}form.gridform{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:14px}.full{grid-column:1/-1}label{display:block;font-weight:600;color:#32433b}input,select{width:100%;margin-top:6px;border:1px solid #bdc8c2;border-radius:9px;padding:10px 11px;background:#fff;color:#16221d;font:inherit}input:focus,select:focus{outline:2px solid #bcd7cc;border-color:var(--green)}button,.btn{display:inline-block;border:0;border-radius:9px;padding:10px 14px;background:var(--green);color:#fff;font-weight:700;text-decoration:none;cursor:pointer}.btn.alt{background:#fff;color:var(--green);border:1px solid var(--green)}.actions{display:flex;gap:10px;align-items:center;flex-wrap:wrap}table{width:100%;border-collapse:collapse;font-size:14px}th,td{text-align:left;padding:10px 8px;border-bottom:1px solid #e8ecea;vertical-align:top}th{color:#43544c;font-size:12px;text-transform:uppercase;letter-spacing:.03em}tbody tr:hover{background:#fafcfb}.money{white-space:nowrap;font-variant-numeric:tabular-nums}.pill{display:inline-block;padding:3px 8px;border-radius:99px;background:#edf2ef;font-size:12px;font-weight:700}.pill.pending{background:#fff2cd;color:#755000}.pill.valid{background:#def2e8;color:#155c3d}.pill.exported{background:#e9edf8;color:#344c87}.stats{display:flex;gap:22px;flex-wrap:wrap}.stats b{color:var(--green)}.footer{margin-top:35px;color:#7c8983;font-size:12px}.dangerbutton{background:#8d2525}@media(max-width:700px){form.gridform{grid-template-columns:1fr}.full{grid-column:auto}.head{align-items:flex-start;flex-direction:column;gap:8px}table{display:block;overflow:auto}}
</style></head><body><header><div class="head"><div class="brand">GD Soluções<small>Fiscal Saúde · {{.Version}}</small></div><nav><a href="/">Início</a><a href="/professionals">Profissionais</a><a href="/import">Importar Excel</a><a href="/payments">Lançamentos</a><a href="/restrictions">Pendências</a><a href="/export">Escrituração</a></nav></div></header><main>{{.Body}}<div class="footer">Dados armazenados somente neste computador. Banco: {{.DBPath}}</div></main></body></html>`

type pageData struct{ Title,Version,DBPath string; Body template.HTML }

func (a *App) render(w http.ResponseWriter,title,body string){
    t:=template.Must(template.New("base").Parse(baseHTML));w.Header().Set("Content-Type","text/html; charset=utf-8");_ = t.Execute(w,pageData{Title:title,Version:appVersion,DBPath:filepath.Join(a.dataDir,"fiscal_saude.sqlite3"),Body:template.HTML(body)})
}

func esc(s string) string { return template.HTMLEscapeString(s) }
func hid(name,value string)string{return `<input type="hidden" name="`+esc(name)+`" value="`+esc(value)+`">`}
func csrfInput(a *App)string{return hid("csrf",a.csrf)}

func (a *App) checkPOST(w http.ResponseWriter,r *http.Request)bool{
    if r.Method!="POST"{http.Error(w,"método inválido",http.StatusMethodNotAllowed);return false}
    if err:=r.ParseMultipartForm(64<<20);err!=nil && err!=http.ErrNotMultipart{return false}
    if r.FormValue("csrf")!=a.csrf{http.Error(w,"token de segurança inválido",http.StatusBadRequest);return false}
    return true
}

func message(r *http.Request)string{
    if v:=strings.TrimSpace(r.URL.Query().Get("ok"));v!=""{return `<div class="ok">`+esc(v)+`</div>`}
    if v:=strings.TrimSpace(r.URL.Query().Get("err"));v!=""{return `<div class="err">`+esc(v)+`</div>`}
    return ""
}

func (a *App) dashboard(w http.ResponseWriter,r *http.Request){
    pros,_:=a.store.ListProfessionals();pays,_:=a.store.ListPayments(0);res,_:=a.store.ListRestrictions(0)
    pending,validated,exported:=0,0,0
    for _,p:=range pays{switch p.Status{case"PENDENTE_REVISAO":pending++;case"VALIDADO_RECEITA_SAUDE":validated++;case"EXPORTADO_CSV":exported++}}
    body:=message(r)+`<h1>Fiscal Saúde</h1><div class="grid"><div class="card"><span class="muted">Profissionais</span><strong>`+strconv.Itoa(len(pros))+`</strong></div><div class="card"><span class="muted">Pendentes de revisão</span><strong>`+strconv.Itoa(pending)+`</strong></div><div class="card"><span class="muted">Validados</span><strong>`+strconv.Itoa(validated)+`</strong></div><div class="card"><span class="muted">Pendências de importação</span><strong>`+strconv.Itoa(len(res))+`</strong></div></div><section class="panel" style="margin-top:18px"><h2>Fluxo operacional</h2><p>Cadastre o profissional, importe a planilha Excel, confira a prévia, valide os lançamentos e exporte a escrituração por profissional.</p><div class="actions"><a class="btn" href="/professionals">Cadastrar profissional</a><a class="btn alt" href="/import">Importar Excel</a></div></section>`
    _=exported
    a.render(w,"Início",body)
}

func professionalOptions(pros []Professional,selected int64)string{
    var b strings.Builder;b.WriteString(`<option value="">Selecione o profissional</option>`)
    for _,p:=range pros{sel:="";if p.ID==selected{sel=` selected`};fmt.Fprintf(&b,`<option value="%d"%s>%s — %s</option>`,p.ID,sel,esc(p.Name),esc(p.CPF))}
    return b.String()
}

func (a *App) professionals(w http.ResponseWriter,r *http.Request){
    if r.Method=="POST"{
        if !a.checkPOST(w,r){return}
        _,err:=a.store.AddProfessional(r.FormValue("name"),r.FormValue("cpf"),r.FormValue("profession"),r.FormValue("registry"),r.FormValue("state"))
        if err!=nil{http.Redirect(w,r,"/professionals?err="+urlQuery(err.Error()),http.StatusSeeOther);return}
        http.Redirect(w,r,"/professionals?ok="+urlQuery("Profissional cadastrado e salvo."),http.StatusSeeOther);return
    }
    pros,_:=a.store.ListProfessionals();var rows strings.Builder
    for _,p:=range pros{fmt.Fprintf(&rows,`<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s/%s</td></tr>`,esc(p.Name),esc(p.CPF),esc(professionLabels[p.ProfessionKey]),esc(p.OccupationCode),esc(p.Registry),esc(p.RegistryState))}
    body:=message(r)+`<h1>Profissionais</h1><section class="panel"><h2>Novo profissional</h2><form method="post" class="gridform">`+csrfInput(a)+`<label>Nome<input name="name" required></label><label>CPF<input name="cpf" inputmode="numeric" required></label><label>Profissão<select name="profession"><option value="PSICOLOGO">Psicólogo</option><option value="MEDICO">Médico</option><option value="ODONTOLOGO_DENTISTA">Odontólogo/Dentista</option><option value="FONOAUDIOLOGO">Fonoaudiólogo</option><option value="FISIOTERAPEUTA">Fisioterapeuta</option><option value="TERAPEUTA_OCUPACIONAL">Terapeuta Ocupacional</option></select></label><label>Registro profissional<input name="registry" placeholder="07/12345"></label><label>UF do registro<input name="state" maxlength="2" value="RS" required></label><div class="actions full"><button type="submit">Salvar profissional</button></div></form></section><section class="panel"><h2>Profissionais cadastrados</h2><table><thead><tr><th>Nome</th><th>CPF</th><th>Profissão</th><th>Código</th><th>Registro</th></tr></thead><tbody>`+rows.String()+`</tbody></table></section>`
    a.render(w,"Profissionais",body)
}

func (a *App) importPage(w http.ResponseWriter,r *http.Request){
    pros,_:=a.store.ListProfessionals()
    if r.Method=="POST"{
        if !a.checkPOST(w,r){return}
        pid,_:=strconv.ParseInt(r.FormValue("professional_id"),10,64);if pid<=0{a.renderImportForm(w,pros,0,"Selecione o profissional antes da importação.");return}
        if _,err:=a.store.Professional(pid);err!=nil{a.renderImportForm(w,pros,0,err.Error());return}
        file,header,err:=r.FormFile("spreadsheet");if err!=nil{a.renderImportForm(w,pros,pid,"Selecione um arquivo .xlsx.");return};defer file.Close()
        if !strings.HasSuffix(strings.ToLower(header.Filename),".xlsx"){a.renderImportForm(w,pros,pid,"O arquivo precisa estar no formato .xlsx.");return}
        raw,err:=io.ReadAll(io.LimitReader(file,50<<20));if err!=nil{a.renderImportForm(w,pros,pid,err.Error());return}
        preview,err:=PreviewXLSX(raw);if err!=nil{a.renderImportForm(w,pros,pid,err.Error());return}
        token:=randomToken();a.mu.Lock();a.previews[token]=previewEntry{ProfessionalID:pid,SourceName:header.Filename,Preview:preview,Created:time.Now()};for k,v:=range a.previews{if time.Since(v.Created)>30*time.Minute{delete(a.previews,k)}};a.mu.Unlock()
        a.renderPreview(w,token,pid,header.Filename,preview);return
    }
    a.renderImportForm(w,pros,0,"")
}

func (a *App) renderImportForm(w http.ResponseWriter,pros []Professional,selected int64,errMsg string){
    alert:="";if errMsg!=""{alert=`<div class="err">`+esc(errMsg)+`</div>`}
    body:=`<h1>Importar Excel</h1>`+alert+`<section class="panel"><h2>Nova importação</h2><form method="post" enctype="multipart/form-data" class="gridform">`+csrfInput(a)+`<label>Profissional<select name="professional_id" required>`+professionalOptions(pros,selected)+`</select></label><label>Planilha Excel (.xlsx)<input type="file" name="spreadsheet" accept=".xlsx" required></label><div class="full muted">O sistema identifica automaticamente mês/ano e blocos mensais da planilha. Linhas com data e valor ambos vazios são tratadas como ausência normal de atendimento e não são importadas.</div><div class="actions full"><button type="submit">Gerar prévia</button></div></form></section>`
    a.render(w,"Importar Excel",body)
}

func (a *App) renderPreview(w http.ResponseWriter,token string,pid int64,source string,p ImportPreview){
    var vrows strings.Builder
    limit:=len(p.Valid);if limit>150{limit=150}
    for _,x:=range p.Valid[:limit]{fmt.Fprintf(&vrows,`<tr><td>%d</td><td>%02d/%d</td><td>%s</td><td>%s</td><td>%s</td><td class="money">%s</td></tr>`,x.SourceRow,x.Month,x.Year,esc(x.PayerName),esc(x.PayerCPF),x.PaymentDate.Format("02/01/2006"),centsBR(x.AmountCents))}
    var rrows strings.Builder
    for _,x:=range p.Restricted{fmt.Fprintf(&rrows,`<tr><td>%d</td><td>%02d/%d</td><td>%s</td><td>%s</td><td>%s</td></tr>`,x.SourceRow,x.Month,x.Year,esc(x.PayerName),esc(x.PayerCPFRaw),esc(x.Reason))}
    total:=int64(0);for _,x:=range p.Valid{total+=x.AmountCents}
    body:=`<h1>Prévia da importação</h1><section class="panel"><div class="stats"><span>Origem: <b>`+esc(source)+`</b></span><span>Importáveis: <b>`+strconv.Itoa(len(p.Valid))+`</b></span><span>Sem atendimento: <b>`+strconv.Itoa(len(p.Skipped))+`</b></span><span>Pendências: <b>`+strconv.Itoa(len(p.Restricted))+`</b></span><span>Erros estruturais: <b>`+strconv.Itoa(len(p.Errors))+`</b></span><span>Total importável: <b>`+centsBR(total)+`</b></span></div></section>`
    if len(p.Valid)>0{body+=`<section class="panel"><h2>Registros importáveis</h2><table><thead><tr><th>Linha</th><th>Competência</th><th>Pagador</th><th>CPF</th><th>Pagamento</th><th>Valor</th></tr></thead><tbody>`+vrows.String()+`</tbody></table></section>`}
    if len(p.Restricted)>0{body+=`<section class="panel"><h2>Pendências que serão salvas para correção</h2><div class="warn">Há movimento informado, mas algum dado impede a escrituração. Essas linhas não entram no CSV até correção e validação.</div><table><thead><tr><th>Linha</th><th>Competência</th><th>Nome</th><th>CPF informado</th><th>Restrição</th></tr></thead><tbody>`+rrows.String()+`</tbody></table></section>`}
    if len(p.Skipped)>0{body+=`<div class="ok"><b>`+strconv.Itoa(len(p.Skipped))+`</b> linha(s) sem atendimento no período foram reconhecidas e serão apenas ignoradas, sem erro.</div>`}
    body+=`<section class="panel"><form method="post" action="/import/confirm" class="actions">`+csrfInput(a)+hid("token",token)+`<button type="submit">Confirmar importação</button><a class="btn alt" href="/import">Cancelar</a></form></section>`
    _=pid;a.render(w,"Prévia da importação",body)
}

func (a *App) importConfirm(w http.ResponseWriter,r *http.Request){
    if !a.checkPOST(w,r){return};token:=r.FormValue("token");a.mu.Lock();entry,ok:=a.previews[token];if ok{delete(a.previews,token)};a.mu.Unlock();if !ok{http.Redirect(w,r,"/import?err="+urlQuery("Prévia expirada. Gere novamente."),http.StatusSeeOther);return}
    created,dups,restr,err:=a.store.ImportPreview(entry.ProfessionalID,entry.SourceName,entry.Preview);if err!=nil{http.Redirect(w,r,"/import?err="+urlQuery(err.Error()),http.StatusSeeOther);return}
    msg:=fmt.Sprintf("Importação concluída: %d lançamento(s); %d sem atendimento; %d pendência(s); %d duplicado(s) ignorado(s).",created,len(entry.Preview.Skipped),restr,dups)
    http.Redirect(w,r,"/payments?professional_id="+strconv.FormatInt(entry.ProfessionalID,10)+"&ok="+urlQuery(msg),http.StatusSeeOther)
}

func (a *App) payments(w http.ResponseWriter,r *http.Request){
    pid,_:=strconv.ParseInt(r.URL.Query().Get("professional_id"),10,64);pros,_:=a.store.ListProfessionals();rows,_:=a.store.ListPayments(pid);var tb strings.Builder
    for _,p:=range rows{klass:="pending";if p.Status=="VALIDADO_RECEITA_SAUDE"{klass="valid"}else if p.Status=="EXPORTADO_CSV"{klass="exported"};fmt.Fprintf(&tb,`<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td class="money">%s</td><td><span class="pill %s">%s</span></td></tr>`,esc(p.ProfessionalName),esc(p.PaymentDate),esc(p.PayerName),esc(p.PayerCPF),centsBR(p.AmountCents),klass,esc(p.Status))}
    body:=message(r)+`<h1>Lançamentos</h1><section class="panel"><form method="get" class="gridform"><label>Profissional<select name="professional_id">`+professionalOptions(pros,pid)+`</select></label><div class="actions" style="align-self:end"><button type="submit">Filtrar</button><a class="btn alt" href="/payments">Todos</a></div></form></section><section class="panel"><h2>Validação para Receita Saúde</h2><form method="post" action="/validate" class="gridform">`+csrfInput(a)+`<label>Profissional<select name="professional_id" required>`+professionalOptions(pros,pid)+`</select></label><label>Ano<input name="year" inputmode="numeric" value="2026" required></label><label>Mês<input name="month" type="number" min="1" max="12" value="7" required></label><div class="actions"><button type="submit">Validar lançamentos pendentes</button></div></form></section><section class="panel"><table><thead><tr><th>Profissional</th><th>Data</th><th>Pagador</th><th>CPF</th><th>Valor</th><th>Status</th></tr></thead><tbody>`+tb.String()+`</tbody></table></section>`
    a.render(w,"Lançamentos",body)
}

func (a *App) validate(w http.ResponseWriter,r *http.Request){
    if !a.checkPOST(w,r){return};pid,_:=strconv.ParseInt(r.FormValue("professional_id"),10,64);year,_:=strconv.Atoi(r.FormValue("year"));month,_:=strconv.Atoi(r.FormValue("month"));if pid<=0||year<2000||month<1||month>12{http.Redirect(w,r,"/payments?err="+urlQuery("Parâmetros inválidos."),http.StatusSeeOther);return};n,err:=a.store.ValidateMonth(pid,year,month);if err!=nil{http.Redirect(w,r,"/payments?err="+urlQuery(err.Error()),http.StatusSeeOther);return};http.Redirect(w,r,"/payments?professional_id="+strconv.FormatInt(pid,10)+"&ok="+urlQuery(fmt.Sprintf("%d lançamento(s) validado(s) para Receita Saúde.",n)),http.StatusSeeOther)
}

func (a *App) restrictions(w http.ResponseWriter,r *http.Request){
    rows,_:=a.store.ListRestrictions(0);var b strings.Builder
    for _,x:=range rows{fmt.Fprintf(&b,`<section class="panel"><h2>%s · linha %d · %02d/%d</h2><div class="warn">%s</div><form method="post" action="/restrictions/resolve" class="gridform">%s%s<label>Nome do pagador<input name="payer_name" value="%s" required></label><label>CPF do pagador<input name="payer_cpf" value="%s" required></label><label>Nome do beneficiário<input name="beneficiary_name" value="%s"></label><label>CPF do beneficiário<input name="beneficiary_cpf" value="%s"></label><label>Data do pagamento<input name="payment_date" value="%s" required></label><label>Valor<input name="amount" value="%s" required></label><div class="actions full"><button type="submit">Corrigir e criar lançamento</button></div></form></section>`,esc(x.ProfessionalName),x.SourceRow,x.Month,x.Year,esc(x.IssueReason),csrfInput(a),hid("id",strconv.FormatInt(x.ID,10)),esc(x.PayerName),esc(x.PayerCPFRaw),esc(x.BeneficiaryName),esc(x.BeneficiaryCPFRaw),esc(x.PaymentDateRaw),esc(x.AmountRaw))}
    if len(rows)==0{b.WriteString(`<div class="ok">Nenhuma pendência de importação.</div>`)}
    a.render(w,"Pendências",message(r)+`<h1>Pendências de importação</h1>`+b.String())
}

func (a *App) resolveRestriction(w http.ResponseWriter,r *http.Request){
    if !a.checkPOST(w,r){return};id,_:=strconv.ParseInt(r.FormValue("id"),10,64);payerName,payerCPF:=r.FormValue("payer_name"),r.FormValue("payer_cpf");benName,benCPF:=strings.TrimSpace(r.FormValue("beneficiary_name")),strings.TrimSpace(r.FormValue("beneficiary_cpf"));if benName==""{benName=payerName};if benCPF==""{benCPF=payerCPF};err:=a.store.ResolveRestriction(id,payerName,payerCPF,benName,benCPF,r.FormValue("payment_date"),r.FormValue("amount"));if err!=nil{http.Redirect(w,r,"/restrictions?err="+urlQuery(err.Error()),http.StatusSeeOther);return};http.Redirect(w,r,"/restrictions?ok="+urlQuery("Pendência corrigida. O lançamento foi criado como PENDENTE_REVISAO."),http.StatusSeeOther)
}

func (a *App) exportPage(w http.ResponseWriter,r *http.Request){
    pros,_:=a.store.ListProfessionals()
    if r.Method=="POST"{
        if !a.checkPOST(w,r){return};pid,_:=strconv.ParseInt(r.FormValue("professional_id"),10,64);year,_:=strconv.Atoi(r.FormValue("year"));month,_:=strconv.Atoi(r.FormValue("month"));data,count,total,err:=a.store.ExportProfessional(pid,year,month);if err!=nil{body:=`<h1>Escrituração</h1><div class="err">`+esc(err.Error())+`</div>`+a.exportForm(pros,pid,year,month);a.render(w,"Escrituração",body);return};name:=fmt.Sprintf("RECEITA_SAUDE_%04d_%02d_PROF_%d.csv",year,month,pid);w.Header().Set("Content-Type","text/csv; charset=utf-8");w.Header().Set("Content-Disposition",`attachment; filename="`+name+`"`);w.Header().Set("X-GD-Export-Rows",strconv.Itoa(count));w.Header().Set("X-GD-Export-Total-Cents",strconv.FormatInt(total,10));_,_=w.Write(data);return
    }
    a.render(w,"Escrituração",message(r)+`<h1>Escrituração Receita Saúde</h1>`+a.exportForm(pros,0,2026,7))
}

func (a *App) exportForm(pros []Professional,pid int64,year,month int)string{if year==0{year=time.Now().Year()};if month==0{month=int(time.Now().Month())};return `<section class="panel"><div class="muted" style="margin-bottom:14px">Exporta somente lançamentos <b>VALIDADO_RECEITA_SAUDE</b> do profissional e competência selecionados. CSV sem cabeçalho, separado por ponto e vírgula, 16 campos por linha.</div><form method="post" class="gridform">`+csrfInput(a)+`<label>Profissional<select name="professional_id" required>`+professionalOptions(pros,pid)+`</select></label><label>Ano<input name="year" type="number" value="`+strconv.Itoa(year)+`" required></label><label>Mês<input name="month" type="number" min="1" max="12" value="`+strconv.Itoa(month)+`" required></label><div class="actions"><button type="submit">Exportar CSV</button></div></form></section>`}

func (a *App) exit(w http.ResponseWriter,r *http.Request){if !a.checkPOST(w,r){return};a.render(w,"Encerrar",`<h1>GD Fiscal encerrado</h1><div class="ok">Os dados já estão salvos. Esta janela pode ser fechada.</div>`);go func(){time.Sleep(300*time.Millisecond);_ = a.server.Shutdown(context.Background())}()}

func urlQuery(s string)string{return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(s,"%","%25")," ","+"),"?","%3F")}

func newMux(a *App)*http.ServeMux{m:=http.NewServeMux();m.HandleFunc("/health",func(w http.ResponseWriter,r *http.Request){w.Header().Set("Content-Type","text/plain");_,_=w.Write([]byte("ok"))});m.HandleFunc("/professionals",a.professionals);m.HandleFunc("/import/confirm",a.importConfirm);m.HandleFunc("/import",a.importPage);m.HandleFunc("/payments",a.payments);m.HandleFunc("/validate",a.validate);m.HandleFunc("/restrictions/resolve",a.resolveRestriction);m.HandleFunc("/restrictions",a.restrictions);m.HandleFunc("/export",a.exportPage);m.HandleFunc("/exit",a.exit);m.HandleFunc("/",a.dashboard);return m}

func copyFile(src,dst string)error{in,err:=os.Open(src);if err!=nil{return err};defer in.Close();if err:=os.MkdirAll(filepath.Dir(dst),0755);err!=nil{return err};tmp:=dst+".new";out,err:=os.Create(tmp);if err!=nil{return err};_,cpErr:=io.Copy(out,in);closeErr:=out.Close();if cpErr!=nil{return cpErr};if closeErr!=nil{return closeErr};_ = os.Remove(dst);return os.Rename(tmp,dst)}

func samePath(a,b string)bool{aa,_:=filepath.Abs(a);bb,_:=filepath.Abs(b);return strings.EqualFold(filepath.Clean(aa),filepath.Clean(bb))}

func installAndLaunch()bool{
    if os.Getenv("GD_FISCAL_TEST_MODE")=="1"{return false}
    for _,arg:=range os.Args[1:]{if arg=="--installed"||arg=="--serve-only"{return false}}
    self,err:=os.Executable();if err!=nil{return false};base:=os.Getenv("LOCALAPPDATA");if base==""{return false};target:=filepath.Join(base,"Programs","GD Solucoes Fiscal Saude","GD-Fiscal-Saude.exe");if samePath(self,target){return false}
    if err:=copyFile(self,target);err!=nil{return false}
    _=exec.Command(target,"--installed").Start();return true
}

func openBrowser(url string){if os.Getenv("GD_FISCAL_TEST_MODE")=="1"{return};_ = exec.Command("rundll32","url.dll,FileProtocolHandler",url).Start()}

func main(){
    if installAndLaunch(){return}
    dataDir,err:=defaultDataDir();if err!=nil{panic(err)};if v:=strings.TrimSpace(os.Getenv("GD_FISCAL_DATA_DIR"));v!=""{dataDir=v}
    store,err:=OpenStore(dataDir);if err!=nil{panic(err)};defer store.Close()
    app:=&App{store:store,csrf:randomToken(),previews:map[string]previewEntry{},dataDir:dataDir}
    mux:=newMux(app);ln,err:=net.Listen("tcp","127.0.0.1:0");if err!=nil{panic(err)};addr:=ln.Addr().(*net.TCPAddr);runtimeDir:=filepath.Join(dataDir,"runtime");_ = os.MkdirAll(runtimeDir,0700);_ = os.WriteFile(filepath.Join(runtimeDir,"port.txt"),[]byte(strconv.Itoa(addr.Port)),0600)
    app.server=&http.Server{Handler:mux,ReadHeaderTimeout:10*time.Second};url:=fmt.Sprintf("http://127.0.0.1:%d/",addr.Port);openBrowser(url);err=app.server.Serve(ln);if err!=nil&&err!=http.ErrServerClosed{panic(err)}
}
