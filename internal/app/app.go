// Package app is the Hue command surface used by the shell panel.
package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"hue/internal/config"
	"hue/internal/hue"
)

// Result is the JSON document for commands that change something.
type Result struct {
	OK     bool        `json:"ok"`
	Error  string      `json:"error,omitempty"`
	Bridge *hue.Bridge `json:"bridge,omitempty"`
}

// Patch is a partial update for a light, switch, or room.
type Patch struct {
	On         *bool
	Brightness *float64
	Hue        *float64
	Saturation *float64
	CT         *int
}

// Snapshot returns the paired home, or the bridges available to pair with.
func Snapshot(ctx context.Context) (hue.Snapshot, error) {
	cfg, err := config.Load()
	if err != nil {
		return hue.Snapshot{}, err
	}
	if cfg == nil {
		return hue.UnpairedSnapshot(hue.Discover(ctx), ""), nil
	}
	return snapshotWith(ctx, hue.NewClient(cfg.IP, cfg.Username, cfg.ID), cfg)
}

func snapshotWith(ctx context.Context, client *hue.Client, cfg *config.Bridge) (hue.Snapshot, error) {
	bridge := publicBridge(cfg, client.IP)
	items, err := client.Resources(ctx)
	if err != nil && unreachable(err) {
		if relocated, ok := relocate(ctx, cfg); ok {
			client = relocated
			bridge = publicBridge(cfg, client.IP)
			items, err = client.Resources(ctx)
		}
	}
	if errors.Is(err, hue.ErrUnauthorized) {
		return hue.Snapshot{
			Paired:    true,
			Reachable: true,
			Error:     "The bridge rejected the saved key. Forget it and pair again.",
			Bridge:    bridge,
			Bridges:   []hue.Found{},
			Groups:    []hue.Group{},
		}, nil
	}
	if err != nil {
		return hue.Snapshot{
			Paired:     true,
			Authorized: true,
			Error:      cleanError(err),
			Bridge:     bridge,
			Bridges:    []hue.Found{},
			Groups:     []hue.Group{},
		}, nil
	}
	snap := hue.SnapshotFrom(items)
	snap.Bridge = bridge
	return snap, nil
}

// Pair waits for the link button and stores the application key.
func Pair(ctx context.Context, ip, id, name string, wait time.Duration) (Result, error) {
	if strings.TrimSpace(ip) == "" {
		return Result{}, errors.New("a bridge address is required")
	}
	if wait <= 0 {
		wait = 30 * time.Second
	}
	client := hue.NewClient(ip, "", id)
	deadline := time.Now().Add(wait)
	var username, clientKey string
	var err error
	for {
		username, clientKey, err = client.CreateUser(ctx)
		if err == nil {
			break
		}
		if !errors.Is(err, hue.ErrLinkButton) {
			return Result{}, cleanErr(err)
		}
		if !time.Now().Before(deadline) {
			return Result{}, errors.New("press the link button on the bridge, then pair again")
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Result{}, ctx.Err()
		case <-timer.C:
		}
	}
	client.Username = username
	if confirmed := client.Identity(); confirmed != "" {
		id = confirmed
	} else if id == "" {
		if got, err := client.BridgeID(ctx); err == nil {
			id = got
		}
	}
	if strings.TrimSpace(name) == "" {
		name = "Hue Bridge"
	}
	saved := config.Bridge{
		ID:        id,
		IP:        ip,
		Name:      name,
		Username:  username,
		ClientKey: clientKey,
	}
	if err := config.Save(saved); err != nil {
		return Result{}, err
	}
	return Result{OK: true, Bridge: publicBridge(&saved, ip)}, nil
}

// Forget deletes the stored application key.
func Forget() Result {
	if err := config.Forget(); err != nil {
		return Result{Error: err.Error()}
	}
	return Result{OK: true}
}

