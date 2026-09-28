package hue

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

// Found is a bridge seen on the local network.
type Found struct {
	ID    string `json:"id"`
	IP    string `json:"ip"`
	Name  string `json:"name"`
	Model string `json:"model,omitempty"`
}

// Discover looks for bridges with mDNS and, in parallel, Philips' discovery service.
// A failure of either source still returns whatever the other one found.
func Discover(ctx context.Context) []Found {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	var (
		mu   sync.Mutex
		all  []Found
		wait sync.WaitGroup
	)
	wait.Add(2)
	go func() {
		defer wait.Done()
		found := discoverAvahi(ctx)
		mu.Lock()
		all = append(all, found...)
		mu.Unlock()
	}()
	go func() {
		defer wait.Done()
		found := discoverNUPNP(ctx)
		mu.Lock()
		all = append(all, found...)
		mu.Unlock()
	}()
	wait.Wait()
	return dedupeBridges(all)
}

func discoverAvahi(ctx context.Context) []Found {
	cmd := exec.CommandContext(ctx, "avahi-browse", "-kprt", "_hue._tcp")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	_ = cmd.Run()
	var found []Found
	for _, line := range strings.Split(stdout.String(), "\n") {
		if bridge, ok := parseAvahiLine(line); ok {
			found = append(found, bridge)
		}
	}
	return found
}

func discoverNUPNP(ctx context.Context) []Found {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://discovery.meethue.com/", nil)
	if err != nil {
		return nil
	}
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var rows []struct {
		ID   string `json:"id"`
		IP   string `json:"internalipaddress"`
		Port int    `json:"port"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return nil
	}
	found := make([]Found, 0, len(rows))
	for _, row := range rows {
		if row.IP == "" {
			continue
		}
		found = append(found, Found{
			ID:   strings.ToUpper(row.ID),
			IP:   row.IP,
			Name: "Hue Bridge",
		})
	}
	return found
}

// parseAvahiLine reads one resolved browse record:
// =;iface;proto;name;type;domain;host;address;port;txt...
func parseAvahiLine(line string) (Found, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "=;") {
		return Found{}, false
	}
	parts := strings.Split(line, ";")
	if len(parts) < 9 {
		return Found{}, false
	}
	if parts[2] != "IPv4" && parts[2] != "IPv6" {
		return Found{}, false
	}
	ip := parts[7]
	if ip == "" {
		return Found{}, false
	}
	bridge := Found{
		IP:   ip,
		Name: unescapeAvahi(parts[3]),
	}
	txt := strings.Join(parts[9:], ";")
	for _, field := range strings.Split(txt, `" "`) {
		field = strings.Trim(field, `"`)
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		switch key {
		case "bridgeid":
			bridge.ID = strings.ToUpper(value)
		case "modelid":
			bridge.Model = value
		}
	}
	if bridge.Name == "" {
		bridge.Name = "Hue Bridge"
	}
	return bridge, true
}

func dedupeBridges(found []Found) []Found {
	byKey := map[string]Found{}
	var order []string
	for _, bridge := range found {
		key := strings.ToUpper(bridge.ID)
		if key == "" {
			key = "ip:" + bridge.IP
		}
		prev, ok := byKey[key]
		if !ok {
			order = append(order, key)
			byKey[key] = bridge
			continue
		}
		// Prefer a record that has both a real name and an IPv4 address.
		if betterBridge(bridge, prev) {
			byKey[key] = bridge
		}
	}
	out := make([]Found, 0, len(order))
	for _, key := range order {
		out = append(out, byKey[key])
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// unescapeAvahi turns avahi's three-digit decimal escapes into characters.
// A space in a service name arrives as "\032".
func unescapeAvahi(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) && isDigit(s[i+1]) && isDigit(s[i+2]) && isDigit(s[i+3]) {
			n := int(s[i+1]-'0')*100 + int(s[i+2]-'0')*10 + int(s[i+3]-'0')
			if n <= 255 {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

func betterBridge(next, prev Found) bool {
	next4 := strings.Count(next.IP, ":") < 2
	prev4 := strings.Count(prev.IP, ":") < 2
	if next4 != prev4 {
		return next4
	}
	nextNamed := next.Name != "" && next.Name != "Hue Bridge"
	prevNamed := prev.Name != "" && prev.Name != "Hue Bridge"
	if nextNamed != prevNamed {
		return nextNamed
	}
	if next.Model != "" && prev.Model == "" {
		return true
	}
	return false
}
