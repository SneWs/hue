package hue

import (
	"encoding/json"
	"testing"
)

func TestSnapshotGroupsByRoom(t *testing.T) {
	items := mustItems(t, `[
		{"type":"bridge","id":"bridge-res","bridge_id":"ABC","owner":{"rid":"dev-bridge","rtype":"device"}},
		{"type":"device","id":"dev-bridge","metadata":{"name":"Hue Bridge","archetype":"bridge_v2"},"product_data":{"model_id":"BSB002"}},
		{"type":"device","id":"dev-lamp","metadata":{"name":"Lamp"}},
		{"type":"device","id":"dev-plug","metadata":{"name":"Plug"}},
		{"type":"device","id":"dev-motion","metadata":{"name":"Hall motion"}},
		{"type":"device","id":"dev-dimmer","metadata":{"name":"Dimmer"}},
		{"type":"device","id":"dev-hall","metadata":{"name":"Hall bulb"}},
		{"type":"room","id":"room-living","metadata":{"name":"Living room","archetype":"living_room"},
			"children":[
				{"rid":"dev-lamp","rtype":"device"},
				{"rid":"dev-plug","rtype":"device"},
				{"rid":"dev-motion","rtype":"device"},
				{"rid":"dev-dimmer","rtype":"device"}
			],
			"services":[{"rid":"gl-living","rtype":"grouped_light"}]},
		{"type":"grouped_light","id":"gl-living","owner":{"rid":"room-living","rtype":"room"},
			"on":{"on":true},"dimming":{"brightness":40}},
		{"type":"light","id":"light-lamp","owner":{"rid":"dev-lamp","rtype":"device"},
			"metadata":{"name":"Lamp","archetype":"sultan_bulb"},
			"on":{"on":true},
			"dimming":{"brightness":55},
			"color":{"xy":{"x":0.3,"y":0.3},"gamut_type":"C"},
			"color_temperature":{"mirek":300,"mirek_valid":true,"mirek_schema":{"mirek_minimum":153,"mirek_maximum":500}}},
		{"type":"light","id":"light-plug","owner":{"rid":"dev-plug","rtype":"device"},
			"metadata":{"name":"Plug","archetype":"plug"},
			"on":{"on":false}},
		{"type":"light","id":"light-hall","owner":{"rid":"dev-hall","rtype":"device"},
			"metadata":{"name":"Hall bulb","archetype":"candle_bulb"},
			"on":{"on":false},
			"dimming":{"brightness":10}},
		{"type":"motion","id":"motion-1","owner":{"rid":"dev-motion","rtype":"device"},
			"motion":{"motion":true,"motion_valid":true}},
		{"type":"temperature","id":"temp-1","owner":{"rid":"dev-motion","rtype":"device"},
			"temperature":{"temperature":21.5,"temperature_valid":true}},
		{"type":"light_level","id":"ll-1","owner":{"rid":"dev-motion","rtype":"device"},
			"light":{"light_level":1,"light_level_valid":true}},
		{"type":"device_power","id":"pow-1","owner":{"rid":"dev-motion","rtype":"device"},
			"power_state":{"battery_level":64,"battery_state":"normal"}},
		{"type":"button","id":"btn-1","owner":{"rid":"dev-dimmer","rtype":"device"},
			"metadata":{"control_id":1},
			"button":{"last_event":"short_release","button_report":{"event":"short_release","updated":"2026-09-28T10:00:00Z"}}},
		{"type":"button","id":"btn-4","owner":{"rid":"dev-dimmer","rtype":"device"},
			"metadata":{"control_id":4},
			"button":{"last_event":"initial_press","button_report":{"event":"initial_press","updated":"2026-09-28T09:00:00Z"}}},
		{"type":"zigbee_connectivity","id":"zb-hall","owner":{"rid":"dev-hall","rtype":"device"},"status":"disconnected"},
		{"type":"scene","id":"scene-relax","metadata":{"name":"Relax"},"group":{"rid":"room-living","rtype":"room"}},
		{"type":"scene","id":"scene-zone","metadata":{"name":"Zone scene"},"group":{"rid":"zone-1","rtype":"zone"}},
		{"type":"smart_scene","id":"scene-smart","metadata":{"name":"Wake"},"group":{"rid":"room-living","rtype":"room"}}
	]`)

	snap := SnapshotFrom(items)
	if !snap.AnyOn {
		t.Fatal("expected a light to be on")
	}
	if len(snap.Groups) != 2 {
		t.Fatalf("groups: %#v", names(snap))
	}
	living := snap.Groups[0]
	if living.Name != "Living room" || living.Kind != "room" {
		t.Fatalf("first group %+v", living)
	}
	if living.ControlID != "gl-living" || living.On == nil || !*living.On || living.Brightness != 40 || !living.SupportsBrightness {
		t.Fatalf("room control %+v", living)
	}
	if len(living.Scenes) != 2 || living.Scenes[0].Name != "Relax" || living.Scenes[1].Name != "Wake" || living.Scenes[1].Kind != "smart_scene" {
		t.Fatalf("scenes %+v", living.Scenes)
	}
	if len(living.Lights) != 1 || living.Lights[0].ID != "light-lamp" || !living.Lights[0].SupportsColor || !living.Lights[0].SupportsColorTemperature {
		t.Fatalf("lights %+v", living.Lights)
	}
	if living.Lights[0].Brightness != 55 || living.Lights[0].Mirek != 300 || living.Lights[0].MirekMin != 153 {
		t.Fatalf("light levels %+v", living.Lights[0])
	}
	if len(living.Switches) != 1 || living.Switches[0].Name != "Plug" || living.Switches[0].On {
		t.Fatalf("switches %+v", living.Switches)
	}
	if len(living.Sensors) != 2 {
		t.Fatalf("sensors %+v", living.Sensors)
	}
	if living.Sensors[0].Name != "Dimmer" || living.Sensors[0].Value != "Button 1 · short press" {
		t.Fatalf("dimmer %+v", living.Sensors[0])
	}
	if living.Sensors[1].Name != "Hall motion" || living.Sensors[1].Kind != "motion" {
		t.Fatalf("motion %+v", living.Sensors[1])
	}
	if living.Sensors[1].Value != "Motion · 21.5°C · 1 lx · 64%" {
		t.Fatalf("motion value %q", living.Sensors[1].Value)
	}

	other := snap.Groups[1]
	if other.Name != "Other" || other.Kind != "other" || other.ControlID != "" {
		t.Fatalf("other %+v", other)
	}
	if len(other.Lights) != 1 || other.Lights[0].Name != "Hall bulb" || other.Lights[0].Reachable {
		t.Fatalf("hall %+v", other.Lights)
	}
	if len(other.Scenes) != 1 || other.Scenes[0].Name != "Zone scene" {
		t.Fatalf("other scenes %+v", other.Scenes)
	}
}

func names(snap Snapshot) []string {
	out := make([]string, len(snap.Groups))
	for i, g := range snap.Groups {
		out[i] = g.Name
	}
	return out
}

func mustItems(t *testing.T, raw string) []json.RawMessage {
	t.Helper()
	var items []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		t.Fatal(err)
	}
	return items
}