// Set changes a light, switch, or room.
func Set(ctx context.Context, kind, id string, patch Patch) (Result, error) {
	if kind != "light" && kind != "grouped_light" {
		return Result{}, errors.New("kind must be light or grouped_light")
	}
	if !validID(id) {
		return Result{}, errors.New("missing resource id")
	}
	cfg, err := config.Load()
	if err != nil {
		return Result{}, err
	}
	if cfg == nil {
		return Result{}, errors.New("not paired with a bridge")
	}
	client := hue.NewClient(cfg.IP, cfg.Username, cfg.ID)
	if err := apply(ctx, client, kind, id, patch); err != nil && unreachable(err) {
		if relocated, ok := relocate(ctx, cfg); ok {
			client = relocated
			err = apply(ctx, client, kind, id, patch)
		}
	}
	if err != nil {
		return Result{}, cleanErr(err)
	}
	return Result{OK: true}, nil
}

// Activate recalls a scene or smart scene.
func Activate(ctx context.Context, kind, id string) (Result, error) {
	if kind == "" {
		kind = "scene"
	}
	if kind != "scene" && kind != "smart_scene" {
		return Result{}, errors.New("kind must be scene or smart_scene")
	}
	if !validID(id) {
		return Result{}, errors.New("missing scene id")
	}
	action := "active"
	if kind == "smart_scene" {
		action = "activate"
	}
	cfg, err := config.Load()
	if err != nil {
		return Result{}, err
	}
	if cfg == nil {
		return Result{}, errors.New("not paired with a bridge")
	}
	client := hue.NewClient(cfg.IP, cfg.Username, cfg.ID)
	body := map[string]any{"recall": map[string]any{"action": action}}
	err = client.Put(ctx, kind, id, body)
	if err != nil && unreachable(err) {
		if relocated, ok := relocate(ctx, cfg); ok {
			err = relocated.Put(ctx, kind, id, body)
		}
	}
	if err != nil {
		return Result{}, cleanErr(err)
	}
	return Result{OK: true}, nil
}

func apply(ctx context.Context, client *hue.Client, kind, id string, patch Patch) error {
	body := map[string]any{}
	if patch.On != nil {
		body["on"] = map[string]any{"on": *patch.On}
	}
	if patch.Brightness != nil {
		body["dimming"] = map[string]any{"brightness": clamp(*patch.Brightness, 1, 100)}
	}
	if patch.CT != nil {
		body["color_temperature"] = map[string]any{"mirek": *patch.CT}
	}
	if patch.Hue != nil {
		sat := 100.0
		if patch.Saturation != nil {
			sat = *patch.Saturation
		}
		// Gamut C covers current Hue color bulbs. Skipping the extra GET keeps
		// a color change to one round trip; the bridge clamps anything outside.
		xy := hue.XYForResource(nil, *patch.Hue, sat)
		body["color"] = map[string]any{"xy": map[string]float64{"x": round4(xy.X), "y": round4(xy.Y)}}
	}
	if patch.On == nil && (patch.Brightness != nil || patch.CT != nil || patch.Hue != nil) {
		body["on"] = map[string]any{"on": true}
	}
	if len(body) == 0 {
		return errors.New("nothing to change")
	}
	return client.Put(ctx, kind, id, body)
}

func relocate(ctx context.Context, cfg *config.Bridge) (*hue.Client, bool) {
	if cfg.ID == "" {
		return nil, false
	}
	for _, found := range hue.Discover(ctx) {
		if !strings.EqualFold(found.ID, cfg.ID) || found.IP == "" || found.IP == cfg.IP {
			continue
		}
		cfg.IP = found.IP
		_ = config.Save(*cfg)
		return hue.NewClient(cfg.IP, cfg.Username, cfg.ID), true
	}
	return nil, false
}

func publicBridge(cfg *config.Bridge, ip string) *hue.Bridge {
	name := cfg.Name
	if name == "" {
		name = "Hue Bridge"
	}
	if ip == "" {
		ip = cfg.IP
	}
	return &hue.Bridge{ID: cfg.ID, IP: ip, Name: name}
}

func unreachable(err error) bool {
	return err != nil && strings.Contains(err.Error(), "bridge unreachable")
}

func cleanErr(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(cleanError(err))
}

func cleanError(err error) string {
	text := err.Error()
	text = strings.TrimPrefix(text, "bridge unreachable: ")
	if len(text) > 240 {
		text = text[:240]
	}
	return text
}

func validID(id string) bool {
	if id == "" || len(id) > 80 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
		default:
			return false
		}
	}
	return true
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func round4(v float64) float64 {
	return float64(int(v*10000+0.5)) / 10000
}
