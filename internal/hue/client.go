package hue

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const deviceType = "omarchy#hue"

// ErrLinkButton means the bridge is reachable but its link button is not pressed.
var ErrLinkButton = errors.New("link button not pressed")

// ErrUnauthorized means the stored application key was rejected.
var ErrUnauthorized = errors.New("application key rejected")

// Client talks to one bridge. The handshake verifies the certificate against
// Signify's Hue Bridge CA and requires the subject to be this bridge's id
// before any request, including the application key, is sent. HTTP/2 is
// disabled because Hue bridges misbehave with it.
type Client struct {
	IP       string
	Username string
	bridgeID string

	mu     sync.Mutex
	seenID string
	http   *http.Client
}

// NewClient returns a client for a bridge address, which may include a port.
// bridgeID is the id from discovery or from the saved config. It may be empty
// only while pairing by address; the certificate subject then supplies it.
func NewClient(ip, username, bridgeID string) *Client {
	c := &Client{
		IP:       ip,
		Username: username,
		bridgeID: normalizeBridgeID(bridgeID),
	}
	c.http = &http.Client{
		Timeout:   6 * time.Second,
		Transport: c.transport(),
	}
	return c
}

// Identity is the bridge id confirmed by the certificate on the last handshake.
func (c *Client) Identity() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seenID != "" {
		return c.seenID
	}
	return c.bridgeID
}

func (c *Client) transport() *http.Transport {
	return &http.Transport{
		TLSClientConfig:       c.tlsConfig(),
		ForceAttemptHTTP2:     false,
		TLSNextProto:          map[string]func(string, *tls.Conn) http.RoundTripper{},
		DialContext:           (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
		ResponseHeaderTimeout: 4 * time.Second,
		MaxIdleConns:          2,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       2 * time.Minute,
	}
}

func (c *Client) tlsConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		// Hue certificates identify the bridge in the Common Name and do not
		// carry a subject alternative name, so Go's default hostname check
		// cannot accept them. This flag disables only that default check.
		// VerifyPeerCertificate performs the CA and bridge-id checks and
		// aborts the handshake before the application key is sent.
		InsecureSkipVerify: true,
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			id, err := verifyBridgeCertificate(raw, c.bridgeID)
			if err != nil {
				return err
			}
			c.mu.Lock()
			c.seenID = id
			c.mu.Unlock()
			return nil
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
