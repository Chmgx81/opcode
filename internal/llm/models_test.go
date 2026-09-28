package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchModelsOpenAIShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/models") {
			t.Errorf("path = %s, want /models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer k" {
			t.Errorf("auth = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{
			{"id": "zeta"}, {"id": "alpha"}, {"id": ""},
		}})
	}))
	defer srv.Close()

	models, err := FetchModels(context.Background(), "", srv.URL, "k")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("models = %v (empty ids must be dropped)", models)
	}
	// Sorted, so the picker is stable between fetches.
	if models[0].ID != "alpha" || models[1].ID != "zeta" {
		t.Errorf("order = %v", models)
	}
}

func TestFetchModelsAnthropicShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/models") {
			t.Errorf("path = %s, want /v1/models", r.URL.Path)
		}
		if r.Header.Get("x-api-key") == "" || r.Header.Get("anthropic-version") == "" {
			t.Error("anthropic headers missing")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{
			{"id": "claude-sonnet-4.5", "display_name": "Claude Sonnet 4.5"},
		}})
	}))
	defer srv.Close()

	models, err := FetchModels(context.Background(), "anthropic", srv.URL, "k")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "claude-sonnet-4.5" ||
		models[0].DisplayName != "Claude Sonnet 4.5" {
		t.Fatalf("models = %+v", models)
	}
}

func TestFetchModelsErrorReadable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer srv.Close()

	_, err := FetchModels(context.Background(), "", srv.URL, "bad")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("error should carry the provider message: %v", err)
	}
}
