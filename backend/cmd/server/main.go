package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"vedhelper/internal/ai"
	"vedhelper/internal/api"
)

func main() {
	root := os.Getenv("BCC_FX_BACKEND_ROOT")
	if root == "" {
		var err error
		root, err = findBackendRoot()
		if err != nil {
			log.Fatal(err)
		}
	}
	addr := os.Getenv("BCC_FX_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	fmt.Printf("ВЭД-помощник API запущен: http://localhost%s (health: /health)\n", addr)
	var provider ai.Provider
	if key := os.Getenv("DEEPSEEK_API_KEY"); key != "" {
		endpoint := os.Getenv("DEEPSEEK_ENDPOINT")
		if endpoint == "" {
			endpoint = "https://api.deepseek.com/chat/completions"
		}
		model := os.Getenv("DEEPSEEK_MODEL")
		if model == "" {
			model = "deepseek-v4-flash"
		}
		provider = ai.ChatProvider{Endpoint: endpoint, APIKey: key, Model: model}
	} else {
		log.Print("DEEPSEEK_API_KEY не задан: объяснения будут локальными")
	}
	client := &ai.Client{Primary: provider, Fallback: ai.StaticProvider{}}
	log.Fatal(http.ListenAndServe(addr, api.Server{Root: root, AI: client}.Handler()))
}

func findBackendRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := wd; ; dir = filepath.Dir(dir) {
		if _, e := os.Stat(filepath.Join(dir, "go.mod")); e == nil {
			b, e := os.ReadFile(filepath.Join(dir, "go.mod"))
			if e == nil && strings.Contains(string(b), "module vedhelper") {
				return dir, nil
			}
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	return "", fmt.Errorf("не нашёл backend/go.mod; запустите сервер из backend/ или задайте BCC_FX_BACKEND_ROOT")
}
