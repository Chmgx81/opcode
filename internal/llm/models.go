package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// ModelInfo is one model a provider offers, as the provider itself
// reports it — fetched live, never hardcoded.
type ModelInfo struct {
	ID          string
	DisplayName string // Anthropic reports one; OpenAI-compatible servers do not
}

// FetchModels lists a provider's models: OpenAI-compatible
// GET {base}/models for the "openai" wire API, or Anthropic's
// GET {base}/v1/models for "anthropic". Sorted by id so the picker is
// stable between fetches. apiKey may be empty for local servers.
func FetchModels(ctx context.Context, api, baseURL, apiKey string) ([]ModelInfo, error) {
	if api == "anthropic" {
		return fetchAnthropicModels(ctx, baseURL, apiKey)
	}
	return fetchOpenAIModels(ctx, baseURL, apiKey)
}

func fetchOpenAIModels(ctx context.Context, baseURL, apiKey string) ([]ModelInfo, error) {
	// OpenAI-compatible base URLs already carry the version segment
	// (…/v1); the list endpoint is {base}/models.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimSuffix(baseURL, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := getJSON(ctx, req, &body); err != nil {
		return nil, err
	}
	out := make([]ModelInfo, 0, len(body.Data))
	for _, m := range body.Data {
		if m.ID != "" {
			out = append(out, ModelInfo{ID: m.ID})
		}
	}
	sortModels(out)
	return out, nil
}

func fetchAnthropicModels(ctx context.Context, baseURL, apiKey string) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimSuffix(baseURL, "/")+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)
	var body struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if err := getJSON(ctx, req, &body); err != nil {
		return nil, err
	}
	out := make([]ModelInfo, 0, len(body.Data))
	for _, m := range body.Data {
		if m.ID != "" {
			out = append(out, ModelInfo{ID: m.ID, DisplayName: m.DisplayName})
		}
	}
	sortModels(out)
	return out, nil
}

// getJSON performs the request and decodes the response, turning
// non-2xx bodies into readable errors (the same shape the chat
// clients use). Unlike the chat clients it carries a total timeout:
// listing models is one bounded request, never a stream.
func getJSON(ctx context.Context, req *http.Request, into any) error {
	ctx, cancel := context.WithTimeout(ctx, modelsTimeout)
	defer cancel()
	req = req.WithContext(ctx)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach %s: %w", req.URL.Host, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return statusError(resp, msg)
	}
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func sortModels(models []ModelInfo) {
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
}
