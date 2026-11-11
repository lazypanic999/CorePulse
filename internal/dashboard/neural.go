package dashboard

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/lazypanic999/CorePulse/internal/collection"
)

func (s *Server) neuralStatus(w http.ResponseWriter, r *http.Request) {
	if s.Neural == nil {
		writeJSON(w, http.StatusOK, map[string]any{"available": false, "state": "disabled", "phase": "Start the dashboard with --ml-python pointing to the locked Python 3.12 environment."})
		return
	}
	writeJSON(w, http.StatusOK, s.Neural.Snapshot())
}
func neuralRequest(w http.ResponseWriter, r *http.Request, value any) error {
	if r.Header.Get("Content-Type") != "application/json" {
		return fmt.Errorf("JSON request required")
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
		return fmt.Errorf("cross-origin request denied")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 512)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("unexpected trailing request content")
	}
	return nil
}
func (s *Server) neuralStart(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Confirm string `json:"confirm"`
	}
	if err := neuralRequest(w, r, &request); err != nil || request.Confirm != "TRAIN_SYNTHETIC" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Explicit synthetic-training confirmation and a same-origin JSON request are required."})
		return
	}
	if s.Neural == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Neural worker is not configured"})
		return
	}
	ownership := collection.InspectOwnership()
	if !ownership.Available || ownership.Locked {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Wait until collection ownership is free before CPU training."})
		return
	}
	job, err := s.Neural.Start()
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}
func (s *Server) neuralPredict(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Job    string `json:"job_id"`
		Model  string `json:"model"`
		Sample *int   `json:"sample"`
	}
	if err := neuralRequest(w, r, &request); err != nil || request.Sample == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid same-origin prediction request"})
		return
	}
	if s.Neural == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Neural worker is not configured"})
		return
	}
	result, sample, err := s.Neural.Predict(request.Job, request.Model, *request.Sample)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": result, "sample": sample, "model": request.Model, "job_id": request.Job, "runtime": "pure Go; no Python inference process"})
}
