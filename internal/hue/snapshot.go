package hue

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Snapshot is the document the shell panel renders.
type Snapshot struct {
	Paired     bool    `json:"paired"`
	Reachable  bool    `json:"reachable"`
	Authorized bool    `json:"authorized"`
	Error      string  `json:"error,omitempty"`
	Bridge     *Bridge `json:"bridge,omitempty"`
	Bridges    []Found `json:"bridges"`
	Groups     []Group `json:"groups"`
	AnyOn      bool    `json:"anyOn"`
}

// Bridge is the paired bridge, without its application key.
type Bridge struct {
	ID   string `json:"id"`
	IP   string `json:"ip"`
	Name string `json:"name"`
}

// Group is one room, or the final Other block for devices that have no room.
type Group struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Kind               string   `json:"kind"`
	ControlID          string   `json:"controlId,omitempty"`
	On                 *bool    `json:"on,omitempty"`
	Brightness         int      `json:"brightness,omitempty"`
	SupportsBrightness bool     `json:"supportsBrightness"`
	Scenes             []Scene  `json:"scenes"`
	Lights             []Light  `json:"lights"`
	Switches           []Switch `json:"switches"`
	Sensors            []Sensor `json:"sensors"`
}

// Scene is a recallable room scene.
type Scene struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// Light is a dimmable or color light.
type Light struct {
	ID                       string `json:"id"`
	Name                     string `json:"name"`
	On                       bool   `json:"on"`
	Reachable                bool   `json:"reachable"`
	Brightness               int    `json:"brightness,omitempty"`
	SupportsBrightness       bool   `json:"supportsBrightness"`
	SupportsColor            bool   `json:"supportsColor"`
	SupportsColorTemperature bool   `json:"supportsColorTemperature"`
	Hue                      int    `json:"hue,omitempty"`
	Saturation               int    `json:"saturation,omitempty"`
	Mirek                    int    `json:"mirek,omitempty"`
	MirekMin                 int    `json:"mirekMin,omitempty"`
	MirekMax                 int    `json:"mirekMax,omitempty"`
	GamutType                string `json:"gamutType,omitempty"`
}

// Switch is an on/off accessory, such as a smart plug.
type Switch struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	On        bool   `json:"on"`
	Reachable bool   `json:"reachable"`
}

// Sensor is a read-only device: motion, climate, contact, or a button pad.
type Sensor struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Value     string `json:"value"`
	Reachable bool   `json:"reachable"`
}

// UnpairedSnapshot is the pair-with-bridge screen.
func UnpairedSnapshot(bridges []Found, message string) Snapshot {
	if bridges == nil {
		bridges = []Found{}
	}
	return Snapshot{
		Bridges: bridges,
		Groups:  []Group{},
		Error:   message,
	}
}

// SnapshotFrom builds the grouped view from a full resource list.
func SnapshotFrom(items []json.RawMessage) Snapshot {
	home := Decode(items)
	groups := home.groups()
	snap := Snapshot{
		Paired:     true,
		Reachable:  true,
		Authorized: true,
		Bridges:    []Found{},
		Groups:     groups,
	}
	for _, group := range groups {
		for _, light := range group.Lights {
			if light.On {
				snap.AnyOn = true
			}
		}
		for _, sw := range group.Switches {
			if sw.On {
				snap.AnyOn = true
			}
		}
	}
	return snap
}

