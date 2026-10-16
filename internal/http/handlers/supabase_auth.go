package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type supabaseUserResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

// validateSupabaseJWT validates a Supabase Auth Bearer token and returns the user profile.
func validateSupabaseJWT(ctx context.Context, client *http.Client, supabaseURL, supabaseKey, token string) (*supabaseUserResponse, error) {
	url := fmt.Sprintf("%s/auth/v1/user", supabaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create supabase auth request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	if supabaseKey != "" {
		req.Header.Set("apikey", supabaseKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("contact supabase auth: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("supabase auth rejected token (status %d): %s", resp.StatusCode, string(body))
	}

	var user supabaseUserResponse
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("decode supabase user response: %w", err)
	}

	if user.ID == "" {
		return nil, fmt.Errorf("supabase returned empty user ID")
	}

	return &user, nil
}
