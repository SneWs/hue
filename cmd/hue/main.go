package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"hue/internal/app"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		if err := app.Serve(); err != nil {
			fail(err)
		}
	case "snapshot":
		if forwardAndPrint(app.Request{Op: "snapshot"}) {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		snap, err := app.Snapshot(ctx)
		if err != nil {
			fail(err)
		}
		writeJSON(snap)
	case "pair":
		fs := flag.NewFlagSet("pair", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		ip := fs.String("ip", "", "bridge address")
		id := fs.String("id", "", "bridge id, when discovery already knows it")
		name := fs.String("name", "", "bridge name")
		wait := fs.Duration("wait", 30*time.Second, "how long to wait for the link button")
		if err := fs.Parse(os.Args[2:]); err != nil {
			fail(err)
		}
		if strings.TrimSpace(*ip) == "" || strings.ContainsAny(*ip, " /\\") {
			fail(fmt.Errorf("pass the bridge address with --ip"))
		}
		ctx, cancel := context.WithTimeout(context.Background(), *wait+15*time.Second)
		defer cancel()
		result, err := app.Pair(ctx, strings.TrimSpace(*ip), *id, *name, *wait)
		if err != nil {
			fail(err)
		}
		writeJSON(result)
	case "forget":
		result := app.Forget()
		_, _ = app.Forward(app.Request{Op: "forget"})
		writeJSON(result)
	case "set":
		fs := flag.NewFlagSet("set", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		kind := fs.String("kind", "light", "light or grouped_light")
		id := fs.String("id", "", "resource id")
		on := fs.String("on", "", "true or false")
		brightness := fs.String("brightness", "", "1-100")
		hueDeg := fs.String("hue", "", "0-360")
		sat := fs.String("saturation", "", "0-100")
		ct := fs.String("ct", "", "color temperature in mirek")
		if err := fs.Parse(os.Args[2:]); err != nil {
			fail(err)
		}
		patch, err := patchFrom(*on, *brightness, *hueDeg, *sat, *ct)
		if err != nil {
			fail(err)
		}
		req := app.Request{Op: "set", Kind: *kind, ID: *id, On: patch.On, Brightness: patch.Brightness, Hue: patch.Hue, Saturation: patch.Saturation, CT: patch.CT}
		if forwardAndPrint(req) {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		result, err := app.Set(ctx, *kind, *id, patch)
		if err != nil {
			fail(err)
		}
		writeJSON(result)
	case "scene":
		fs := flag.NewFlagSet("scene", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		kind := fs.String("kind", "scene", "scene or smart_scene")
		id := fs.String("id", "", "scene id")
		if err := fs.Parse(os.Args[2:]); err != nil {
			fail(err)
		}
		if forwardAndPrint(app.Request{Op: "scene", Kind: *kind, ID: *id}) {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		result, err := app.Activate(ctx, *kind, *id)
		if err != nil {
			fail(err)
		}
		writeJSON(result)
	case "-h", "--help", "help":
		usage()
	default:
		fail(fmt.Errorf("unknown command %q", os.Args[1]))
	}
}

func patchFrom(on, brightness, hueDeg, saturation, ct string) (app.Patch, error) {
	var patch app.Patch
	if on != "" {
		value, err := strconv.ParseBool(on)
		if err != nil {
			return patch, fmt.Errorf("--on must be true or false")
		}
		patch.On = &value
	}
	if brightness != "" {
		value, err := strconv.ParseFloat(brightness, 64)
		if err != nil {
			return patch, fmt.Errorf("--brightness must be a number")
		}
		patch.Brightness = &value
	}
	if hueDeg != "" {
		value, err := strconv.ParseFloat(hueDeg, 64)
		if err != nil {
			return patch, fmt.Errorf("--hue must be a number")
		}
		patch.Hue = &value
	}
	if saturation != "" {
		value, err := strconv.ParseFloat(saturation, 64)
		if err != nil {
			return patch, fmt.Errorf("--saturation must be a number")
		}
		patch.Saturation = &value
	}
	if ct != "" {
		value, err := strconv.Atoi(ct)
		if err != nil {
			return patch, fmt.Errorf("--ct must be an integer")
		}
		patch.CT = &value
	}
	return patch, nil
}

func forwardAndPrint(req app.Request) bool {
	raw, ok := app.Forward(req)
	if !ok {
		return false
	}
	if len(raw) == 0 || raw[len(raw)-1] != '\n' {
		raw = append(raw, '\n')
	}
	os.Stdout.Write(raw)
	var probe struct {
		OK    *bool  `json:"ok"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &probe)
	if probe.OK != nil && !*probe.OK {
		os.Exit(1)
	}
	os.Exit(0)
	return true
}

func writeJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func fail(err error) {
	writeJSON(map[string]any{"ok": false, "error": err.Error()})
	os.Exit(1)
}

func usage() {
	fmt.Fprintf(os.Stderr, `hue controls a local Hue Bridge for the Omarchy shell panel.

The application key is stored in ~/.config/hue/bridge.json.
hue serve keeps one connection open so later commands skip the TLS handshake.

  hue serve
      Listen on the per-user socket. Exits if a server is already running.

  hue snapshot
      Print the paired home, grouped by room, or the bridges available to pair.

  hue pair --ip ADDRESS [--id ID] [--name NAME] [--wait 30s]
      Register with a bridge. Press its link button while this runs.

  hue forget
      Delete the saved application key.

  hue set --kind light|grouped_light --id ID [--on true|false]
          [--brightness 1-100] [--hue 0-360] [--saturation 0-100] [--ct MIREK]

  hue scene --kind scene|smart_scene --id ID
`)
}
