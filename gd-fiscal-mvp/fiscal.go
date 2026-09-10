package main

import (
    "bytes"
    "crypto/rand"
    "encoding/csv"
    "fmt"
    "regexp"
    "strconv"
    "strings"
    "time"
)

var digitsRE = regexp.MustCompile(`\D`)

var occupationCodes = map[string]string{
    "MEDICO":                  "225",
    "ODONTOLOGO_DENTISTA":    "226",
    "FONOAUDIOLOGO":           "230",
    "FISIOTERAPEUTA":          "231",
    "TERAPEUTA_OCUPACIONAL":   "232",
    "PSICOLOGO":               "255",
}

var professionLabels = map[string]string{
    "MEDICO": "Médico",
    "ODONTOLOGO_DENTISTA": "Odontólogo/Dentista",
    "FONOAUDIOLOGO": "Fonoaudiólogo",
    "FISIOTERAPEUTA": "Fisioterapeuta",
    "TERAPEUTA_OCUPACIONAL": "Terapeuta Ocupacional",
    "PSICOLOGO": "Psicólogo",
}

func onlyDigits(s string) string { return digitsRE.ReplaceAllString(s, "") }

func validCPF(input string) bool {
    cpf := onlyDigits(input)
    if len(cpf) != 11 {
        return false
    }
    allSame := true
    for i := 1; i < 11; i++ {
        if cpf[i] != cpf[0] {
            allSame = false
            break
        }
    }
    if allSame {
        return false
    }
    for length := 9; length <= 10; length++ {
        total := 0
        for i := 0; i < length; i++ {
            total += int(cpf[i]-'0') * (length + 1 - i)
        }
        digit := (total * 10) % 11
        if digit == 10 {
            digit = 0
        }
        if digit != int(cpf[length]-'0') {
            return false
        }
    }
    return true
}

func normalizeCPF(s string) (string, error) {
    cpf := onlyDigits(s)
    if !validCPF(cpf) {
        return "", fmt.Errorf("CPF inválido")
    }
    return cpf, nil
}

func randomToken() string {
    b := make([]byte, 24)
    if _, err := rand.Read(b); err != nil {
        return fmt.Sprintf("%d", time.Now().UnixNano())
    }
    const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
    out := make([]byte, len(b))
    for i := range b {
        out[i] = alphabet[int(b[i])%len(alphabet)]
    }
    return string(out)
}

func parseBRAmount(s string) (int64, error) {
    raw := strings.TrimSpace(strings.ReplaceAll(s, "R$", ""))
    raw = strings.ReplaceAll(raw, " ", "")
    if raw == "" {
        return 0, fmt.Errorf("valor vazio")
    }
    if strings.Contains(raw, ",") {
        raw = strings.ReplaceAll(raw, ".", "")
        raw = strings.Replace(raw, ",", ".", 1)
    } else if strings.Count(raw, ".") > 1 {
        raw = strings.ReplaceAll(raw, ".", "")
    }
    f, err := strconv.ParseFloat(raw, 64)
    if err != nil || f <= 0 {
        return 0, fmt.Errorf("valor inválido")
    }
    cents := int64(f*100 + 0.5)
    if cents <= 0 {
        return 0, fmt.Errorf("valor inválido")
    }
    return cents, nil
}

func centsBR(cents int64) string {
    whole := cents / 100
    frac := cents % 100
    raw := strconv.FormatInt(whole, 10)
    var parts []string
    for len(raw) > 3 {
        parts = append([]string{raw[len(raw)-3:]}, parts...)
        raw = raw[:len(raw)-3]
    }
    parts = append([]string{raw}, parts...)
    return fmt.Sprintf("R$ %s,%02d", strings.Join(parts, "."), frac)
}

func receitaValue(cents int64) string {
    return fmt.Sprintf("%d,%02d", cents/100, cents%100)
}

func normalizeRegistry(s string) (string, error) {
    s = strings.TrimSpace(s)
    if len(s) > 15 {
        return "", fmt.Errorf("registro profissional excede 15 caracteres")
    }
    if strings.ContainsAny(s, ";\r\n\"") {
        return "", fmt.Errorf("registro profissional contém caractere incompatível")
    }
    return s, nil
}

func sanitizeDescription(s string) string {
    s = strings.TrimSpace(s)
    if s == "" {
        return "SERVIÇO DE SAÚDE"
    }
    s = strings.ReplaceAll(s, ";", ",")
    s = strings.ReplaceAll(s, "\r", " ")
    s = strings.ReplaceAll(s, "\n", " ")
    s = strings.ReplaceAll(s, `"`, "'")
    if len(s) > 255 {
        s = s[:255]
    }
    return s
}

func receitaRow(paymentDate time.Time, occupation string, amount int64, description, payerCPF, beneficiaryCPF, professionalCPF, registry string) ([]string, error) {
    if !validCPF(payerCPF) || !validCPF(beneficiaryCPF) || !validCPF(professionalCPF) {
        return nil, fmt.Errorf("CPF inválido para exportação")
    }
    ok := false
    for _, code := range occupationCodes {
        if occupation == code {
            ok = true
            break
        }
    }
    if !ok {
        return nil, fmt.Errorf("código de ocupação incompatível")
    }
    registry, err := normalizeRegistry(registry)
    if err != nil {
        return nil, err
    }
    row := []string{
        paymentDate.Format("02/01/2006"),
        "R01.001.001",
        occupation,
        receitaValue(amount),
        "",
        sanitizeDescription(description),
        "PF",
        onlyDigits(payerCPF),
        onlyDigits(beneficiaryCPF),
        "", "", "", "",
        "S",
        onlyDigits(professionalCPF),
        registry,
    }
    if len(row) != 16 {
        return nil, fmt.Errorf("linha fiscal inválida")
    }
    return row, nil
}

func rowsToCSV(rows [][]string) ([]byte, error) {
    var buf bytes.Buffer
    w := csv.NewWriter(&buf)
    w.Comma = ';'
    w.UseCRLF = true
    for _, row := range rows {
        if len(row) != 16 {
            return nil, fmt.Errorf("linha oficial deve ter 16 campos")
        }
        if err := w.Write(row); err != nil {
            return nil, err
        }
    }
    w.Flush()
    if err := w.Error(); err != nil {
        return nil, err
    }
    return buf.Bytes(), nil
}
