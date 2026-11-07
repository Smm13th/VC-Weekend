package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type fakeProvider struct {
	got    string
	answer string
	err    error
}

func (f *fakeProvider) Complete(_ context.Context, s string) (string, error) {
	f.got = s
	return f.answer, f.err
}

func TestExplainMasksBeforeProviderAndFallsBack(t *testing.T) {
	primary := &fakeProvider{err: errors.New("offline")}
	fallback := &fakeProvider{answer: "готово"}
	prompt := "seller: Ivan Petrov; BIN 123456789012; IBAN AE980710123456789012345 and account 1234567890123456. Omar Al-Rashidi is signatory."
	got, err := (Client{Primary: primary, Fallback: fallback, SensitiveValues: []string{"Omar Al-Rashidi"}}).Explain(context.Background(), prompt)
	if err != nil || got != "готово" {
		t.Fatalf("Explain = %q, %v", got, err)
	}
	for _, provider := range []*fakeProvider{primary, fallback} {
		for _, secret := range []string{"Ivan Petrov", "123456789012", "AE980710123456789012345", "1234567890123456", "Omar Al-Rashidi"} {
			if strings.Contains(provider.got, secret) {
				t.Errorf("provider received unmasked value %q in %q", secret, provider.got)
			}
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestChatProviderOpenAICompatible(t *testing.T) {
	h := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer key" {
			t.Errorf("missing auth")
		}
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["model"] != "test" {
			t.Errorf("model missing: %#v", in)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"ok"}}]}`)), Header: make(http.Header)}, nil
	})}
	got, err := (ChatProvider{Endpoint: "https://provider.test/chat", APIKey: "key", Model: "test", HTTP: h}).Complete(context.Background(), "hi")
	if err != nil || got != "ok" {
		t.Fatalf("Complete = %q, %v", got, err)
	}
}
