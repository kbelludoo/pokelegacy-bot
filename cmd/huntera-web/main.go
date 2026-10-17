package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"bot-template-go/pkg/stealth"
)

func getAccountsStatus() []any {
	dataDir := filepath.Join("data")
	var result []any
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return result
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "status_") && strings.HasSuffix(name, ".json") {
			files = append(files, name)
		}
	}
	sort.Strings(files)
	scrub := stealth.Web()
	for i, f := range files {
		raw, err := os.ReadFile(filepath.Join(dataDir, f))
		if err != nil {
			continue
		}
		content, err := stealth.Decode(raw)
		if err != nil {
			continue
		}
		m, ok := content.(map[string]any)
		if !ok {
			continue
		}
		label := fmt.Sprintf("Caçador #%d", i+1)
		if _, ok := m["charName"].(string); ok {
			m["charName"] = label
		}
		if _, ok := m["name"].(string); ok {
			m["name"] = label
		}
		if _, ok := m["trainerName"].(string); ok {
			m["trainerName"] = label
		}
		if _, ok := m["alias"].(string); ok {
			m["alias"] = label
		}
		if _, ok := m["id"].(string); ok {
			m["id"] = fmt.Sprintf("acc%d", i+1)
		}
		result = append(result, scrub.Apply(m))
	}
	return result
}

var mimeTypes = map[string]string{
	".html": "text/html; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".js":   "application/javascript; charset=utf-8",
	".json": "application/json; charset=utf-8",
	".svg":  "image/svg+xml",
	".png":  "image/png",
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	docsDir := filepath.Join("docs")

	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		statuses := getAccountsStatus()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]any{
			"serverTime": 0,
			"accounts":   statuses,
		})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		urlPath := r.URL.Path
		if urlPath == "/" || urlPath == "" {
			urlPath = "/index.html"
		}
		filePath := filepath.Join(docsDir, filepath.Clean(urlPath))
		info, err := os.Stat(filePath)
		if err != nil || info.IsDir() {
			filePath = filepath.Join(docsDir, "index.html")
		}
		ext := strings.ToLower(filepath.Ext(filePath))
		contentType := mimeTypes[ext]
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		content, err := os.ReadFile(filePath)
		if err != nil {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, "404 Not Found")
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Write(content)
	})

	fmt.Printf("[Web Server] Dashboard rodando na porta %s: http://localhost:%s\n", port, port)
	fmt.Printf("[Web Server] API pública (anonimizada): http://localhost:%s/api/status\n", port)
	if err := http.ListenAndServe("0.0.0.0:"+port, mux); err != nil {
		fmt.Fprintf(os.Stderr, "[-] Servidor falhou: %v\n", err)
		os.Exit(1)
	}
}
