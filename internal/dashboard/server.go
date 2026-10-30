package dashboard

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lazypanic999/CorePulse/internal/corepulse"
)

//go:embed web/*
var webFiles embed.FS

type Server struct {
	Client  corepulse.Client
	RunsDir string
}

func New(client corepulse.Client, runsDir string) http.Handler {
	s := &Server{Client: client, RunsDir: runsDir}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/overview", s.overview)
	mux.HandleFunc("GET /api/plan", s.plan)
	mux.HandleFunc("GET /api/runs", s.runs)
	mux.HandleFunc("POST /api/experiments/pod-crash", s.runPodCrash)
	web, _ := fs.Sub(webFiles, "web")
	mux.Handle("/", http.FileServer(http.FS(web)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopbackHost(r.Host) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Dashboard accepts loopback hosts only"})
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func loopbackHost(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) overview(w http.ResponseWriter, _ *http.Request) {
	report, err := corepulse.Discover(s.Client)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"connected":  false,
			"mode":       "preview",
			"message":    "Kubernetes lab is unavailable. Reference topology is shown; no live metrics or predictions are displayed.",
			"detail":     err.Error(),
			"checked_at": time.Now().UTC().Format(time.RFC3339),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"connected": true, "mode": "live", "report": report,
		"checked_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) plan(w http.ResponseWriter, r *http.Request) {
	plan, err := corepulse.PlanPodCrash(r.URL.Query().Get("target"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (s *Server) runs(w http.ResponseWriter, _ *http.Request) {
	entries, err := os.ReadDir(s.RunsDir)
	if os.IsNotExist(err) {
		writeJSON(w, http.StatusOK, map[string]any{"runs": []any{}})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not read run records"})
		return
	}
	type record struct {
		data    map[string]any
		started string
	}
	records := []record{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.RunsDir, entry.Name()))
		if err != nil {
			continue
		}
		var value map[string]any
		if json.Unmarshal(data, &value) == nil && value["run_id"] != nil {
			started, _ := value["started_at"].(string)
			records = append(records, record{data: value, started: started})
		}
	}
	// A local lab typically has few records; cap the response for the UI.
	for i := 0; i < len(records); i++ {
		for j := i + 1; j < len(records); j++ {
			if records[j].started > records[i].started {
				records[i], records[j] = records[j], records[i]
			}
		}
	}
	result := []map[string]any{}
	for i, record := range records {
		if i >= 20 {
			break
		}
		result = append(result, record.data)
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": result})
}

func (s *Server) runPodCrash(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "JSON request required"})
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Cross-origin request denied"})
		return
	}
	var request struct {
		Target  string `json:"target"`
		Confirm string `json:"confirm"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request"})
		return
	}
	if request.Confirm != "DELETE_ONE_LAB_POD" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Explicit confirmation is required"})
		return
	}
	if _, err := corepulse.PlanPodCrash(request.Target); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	result, err := corepulse.RunPodCrash(s.Client, request.Target)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	path := filepath.Join(s.RunsDir, fmt.Sprint(result["run_id"])+".json")
	if err := os.MkdirAll(s.RunsDir, 0755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not create run directory"})
		return
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not encode run record"})
		return
	}
	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not save run record"})
		return
	}
	result["run_record"] = path
	status := http.StatusOK
	if result["status"] != "recovered" {
		status = http.StatusAccepted
	}
	writeJSON(w, status, result)
}
