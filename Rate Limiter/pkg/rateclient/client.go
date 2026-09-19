package rateclient // rateclient: HTTP client for remote rate limiter checks

import ( // import: standard library imports
	"bytes" // bytes: byte slice conversion for request body
	"encoding/json" // json: marshal/unmarshal CheckRequest/CheckResponse
	"fmt" // fmt: error formatting
	"net/http" // http: HTTP client for /check endpoint
	"time" // time: HTTP client timeout duration

	limiter "rate-limiter/internal/limiter" // limiter: Policy type and RateLimiter interface
)

// Client is an HTTP client that communicates with a rate limiter server. // Client: sends Check requests via HTTP POST
type Client struct { // Client: holds base URL and HTTP client configuration
	baseURL string // baseURL: server address (e.g., "http://localhost:8080")
	client  *http.Client // client: HTTP client with timeout
}

// CheckRequest is the JSON payload sent to the /check endpoint. // CheckRequest: identity, algorithm, and policy
type CheckRequest struct { // CheckRequest: serialized as JSON in POST body
	Identity  string        `json:"identity"` // Identity: who/what is making the request (e.g., "create:alice")
	Algorithm string        `json:"algorithm"` // Algorithm: which rate limiting algorithm to use (e.g., "fixed_window")
	Policy    limiter.Policy `json:"policy"` // Policy: limit and window duration
}

// CheckResponse is the JSON response from the /check endpoint. // CheckResponse: allow/deny decision with metadata
type CheckResponse struct { // CheckResponse: parsed from JSON response body
	Allowed    bool          `json:"allowed"` // Allowed: whether the request is permitted
	Remaining  int           `json:"remaining"` // Remaining: requests left in current window
	RetryAfter time.Duration `json:"retry_after"` // RetryAfter: seconds to wait if denied
	ResetTime  string        `json:"reset_time"` // ResetTime: ISO timestamp when window resets
}

// New creates a new Client with a 10-second HTTP timeout. // New: constructor with default timeout
func New(baseURL string) *Client { // New: returns configured Client pointer
	return &Client{ // return: new Client instance
		baseURL: baseURL, // baseURL: server address (no trailing slash expected)
		client:  &http.Client{Timeout: 10 * time.Second}, // client: 10-second timeout for all requests
	}
}

// Check sends a rate limit check request to the server. // Check: synchronous HTTP POST to /check
func (c *Client) Check(identity, algorithm string, policy limiter.Policy) (*CheckResponse, error) { // Check: returns server decision or error
	req := CheckRequest{ // req: build JSON request body
		Identity:  identity, // Identity: caller identity
		Algorithm: algorithm, // Algorithm: limiter algorithm name
		Policy:    policy, // Policy: rate limit configuration
	}

	body, err := json.Marshal(req) // body: serialize request to JSON bytes
	if err != nil { // if JSON marshal fails (programming error)
		return nil, fmt.Errorf("marshal request: %w", err) // wrap and return error
	}

	url := c.baseURL + "/check" // url: construct full endpoint URL
	resp, err := c.client.Post(url, "application/json", bytes.NewReader(body)) // POST JSON to server
	if err != nil { // if network/connection error
		return nil, fmt.Errorf("check request: %w", err) // wrap and return error
	}
	defer resp.Body.Close() // defer: ensure response body is always closed

	if resp.StatusCode != http.StatusOK { // if server returned error status
		return nil, fmt.Errorf("server returned %d", resp.StatusCode) // return error with status code
	}

	var result CheckResponse // result: unmarshaled response struct
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil { // if JSON decode fails
		return nil, fmt.Errorf("decode response: %w", err) // wrap and return error
	}

	return &result, nil // return: parsed response and nil error
}

/*
================================================================================
ARCHITECTURAL / ENGINEERING DECISIONS
================================================================================

1. SEPARATE CLIENT PACKAGE
   - HTTP communication isolated in its own package (rateclient).
   - Decision: URL Shortener's server.js calls POST localhost:8081/check via HTTP.
     The client is the server-side equivalent of that call pattern.
   - Separated from limiter package to avoid import cycles.

2. 10-SECOND TIMEOUT
   - All HTTP requests timeout after 10 seconds.
   - Decision: rate limit checks must be fast. 10s is generous for a local/quick check.
   - Prevents indefinite hangs if server is unresponsive.

3. SYNCHRONOUS CHECK
   - Check() blocks until server responds or times out.
   - Decision: rate limiting must happen before allowing the request.
   - Alternative: async check with cached result. Rejected — stale results risk security.

4. JSON OVER HTTP
   - Request and response are JSON-encoded.
   - Decision: standard, human-readable, language-agnostic.
   - Alternative: Protobuf (more compact). JSON is simpler and sufficient for low-volume rate checks.

5. ERROR WRAPPING (%w)
   - All errors use fmt.Errorf with %w for wrapping.
   - Decision: callers can use errors.Is() / errors.As() for specific error types.
   - Three error types: marshal error, network error, server error, decode error.

6. URL CONSTRUCTION (baseURL + "/check")
   - Endpoint path is hardcoded as "/check".
   - Decision: simple, single-endpoint client. No complex routing needed.
   - URL Shortener server uses POST /check on port 8081.

7. DEFERRED BODY CLOSE
   - resp.Body.Close() is deferred immediately after Post.
   - Decision: ensures body is always closed, even on early returns (status check, decode error).
   - Critical for connection reuse (prevents connection leaks).

8. REUSABLE HTTP CLIENT
   - http.Client is created once in New() and reused across Check() calls.
   - Decision: enables connection pooling and timeout consistency.
   - Creating a new client per request would lose connection reuse.

9. RESPONSE FIELDS
   - Allowed: decision (bool). Remaining: requests left. RetryAfter: wait time. ResetTime: window end.
   - Decision: mirrors Result struct from limiter.Check() for consistency.
   - URL Shortener server.js uses Allowed, Remaining, RetryAfter, ResetTime from response.
*/
