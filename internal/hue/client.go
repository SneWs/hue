package hue

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const deviceType = "omarchy#hue"

// ErrLinkButton means the bridge is reachable but its link button is not pressed.
var ErrLinkButton = errors.New("link button not pressed")

// ErrUnauthorized means the stored application key was rejected.
var ErrUnauthorized = errors.New("application key rejected")

// Client talks to one bridge. The bridge certificate is self-signed and
// changes with the bridge identity, so verification is skipped for this
// local device only. HTTP/2 is disabled because Hue bridges misbehave with it.
type Client struct {
	IP       string
	Username string
	http     *http.Client
}

// warmTransport is shared by the long-lived hue serve process so commands
// reuse one TLS session. A Hue Bridge handshake is most of the delay of a
// one-shot command.
var warmTransport = newTransport()

func newTransport() *http.Transport {
	return &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, // local Hue Bridge presents a self-signed certificate
			MinVersion:         tls.VersionTLS12,
		},
		ForceAttemptHTTP2:     false,
		TLSNextProto:          map[string]func(string, *tls.Conn) http.RoundTripper{},
		DialContext:           (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
		ResponseHeaderTimeout: 4 * time.Second,
		MaxIdleConns:          2,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       2 * time.Minute,
	}
}

// NewClient returns a client for a bridge address, which may include a port.
func NewClient(ip, username string) *Client {
	return newClient(ip, username, newTransport())
}

// NewWarmClient returns a client that reuses the process-wide connection pool.
func NewWarmClient(ip, username string) *Client {
	return newClient(ip, username, warmTransport)
}

func newClient(ip, username string, transport *http.Transport) *Client {
	return &Client{
		IP:       ip,
		Username: username,
		http: &http.Client{
			Timeout:   6 * time.Second,
			Transport: transport,
		},
	}
}

type apiError struct {
	Type        int    `json:"type"`
	Description string `json:"description"`
}

type envelope struct {
	Errors []apiError      `json:"errors"`
	Data   json.RawMessage `json:"data"`
}

// Resources fetches every CLIP v2 resource on the bridge.
func (c *Client) Resources(ctx context.Context) ([]json.RawMessage, error) {
	body, err := c.do(ctx, http.MethodGet, "/clip/v2/resource", nil, true)
	if err != nil {
		return nil, err
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, err
	}
	if err := firstError(env.Errors); err != nil {
		return nil, err
	}
	var items []json.RawMessage
	if len(env.Data) == 0 {
		return items, nil
	}
	if err := json.Unmarshal(env.Data, &items); err != nil {
		return nil, err
	}
	return items, nil
}

// Resource fetches one resource, such as a light, so a color change can read its gamut.
func (c *Client) Resource(ctx context.Context, kind, id string) (json.RawMessage, error) {
	body, err := c.do(ctx, http.MethodGet, "/clip/v2/resource/"+kind+"/"+id, nil, true)
	if err != nil {
		return nil, err
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, err
	}
	if err := firstError(env.Errors); err != nil {
		return nil, err
	}
	var items []json.RawMessage
	if err := json.Unmarshal(env.Data, &items); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("hue %s %s was not found", kind, id)
	}
	return items[0], nil
}

// Put updates a resource. kind is a CLIP type such as "light" or "scene".
func (c *Client) Put(ctx context.Context, kind, id string, payload any) error {
	body, err := c.do(ctx, http.MethodPut, "/clip/v2/resource/"+kind+"/"+id, payload, true)
	if err != nil {
		return err
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return err
	}
	return firstError(env.Errors)
}

// BridgeID reads the bridge's own id after pairing.
func (c *Client) BridgeID(ctx context.Context) (string, error) {
	body, err := c.do(ctx, http.MethodGet, "/clip/v2/resource/bridge", nil, true)
	if err != nil {
		return "", err
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return "", err
	}
	if err := firstError(env.Errors); err != nil {
		return "", err
	}
	var items []struct {
		BridgeID string `json:"bridge_id"`
	}
	if err := json.Unmarshal(env.Data, &items); err != nil {
		return "", err
	}
	if len(items) == 0 || items[0].BridgeID == "" {
		return "", errors.New("bridge did not report an id")
	}
	return items[0].BridgeID, nil
}

// CreateUser registers an application key. The link button must be pressed
// first; otherwise this returns ErrLinkButton.
func (c *Client) CreateUser(ctx context.Context) (username, clientKey string, err error) {
	payload := map[string]any{
		"devicetype":        deviceType,
		"generateclientkey": true,
	}
	body, err := c.do(ctx, http.MethodPost, "/api", payload, false)
	if err != nil {
		return "", "", err
	}
	var rows []struct {
		Success *struct {
			Username  string `json:"username"`
			ClientKey string `json:"clientkey"`
		} `json:"success"`
		Error *apiError `json:"error"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return "", "", err
	}
	if len(rows) == 0 {
		return "", "", errors.New("bridge returned an empty pairing response")
	}
	if rows[0].Error != nil {
		if rows[0].Error.Type == 101 {
			return "", "", ErrLinkButton
		}
		if rows[0].Error.Description != "" {
			return "", "", errors.New(rows[0].Error.Description)
		}
		return "", "", fmt.Errorf("pairing failed (%d)", rows[0].Error.Type)
	}
	if rows[0].Success == nil || rows[0].Success.Username == "" {
		return "", "", errors.New("bridge did not return an application key")
	}
	return rows[0].Success.Username, rows[0].Success.ClientKey, nil
}

func (c *Client) do(ctx context.Context, method, path string, payload any, auth bool) ([]byte, error) {
	var buf io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		buf = bytes.NewReader(data)
	}
	url := "https://" + formatHost(c.IP) + path
	req, err := http.NewRequestWithContext(ctx, method, url, buf)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if auth {
		if c.Username == "" {
			return nil, ErrUnauthorized
		}
		req.Header.Set("hue-application-key", c.Username)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bridge unreachable: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, ErrUnauthorized
	}
	if resp.StatusCode >= 400 {
		text := strings.TrimSpace(string(body))
		if len(text) > 180 {
			text = text[:180]
		}
		if text == "" {
			text = resp.Status
		}
		return nil, errors.New(text)
	}
	return body, nil
}

func firstError(errs []apiError) error {
	if len(errs) == 0 {
		return nil
	}
	desc := strings.ToLower(errs[0].Description)
	if errs[0].Type == 1 || strings.Contains(desc, "unauthorized") || strings.Contains(desc, "not authorized") {
		return ErrUnauthorized
	}
	if errs[0].Description != "" {
		return errors.New(errs[0].Description)
	}
	return fmt.Errorf("hue error %d", errs[0].Type)
}

func formatHost(ip string) string {
	if host, port, err := net.SplitHostPort(ip); err == nil {
		if strings.Count(host, ":") >= 2 {
			return "[" + host + "]:" + port
		}
		return ip
	}
	if strings.Count(ip, ":") >= 2 {
		return "[" + ip + "]"
	}
	return ip
}
