package deribit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("Deribit RPC error %d: %s", e.Code, e.Message) }

type Client struct {
	baseURL string
	key     string
	secret  string
	http    *http.Client
	nextID  atomic.Uint64
	now     func() time.Time

	authMu         sync.Mutex
	token          string
	tokenExpiresAt time.Time
	orderWSDial    WSDialFunc
}

type envelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *RPCError       `json:"error"`
}

type request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

func NewClient(baseURL, key, secret string, httpClient *http.Client) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, errors.New("invalid Deribit HTTPS base URL")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	c := &Client{baseURL: strings.TrimRight(baseURL, "/"), key: key, secret: secret, http: httpClient, now: time.Now}
	c.nextID.Store(0)
	return c, nil
}

func (c *Client) call(ctx context.Context, method string, params any, token string, result any) error {
	id := c.nextID.Add(1)
	body, err := json.Marshal(request{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return fmt.Errorf("encode Deribit request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/"+method, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build Deribit request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("Deribit HTTP transport: %w", err)
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, 4<<20)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("read Deribit response: %w", err)
	}
	var decoded envelope
	if err := json.Unmarshal(raw, &decoded); err != nil {
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("Deribit HTTP status %d", response.StatusCode)
		}
		return errors.New("malformed Deribit JSON-RPC response")
	}
	if decoded.JSONRPC != "2.0" || decoded.ID != id {
		return errors.New("mismatched Deribit JSON-RPC response")
	}
	if decoded.Error != nil {
		decoded.Error.Data = nil // server data may echo sensitive request details
		return decoded.Error
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("Deribit HTTP status %d", response.StatusCode)
	}
	if len(decoded.Result) == 0 || string(decoded.Result) == "null" {
		return errors.New("Deribit JSON-RPC response has no result")
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(decoded.Result, result); err != nil {
		return fmt.Errorf("decode Deribit result: %w", err)
	}
	return nil
}

func (c *Client) Public(ctx context.Context, method string, params any, result any) error {
	if !strings.HasPrefix(method, "public/") {
		return errors.New("public call requires public/ method")
	}
	return c.call(ctx, method, params, "", result)
}

func (c *Client) PrivateRead(ctx context.Context, method string, params any, result any) error {
	if !strings.HasPrefix(method, "private/get_") {
		return errors.New("private read call requires private/get_ method")
	}
	token, err := c.accessToken(ctx, false)
	if err != nil {
		return err
	}
	err = c.call(ctx, method, params, token, result)
	var rpcErr *RPCError
	if errors.As(err, &rpcErr) && isAuthError(rpcErr.Code) {
		token, refreshErr := c.accessToken(ctx, true)
		if refreshErr != nil {
			return refreshErr
		}
		return c.call(ctx, method, params, token, result)
	}
	return err
}

func isAuthError(code int) bool { return code == 13004 || code == 13009 }

type authResult struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

func (c *Client) accessToken(ctx context.Context, force bool) (string, error) {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	now := c.now()
	if !force && c.token != "" && now.Add(30*time.Second).Before(c.tokenExpiresAt) {
		return c.token, nil
	}
	if strings.TrimSpace(c.key) == "" || strings.TrimSpace(c.secret) == "" {
		return "", errors.New("Deribit credentials are required")
	}
	var auth authResult
	err := c.call(ctx, "public/auth", map[string]string{
		"grant_type": "client_credentials", "client_id": c.key, "client_secret": c.secret,
	}, "", &auth)
	if err != nil {
		return "", fmt.Errorf("Deribit authentication failed: %w", err)
	}
	if auth.AccessToken == "" || auth.ExpiresIn <= 0 {
		return "", errors.New("Deribit authentication returned invalid token metadata")
	}
	c.token, c.tokenExpiresAt = auth.AccessToken, now.Add(time.Duration(auth.ExpiresIn)*time.Second)
	return c.token, nil
}