func (h Home) groups() []Group {
	bridgeDevices := map[string]bool{}
	for _, bridge := range h.Bridges {
		if bridge.Owner.RID != "" {
			bridgeDevices[bridge.Owner.RID] = true
		}
	}
	devices := map[string]device{}
	for _, d := range h.Devices {
		devices[d.ID] = d
	}
	reachable := map[string]bool{}
	for _, z := range h.Zigbees {
		reachable[z.Owner.RID] = z.Status == "" || z.Status == "connected"
	}
	battery := map[string]string{}
	for _, power := range h.Powers {
		if power.PowerState.BatteryLevel == nil {
			continue
		}
		battery[power.Owner.RID] = fmt.Sprintf("%d%%", *power.PowerState.BatteryLevel)
	}

	roomOf := map[string]string{}
	for _, room := range h.Rooms {
		for _, child := range room.Children {
			if child.RType == "device" && child.RID != "" {
				roomOf[child.RID] = room.ID
			}
		}
	}

	grouped := map[string]groupedLight{}
	for _, gl := range h.GroupedLights {
		grouped[gl.ID] = gl
		if gl.Owner.RType == "room" && gl.Owner.RID != "" {
			grouped[gl.Owner.RID] = gl
		}
	}

	byRoom := map[string]*Group{}
	ensure := func(id, name, kind string) *Group {
		if g, ok := byRoom[id]; ok {
			return g
		}
		g := &Group{
			ID:       id,
			Name:     name,
			Kind:     kind,
			Scenes:   []Scene{},
			Lights:   []Light{},
			Switches: []Switch{},
			Sensors:  []Sensor{},
		}
		byRoom[id] = g
		return g
	}
	for _, room := range h.Rooms {
		g := ensure(room.ID, nameOr(room.Metadata.Name, "Room"), "room")
		for _, svc := range room.Services {
			if svc.RType != "grouped_light" {
				continue
			}
			gl, ok := grouped[svc.RID]
			if !ok {
				gl, ok = grouped[room.ID]
			}
			if !ok {
				g.ControlID = svc.RID
				continue
			}
			g.ControlID = gl.ID
			if gl.On != nil {
				on := gl.On.On
				g.On = &on
			}
			if gl.Dimming != nil {
				g.SupportsBrightness = true
				g.Brightness = clampPercent(gl.Dimming.Brightness)
			}
		}
	}
	other := ensure("other", "Other", "other")

	place := func(deviceID string) *Group {
		if roomID, ok := roomOf[deviceID]; ok {
			if g, ok := byRoom[roomID]; ok {
				return g
			}
		}
		return other
	}

	for _, light := range h.Lights {
		dev := devices[light.Owner.RID]
		if dev.isBridge(bridgeDevices) {
			continue
		}
		name := nameOr(light.Metadata.Name, nameOr(dev.Metadata.Name, "Light"))
		online := deviceOnline(reachable, light.Owner.RID)
		if isSwitch(light) {
			place(light.Owner.RID).Switches = append(place(light.Owner.RID).Switches, Switch{
				ID:        light.ID,
				Name:      name,
				On:        light.on(),
				Reachable: online,
			})
			continue
		}
		row := Light{
			ID:                       light.ID,
			Name:                     name,
			On:                       light.on(),
			Reachable:                online,
			SupportsBrightness:       light.Dimming != nil,
			SupportsColor:            light.Color != nil,
			SupportsColorTemperature: light.ColorTemperature != nil,
		}
		if light.Dimming != nil {
			row.Brightness = clampPercent(light.Dimming.Brightness)
		}
		if light.Color != nil {
			hue, sat, _ := XYToHSV(Point{light.Color.XY.X, light.Color.XY.Y}, light.gamut())
			row.Hue = int(math.Round(hue))
			row.Saturation = int(math.Round(sat * 100))
			row.GamutType = light.Color.GamutType
		}
		if light.ColorTemperature != nil {
			if light.ColorTemperature.Mirek != nil {
				row.Mirek = int(math.Round(*light.ColorTemperature.Mirek))
			}
			row.MirekMin = light.ColorTemperature.MirekSchema.Min
			row.MirekMax = light.ColorTemperature.MirekSchema.Max
			if row.MirekMin == 0 {
				row.MirekMin = 153
			}
			if row.MirekMax == 0 {
				row.MirekMax = 500
			}
		}
		place(light.Owner.RID).Lights = append(place(light.Owner.RID).Lights, row)
	}

	controlled := map[string]bool{}
	for _, light := range h.Lights {
		controlled[light.Owner.RID] = true
	}

	bits := map[string]*sensorBits{}
	sensor := func(deviceID string) *sensorBits {
		if bits[deviceID] == nil {
			bits[deviceID] = &sensorBits{deviceID: deviceID}
		}
		return bits[deviceID]
	}
	for _, m := range h.Motions {
		if m.Motion.MotionValid {
			if m.Motion.Motion {
				sensor(m.Owner.RID).motion = "Motion"
			} else {
				sensor(m.Owner.RID).motion = "Clear"
			}
		}
	}
	for _, t := range h.Temperatures {
		if t.Temperature.TemperatureValid {
			sensor(t.Owner.RID).temp = fmt.Sprintf("%.1f°C", t.Temperature.Temperature)
		}
	}
	for _, level := range h.LightLevels {
		if level.Light.LightLevelValid {
			sensor(level.Owner.RID).lux = fmt.Sprintf("%d lx", luxFromLightLevel(level.Light.LightLevel))
		}
	}
	for _, c := range h.Contacts {
		switch c.ContactReport.State {
		case "contact":
			sensor(c.Owner.RID).contact = "Closed"
		case "no_contact":
			sensor(c.Owner.RID).contact = "Open"
		}
	}
	for _, b := range h.Buttons {
		sensor(b.Owner.RID).buttons = append(sensor(b.Owner.RID).buttons, b)
	}

	for _, bit := range bits {
		dev := devices[bit.deviceID]
		if dev.ID == "" || dev.isBridge(bridgeDevices) || controlled[bit.deviceID] {
			continue
		}
		value, kind := bit.summary(battery[bit.deviceID])
		if value == "" {
			continue
		}
		place(bit.deviceID).Sensors = append(place(bit.deviceID).Sensors, Sensor{
			ID:        bit.deviceID,
			Name:      nameOr(dev.Metadata.Name, "Sensor"),
			Kind:      kind,
			Value:     value,
			Reachable: deviceOnline(reachable, bit.deviceID),
		})
	}

	for _, sc := range h.Scenes {
		if sc.Metadata.Name == "" || sc.ID == "" {
			continue
		}
		target := other
		if sc.Group.RType == "room" {
			if g, ok := byRoom[sc.Group.RID]; ok {
				target = g
			}
		}
		kind := sc.Type
		if kind == "" {
			kind = "scene"
		}
		target.Scenes = append(target.Scenes, Scene{ID: sc.ID, Name: sc.Metadata.Name, Kind: kind})
	}

	var rooms []Group
	for _, room := range h.Rooms {
		g := byRoom[room.ID]
		if g == nil || groupEmpty(*g) {
			continue
		}
		sortGroup(g)
		rooms = append(rooms, *g)
	}
	sort.Slice(rooms, func(i, j int) bool {
		return strings.ToLower(rooms[i].Name) < strings.ToLower(rooms[j].Name)
	})
	if !groupEmpty(*other) {
		sortGroup(other)
		rooms = append(rooms, *other)
	}
	if rooms == nil {
		rooms = []Group{}
	}
	return rooms
}

