package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIRoutesRequireBearerSecret(t *testing.T) {
	server := Server{APISecret: "test-secret"}
	for _, test := range []struct {
		name          string
		authorization string
		status        int
	}{
		{"missing", "", http.StatusUnauthorized},
		{"invalid", "Bearer wrong-secret", http.StatusUnauthorized},
		{"valid", "Bearer test-secret", http.StatusNoContent},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/mailboxes", nil)
			request.Header.Set("Authorization", test.authorization)
			response := httptest.NewRecorder()
			server.requireAPISecret(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			})).ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
		})
	}
}
