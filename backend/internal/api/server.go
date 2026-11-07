package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vedhelper/internal/ai"
	"vedhelper/internal/model"
	"vedhelper/internal/parse"
	"vedhelper/internal/refdata"
	"vedhelper/internal/rules"
)

const maxUpload = 15 << 20

type Server struct {
	Root string
	AI   *ai.Client
}

type Analysis struct {
	Documents       []model.DocumentResult `json:"documents"`
	ContractReports []model.ContractReport `json:"contractReports"`
	PaymentReport   model.PaymentReport    `json:"paymentReport"`
}

func (s Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /api/demo", s.demo)
	mux.HandleFunc("GET /api/reference", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"fields": refdata.Fields(), "accounts": refdata.Accounts(), "countries": refdata.Countries(), "knp": refdata.Knp()})
	})
	mux.HandleFunc("POST /api/check", s.check)
	mux.HandleFunc("POST /api/analyze", s.analyze)
	mux.HandleFunc("POST /api/ai/explain", s.explain)
	return cors(mux)
}

func (s Server) explain(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RuleID string `json:"ruleId"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || req.RuleID == "" {
		writeError(w, 400, "Укажите ruleId")
		return
	}
	// Build AI context from a known rule ID only. Document text, quotes, names and account data never enter this request.
	var data struct {
		Rules map[string]string `json:"rules"`
	}
	_ = json.Unmarshal(refdata.Raw("fallback"), &data)
	rule, ok := data.Rules[req.RuleID]
	if !ok {
		writeError(w, 404, "Правило не найдено")
		return
	}
	fallback := rule
	prompt, _ := json.Marshal(map[string]string{"instruction": "Кратко объясни по-русски смысл банковской проверки и следующий шаг. Не выдумывай факты.", "ruleId": req.RuleID, "fallback": fallback})
	if s.AI == nil {
		writeJSON(w, 200, map[string]string{"ruleId": req.RuleID, "explanation": fallback, "source": "fallback"})
		return
	}
	answer, source, err := s.AI.ExplainWithSource(r.Context(), string(prompt))
	if err != nil {
		writeJSON(w, 200, map[string]string{"ruleId": req.RuleID, "explanation": fallback, "source": "fallback"})
		return
	}
	writeJSON(w, 200, map[string]string{"ruleId": req.RuleID, "explanation": answer, "source": source})
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s Server) demo(w http.ResponseWriter, r *http.Request) {
	paths := []string{"A/contract.docx", "A/invoice.pdf"}
	docs := make([]model.ParsedDoc, 0, len(paths))
	for _, rel := range paths {
		b, err := os.ReadFile(filepath.Join(s.Root, "samples", rel))
		if err != nil {
			writeError(w, 500, "Не удалось прочитать демонстрационные документы")
			return
		}
		doc, err := parse.ParseFile(b, filepath.Base(rel), rel)
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		docs = append(docs, doc)
	}
	input := model.CaseInput{Docs: docs, Today: time.Now().Format("2006-01-02"), ValueDate: time.Now().AddDate(0, 0, 2).Format("2006-01-02"), Task: map[string]string{"timing": "prepay"}}
	a, err := buildAnalysis(input)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, a)
}

func (s Server) check(w http.ResponseWriter, r *http.Request) {
	var input model.CaseInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxUpload)).Decode(&input); err != nil {
		writeError(w, 400, "Некорректный JSON: "+err.Error())
		return
	}
	if input.Today == "" {
		input.Today = time.Now().Format("2006-01-02")
	}
	a, err := buildAnalysis(input)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, a)
}

func (s Server) analyze(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	if err := r.ParseMultipartForm(maxUpload); err != nil {
		writeError(w, 400, "Не удалось прочитать файлы: "+err.Error())
		return
	}
	files := r.MultipartForm.File["documents"]
	if len(files) == 0 {
		writeError(w, 400, "Добавьте хотя бы один документ в поле documents")
		return
	}
	if len(files) > 10 {
		writeError(w, 400, "Можно загрузить не более 10 документов за раз")
		return
	}
	input := model.CaseInput{Today: time.Now().Format("2006-01-02"), ValueDate: time.Now().AddDate(0, 0, 2).Format("2006-01-02")}
	input.AccountingNumber = r.FormValue("accountingNumber")
	if v := r.FormValue("valueDate"); v != "" {
		input.ValueDate = v
	}
	if v := r.FormValue("today"); v != "" {
		input.Today = v
	}
	if v := r.FormValue("form"); v != "" {
		if err := json.Unmarshal([]byte(v), &input.Form); err != nil {
			writeError(w, 400, "Поле form должно быть JSON")
			return
		}
	}
	if v := r.FormValue("task"); v != "" {
		if err := json.Unmarshal([]byte(v), &input.Task); err != nil {
			writeError(w, 400, "Поле task должно быть JSON")
			return
		}
	}
	for _, header := range files {
		if header.Size > maxUpload {
			writeError(w, 413, "Размер файла превышает 15 МБ")
			return
		}
		f, err := header.Open()
		if err != nil {
			writeError(w, 400, "Не удалось открыть документ")
			return
		}
		b, readErr := io.ReadAll(io.LimitReader(f, maxUpload+1))
		_ = f.Close()
		if readErr != nil {
			writeError(w, 400, "Не удалось прочитать документ")
			return
		}
		if len(b) > maxUpload {
			writeError(w, 413, "Размер файла превышает 15 МБ")
			return
		}
		doc, err := parse.ParseFile(b, filepath.Base(header.Filename), filepath.Base(header.Filename))
		if err != nil {
			writeError(w, 400, header.Filename+": "+err.Error())
			return
		}
		input.Docs = append(input.Docs, doc)
	}
	a, err := buildAnalysis(input)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, a)
}

func buildAnalysis(input model.CaseInput) (Analysis, error) {
	if len(input.Docs) == 0 {
		return Analysis{}, errors.New("добавьте документы для проверки")
	}
	a := Analysis{Documents: []model.DocumentResult{}, ContractReports: []model.ContractReport{}}
	for _, doc := range input.Docs {
		if strings.TrimSpace(doc.Text) == "" {
			continue
		}
		contract := rules.CheckContract(doc)
		a.Documents = append(a.Documents, model.DocumentResult{Doc: doc, Extraction: contract.Extraction, Clauses: contract.Clauses})
		a.ContractReports = append(a.ContractReports, contract)
	}
	if len(a.Documents) == 0 {
		return Analysis{}, errors.New("не удалось извлечь текст из загруженных документов")
	}
	a.PaymentReport = rules.CheckPayment(input)
	return a, nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
