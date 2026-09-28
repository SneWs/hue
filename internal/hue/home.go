package hue

import (
	"encoding/json"
	"strings"
)

// Ref points at another CLIP resource.
type Ref struct {
	RID   string `json:"rid"`
	RType string `json:"rtype"`
}

// Home is the subset of bridge resources the panel knows how to draw.
type Home struct {
	Devices       []device
	Rooms         []room
	Scenes        []scene
	Lights        []light
	GroupedLights []groupedLight
	Motions       []motion
	Temperatures  []temperature
	LightLevels   []lightLevel
	Buttons       []button
	Contacts      []contact
	Powers        []devicePower
	Zigbees       []zigbee
	Bridges       []bridgeResource
}

type device struct {
	ID       string `json:"id"`
	Metadata struct {
		Name      string `json:"name"`
		Archetype string `json:"archetype"`
	} `json:"metadata"`
	ProductData struct {
		ModelID     string `json:"model_id"`
		ProductName string `json:"product_name"`
	} `json:"product_data"`
	Services []Ref `json:"services"`
}

type room struct {
	ID       string `json:"id"`
	Metadata struct {
		Name      string `json:"name"`
		Archetype string `json:"archetype"`
	} `json:"metadata"`
	Children []Ref `json:"children"`
	Services []Ref `json:"services"`
}

type scene struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Group Ref `json:"group"`
}

type light struct {
	ID       string `json:"id"`
	Owner    Ref    `json:"owner"`
	Metadata struct {
		Name      string `json:"name"`
		Archetype string `json:"archetype"`
	} `json:"metadata"`
	On *struct {
		On bool `json:"on"`
	} `json:"on"`
	Dimming *struct {
		Brightness float64 `json:"brightness"`
	} `json:"dimming"`
	ColorTemperature *struct {
		Mirek       *float64 `json:"mirek"`
		MirekValid  bool     `json:"mirek_valid"`
		MirekSchema struct {
			Min int `json:"mirek_minimum"`
			Max int `json:"mirek_maximum"`
		} `json:"mirek_schema"`
	} `json:"color_temperature"`
	Color *struct {
		XY struct {
			X float64 `json:"x"`
			Y float64 `json:"y"`
		} `json:"xy"`
		Gamut *struct {
			Red   Point `json:"red"`
			Green Point `json:"green"`
			Blue  Point `json:"blue"`
		} `json:"gamut"`
		GamutType string `json:"gamut_type"`
	} `json:"color"`
}

type groupedLight struct {
	ID    string `json:"id"`
	Owner Ref    `json:"owner"`
	On    *struct {
		On bool `json:"on"`
	} `json:"on"`
	Dimming *struct {
		Brightness float64 `json:"brightness"`
	} `json:"dimming"`
}

type motion struct {
	ID     string `json:"id"`
	Owner  Ref    `json:"owner"`
	Motion struct {
		Motion      bool `json:"motion"`
		MotionValid bool `json:"motion_valid"`
	} `json:"motion"`
}

type temperature struct {
	ID          string `json:"id"`
	Owner       Ref    `json:"owner"`
	Temperature struct {
		Temperature      float64 `json:"temperature"`
		TemperatureValid bool    `json:"temperature_valid"`
	} `json:"temperature"`
}

type lightLevel struct {
	ID    string `json:"id"`
	Owner Ref    `json:"owner"`
	Light struct {
		LightLevel      float64 `json:"light_level"`
		LightLevelValid bool    `json:"light_level_valid"`
	} `json:"light"`
}

type button struct {
	ID       string `json:"id"`
	Owner    Ref    `json:"owner"`
	Metadata struct {
		ControlID int `json:"control_id"`
	} `json:"metadata"`
	Button struct {
		LastEvent string `json:"last_event"`
		Report    *struct {
			Event   string `json:"event"`
			Updated string `json:"updated"`
		} `json:"button_report"`
	} `json:"button"`
}

