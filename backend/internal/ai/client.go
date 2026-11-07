// Package ai sends only masked text to an OpenAI-compatible provider.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var (
	ibanRE    = regexp.MustCompile(`(?i)\b[A-Z]{2}\d{2}(?:[ ]?[A-Z0-9]){11,30}\b`)
	accountRE = regexp.MustCompile(`\b\d{12,20}\b`)
	binRE     = regexp.MustCompile(`\b\d{12}\b`)
	nameRE    = regexp.MustCompile(`(?i)(?:seller|buyer|beneficiary|имя|покупатель|продавец)\s*[:=]\s*[^,;\n]+`)
)

// Mask replaces sensitive values before text leaves the process. Labels remain useful to the model.
func Mask(text string) string {
	text = ibanRE.ReplaceAllString(text, "[ACCOUNT]")
	text = binRE.ReplaceAllString(text, "[BIN]")
	text = accountRE.ReplaceAllString(text, "[ACCOUNT]")
	return nameRE.ReplaceAllStringFunc(text, func(s string) string {
		idx := strings.IndexAny(s, ":=")
		return s[:idx+1] + " [NAME]"
	})
}

// MaskValues replaces exact sensitive names or identifiers supplied from parsed fields.
func MaskValues(text string, values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			text = strings.ReplaceAll(text, value, "[REDACTED]")
		}
	}
	return Mask(text)
}

type Provider interface {
	Complete(context.Context, string) (string, error)
}

type Client struct {
	Primary         Provider
	Fallback        Provider
	SensitiveValues []string
}

// Explain masks the prompt before either configured provider receives it, then falls back locally.
func (c Client) Explain(ctx context.Context, prompt string) (string, error) {
	answer, _, err := c.ExplainWithSource(ctx, prompt)
	return answer, err
}

func (c Client) ExplainWithSource(ctx context.Context, prompt string) (string, string, error) {
	if c.Primary != nil {
		answer, err := c.Primary.Complete(ctx, MaskValues(prompt, c.SensitiveValues...))
		if err == nil && strings.TrimSpace(answer) != "" {
			return answer, "deepseek", nil
		}
	}
	if c.Fallback != nil {
		answer, err := c.Fallback.Complete(ctx, MaskValues(prompt, c.SensitiveValues...))
		if err == nil && strings.TrimSpace(answer) != "" {
			return answer, "fallback", nil
		}
	}
	return "", "", errors.New("AI providers unavailable")
}

type StaticProvider struct{}

func (StaticProvider) Complete(_ context.Context, prompt string) (string, error) {
	var input struct {
		Fallback string `json:"fallback"`
	}
	if err := json.Unmarshal([]byte(prompt), &input); err != nil {
		return "", err
	}
	if strings.TrimSpace(input.Fallback) == "" {
		return "", errors.New("no local explanation")
	}
	return input.Fallback, nil
}

// ChatProvider works with DeepSeek and local OpenAI-compatible endpoints.
type ChatProvider struct {
	Endpoint, APIKey, Model string
	HTTP                    *http.Client
}

func (p ChatProvider) Complete(ctx context.Context, prompt string) (string, error) {
	if p.Endpoint == "" || p.Model == "" {
		return "", errors.New("AI endpoint and model are required")
	}
	body, _ := json.Marshal(map[string]any{"model": p.Model, "messages": []map[string]string{{"role": "user", "content": prompt}}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.Endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	h := p.HTTP
	if h == nil {
		h = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := h.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", errors.New("AI provider returned HTTP " + resp.Status)
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("AI provider returned no choices")
	}
	return parsed.Choices[0].Message.Content, nil
}
