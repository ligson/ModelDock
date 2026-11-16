package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestJSONResponseEnvelope(t *testing.T) {
	h := (&Server{}).Handler()
	for _, tc := range []struct {
		name        string
		path        string
		method      string
		wantStatus  int
		wantSuccess bool
	}{
		{"success", "/api/health", http.MethodGet, http.StatusOK, true},
		{"error", "/api/generate", http.MethodPost, http.StatusBadRequest, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.path == "/api/generate" {
				req = httptest.NewRequest(tc.method, tc.path, httptest.NewRequest(http.MethodGet, "/", nil).Body)
			}
			resp := httptest.NewRecorder()
			h.ServeHTTP(resp, req)
			if resp.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.Code, tc.wantStatus)
			}
			var body APIResponse
			if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode envelope: %v; body=%s", err, resp.Body.String())
			}
			if body.Success != tc.wantSuccess {
				t.Fatalf("success = %v, want %v", body.Success, tc.wantSuccess)
			}
			if tc.wantSuccess && body.ErrorMsg != "" {
				t.Fatalf("unexpected errorMsg: %q", body.ErrorMsg)
			}
		})
	}
}