type contact struct {
	ID            string `json:"id"`
	Owner         Ref    `json:"owner"`
	ContactReport struct {
		State string `json:"state"`
	} `json:"contact_report"`
}

type devicePower struct {
	ID         string `json:"id"`
	Owner      Ref    `json:"owner"`
	PowerState struct {
		BatteryLevel *int   `json:"battery_level"`
		BatteryState string `json:"battery_state"`
	} `json:"power_state"`
}

type zigbee struct {
	ID     string `json:"id"`
	Owner  Ref    `json:"owner"`
	Status string `json:"status"`
}

type bridgeResource struct {
	ID       string `json:"id"`
	BridgeID string `json:"bridge_id"`
	Owner    Ref    `json:"owner"`
}

// Decode sorts a /clip/v2/resource payload into the resource types we display.
func Decode(items []json.RawMessage) Home {
	var home Home
	for _, raw := range items {
		var head struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &head); err != nil {
			continue
		}
		switch head.Type {
		case "device":
			var v device
			if json.Unmarshal(raw, &v) == nil {
				home.Devices = append(home.Devices, v)
			}
		case "room":
			var v room
			if json.Unmarshal(raw, &v) == nil {
				home.Rooms = append(home.Rooms, v)
			}
		case "scene", "smart_scene":
			var v scene
			if json.Unmarshal(raw, &v) == nil {
				v.Type = head.Type
				home.Scenes = append(home.Scenes, v)
			}
		case "light":
			var v light
			if json.Unmarshal(raw, &v) == nil {
				home.Lights = append(home.Lights, v)
			}
		case "grouped_light":
			var v groupedLight
			if json.Unmarshal(raw, &v) == nil {
				home.GroupedLights = append(home.GroupedLights, v)
			}
		case "motion":
			var v motion
			if json.Unmarshal(raw, &v) == nil {
				home.Motions = append(home.Motions, v)
			}
		case "temperature":
			var v temperature
			if json.Unmarshal(raw, &v) == nil {
				home.Temperatures = append(home.Temperatures, v)
			}
		case "light_level":
			var v lightLevel
			if json.Unmarshal(raw, &v) == nil {
				home.LightLevels = append(home.LightLevels, v)
			}
		case "button":
			var v button
			if json.Unmarshal(raw, &v) == nil {
				home.Buttons = append(home.Buttons, v)
			}
		case "contact":
			var v contact
			if json.Unmarshal(raw, &v) == nil {
				home.Contacts = append(home.Contacts, v)
			}
		case "device_power":
			var v devicePower
			if json.Unmarshal(raw, &v) == nil {
				home.Powers = append(home.Powers, v)
			}
		case "zigbee_connectivity":
			var v zigbee
			if json.Unmarshal(raw, &v) == nil {
				home.Zigbees = append(home.Zigbees, v)
			}
		case "bridge":
			var v bridgeResource
			if json.Unmarshal(raw, &v) == nil {
				home.Bridges = append(home.Bridges, v)
			}
		}
	}
	return home
}

func (l light) gamut() Gamut {
	if l.Color != nil && l.Color.Gamut != nil {
		g := l.Color.Gamut
		if g.Red.X > 0 && g.Green.Y > 0 && g.Blue.X > 0 {
			return Gamut{Red: g.Red, Green: g.Green, Blue: g.Blue}
		}
	}
	if l.Color != nil && l.Color.GamutType != "" {
		return GamutByType(l.Color.GamutType)
	}
	return GamutC()
}

func (d device) isBridge(bridgeDevices map[string]bool) bool {
	if bridgeDevices[d.ID] {
		return true
	}
	arch := strings.ToLower(d.Metadata.Archetype)
	if arch == "bridge_v2" || arch == "bridge" {
		return true
	}
	model := strings.ToUpper(d.ProductData.ModelID)
	return strings.HasPrefix(model, "BSB")
}