type sensorBits struct {
	deviceID string
	motion   string
	temp     string
	lux      string
	contact  string
	buttons  []button
}

func (s *sensorBits) summary(battery string) (string, string) {
	var parts []string
	kind := "sensor"
	if s.motion != "" {
		parts = append(parts, s.motion)
		kind = "motion"
	}
	if s.contact != "" {
		parts = append(parts, s.contact)
		if kind == "sensor" {
			kind = "contact"
		}
	}
	if s.temp != "" {
		parts = append(parts, s.temp)
		if kind == "sensor" {
			kind = "climate"
		}
	}
	if s.lux != "" {
		parts = append(parts, s.lux)
		if kind == "sensor" {
			kind = "climate"
		}
	}
	if len(s.buttons) > 0 {
		parts = append(parts, newestButton(s.buttons))
		if kind == "sensor" {
			kind = "button"
		}
	}
	if battery != "" {
		parts = append(parts, battery)
	}
	return strings.Join(parts, " · "), kind
}

func newestButton(buttons []button) string {
	sort.Slice(buttons, func(i, j int) bool {
		return buttonTime(buttons[i]).After(buttonTime(buttons[j]))
	})
	best := buttons[0]
	event := best.Button.LastEvent
	if best.Button.Report != nil && best.Button.Report.Event != "" {
		event = best.Button.Report.Event
	}
	label := buttonEventLabel(event)
	if label == "" {
		return "No recent press"
	}
	if best.Metadata.ControlID > 0 {
		return fmt.Sprintf("Button %d · %s", best.Metadata.ControlID, label)
	}
	return label
}

func buttonTime(b button) time.Time {
	if b.Button.Report == nil || b.Button.Report.Updated == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, b.Button.Report.Updated)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func buttonEventLabel(event string) string {
	switch event {
	case "initial_press":
		return "pressed"
	case "repeat":
		return "held"
	case "short_release":
		return "short press"
	case "long_release":
		return "long press"
	case "double_short_release":
		return "double press"
	case "":
		return ""
	default:
		return strings.ReplaceAll(event, "_", " ")
	}
}

func (l light) on() bool {
	return l.On != nil && l.On.On
}

func isSwitch(l light) bool {
	switch strings.ToLower(l.Metadata.Archetype) {
	case "plug", "on_off_plug", "non_hue_on_off_plug":
		return true
	}
	return l.Dimming == nil && l.Color == nil && l.ColorTemperature == nil
}

func deviceOnline(reachable map[string]bool, deviceID string) bool {
	online, known := reachable[deviceID]
	return !known || online
}

func luxFromLightLevel(level float64) int {
	if level <= 0 {
		return 0
	}
	return int(math.Round(math.Pow(10, (level-1)/10000)))
}

func clampPercent(v float64) int {
	n := int(math.Round(v))
	if n < 0 {
		return 0
	}
	if n > 100 {
		return 100
	}
	return n
}

func nameOr(name, fallback string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return fallback
	}
	return name
}

func groupEmpty(g Group) bool {
	return len(g.Scenes) == 0 && len(g.Lights) == 0 && len(g.Switches) == 0 && len(g.Sensors) == 0
}

func sortGroup(g *Group) {
	sort.Slice(g.Scenes, func(i, j int) bool { return strings.ToLower(g.Scenes[i].Name) < strings.ToLower(g.Scenes[j].Name) })
	sort.Slice(g.Lights, func(i, j int) bool { return strings.ToLower(g.Lights[i].Name) < strings.ToLower(g.Lights[j].Name) })
	sort.Slice(g.Switches, func(i, j int) bool { return strings.ToLower(g.Switches[i].Name) < strings.ToLower(g.Switches[j].Name) })
	sort.Slice(g.Sensors, func(i, j int) bool { return strings.ToLower(g.Sensors[i].Name) < strings.ToLower(g.Sensors[j].Name) })
}
