package api

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"vedhelper/internal/ai"
)

func TestDemoEndpoint(t *testing.T) {
	handler := Server{Root: "../.."}.Handler()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/demo", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var result Analysis
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Documents) != 2 {
		t.Fatalf("got %d demo docs, want 2", len(result.Documents))
	}
}

func TestAnalyzeUploadsDocument(t *testing.T) {
	data, err := os.ReadFile("../../samples/A/contract.docx")
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("documents", "contract.docx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/analyze", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	Server{Root: "../.."}.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var result Analysis
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Documents) != 1 {
		t.Fatalf("got %d docs, want 1", len(result.Documents))
	}
	if len(result.Documents[0].Extraction.Fields) == 0 {
		t.Fatal("expected extracted fields")
	}
}

func TestAnalyzeRequiresDocument(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/analyze", bytes.NewBufferString(""))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=none")
	rec := httptest.NewRecorder()
	Server{Root: "../.."}.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", rec.Code)
	}
}

type captureAI struct{ prompt string }

func (c *captureAI) Complete(_ context.Context, prompt string) (string, error) {
	c.prompt = prompt
	return "Объяснение", nil
}

func TestAIExplainSendsOnlyRuleContextAndFallsBack(t *testing.T) {
	provider := &captureAI{}
	server := Server{Root: "../..", AI: &ai.Client{Primary: provider, Fallback: ai.StaticProvider{}}}
	req := httptest.NewRequest(http.MethodPost, "/api/ai/explain", strings.NewReader(`{"ruleId":"iban_checksum","documentText":"Secret Person","account":"KZ123456789012345678"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(provider.prompt, "Secret Person") || strings.Contains(provider.prompt, "KZ123456789012345678") {
		t.Fatalf("sensitive values reached provider: %s", provider.prompt)
	}
	if !strings.Contains(provider.prompt, "iban_checksum") {
		t.Fatalf("rule context missing: %s", provider.prompt)
	}
	var result map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["source"] != "deepseek" || result["explanation"] != "Объяснение" {
		t.Fatalf("unexpected response: %#v", result)
	}

	local := httptest.NewRecorder()
	Server{Root: "../.."}.Handler().ServeHTTP(local, httptest.NewRequest(http.MethodPost, "/api/ai/explain", strings.NewReader(`{"ruleId":"iban_checksum"}`)))
	if local.Code != http.StatusOK || !strings.Contains(local.Body.String(), `"source":"fallback"`) {
		t.Fatalf("fallback response: %d %s", local.Code, local.Body.String())
	}
}
