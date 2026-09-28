package hue

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveBridgeCertificate(t *testing.T) {
	if os.Getenv("HUE_LIVE") == "" {
		t.Skip("set HUE_LIVE=1 to check the bridge on this LAN")
	}
	ip := os.Getenv("HUE_IP")
	id := os.Getenv("HUE_ID")
	if ip == "" || id == "" {
		t.Fatal("HUE_IP and HUE_ID are required")
	}
	client := NewClient(ip, "", id)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	body, err := client.do(ctx, http.MethodGet, "/api/0/config", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "bridgeid") && !strings.Contains(string(body), "name") {
		t.Fatalf("unexpected response %q", body)
	}
	if !strings.EqualFold(client.Identity(), id) {
		t.Fatalf("certificate identity %s, want %s", client.Identity(), id)
	}
	wrong := NewClient(ip, "not-sent", "001788fffe000000")
	if _, err := wrong.do(ctx, http.MethodGet, "/api/0/config", nil, true); err == nil {
		t.Fatal("accepted a certificate for a different bridge id")
	}
}
