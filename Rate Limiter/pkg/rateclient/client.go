package rateclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	limiter "rate-limiter/internal/limiter"
)

type Client struct {
	baseURL string
	client  *http.Client
}

type CheckRequest struct {
	Identity  string        `json:"identity"`
	Algorithm string        `json:"algorithm"`
	Policy    limiter.Policy `json:"policy"`
}

type CheckResponse struct {
	Allowed    bool          `json:"allowed"`
	Remaining  int           `json:"remaining"`
	RetryAfter time.Duration `json:"retry_after"`
	ResetTime  string        `json:"reset_time"`
}

func New(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) Check(identity, algorithm string, policy limiter.Policy) (*CheckResponse, error) {
	req := CheckRequest{
		Identity:  identity,
		Algorithm: algorithm,
		Policy:    policy,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := c.baseURL + "/check"
	resp, err := c.client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("check request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var result CheckResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &result, nil
}
