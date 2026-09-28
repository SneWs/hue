package hue

import "testing"

func TestParseAvahiLine(t *testing.T) {
	line := `=;wlp1s0;IPv4;Philips hue - AB12;_hue._tcp;local;Philips-hue.local;192.168.1.42;443;"bridgeid=001788fffeab12cd" "modelid=BSB002"`
	got, ok := parseAvahiLine(line)
	if !ok {
		t.Fatal("expected a bridge")
	}
	if got.IP != "192.168.1.42" || got.ID != "001788FFFEAB12CD" || got.Model != "BSB002" {
		t.Fatalf("parsed %+v", got)
	}
	if got.Name != "Philips hue - AB12" {
		t.Fatalf("name %q", got.Name)
	}
}

func TestUnescapeAvahiName(t *testing.T) {
	line := `=;wlp1s0;IPv4;Hue\032Bridge\032-\0328848F4;_hue._tcp;local;Hue.local;192.168.1.214;443;"bridgeid=ECB5FAFFFE8848F4" "modelid=BSB002"`
	got, ok := parseAvahiLine(line)
	if !ok {
		t.Fatal("expected a bridge")
	}
	if got.Name != "Hue Bridge - 8848F4" {
		t.Fatalf("name %q", got.Name)
	}
}

func TestParseAvahiLineIgnoresUnresolved(t *testing.T) {
	if _, ok := parseAvahiLine("+;wlp1s0;IPv4;Philips hue;_hue._tcp;local"); ok {
		t.Fatal("unresolved browse line should be ignored")
	}
}

func TestDedupePrefersIPv4AndName(t *testing.T) {
	got := dedupeBridges([]Found{
		{ID: "ABC", IP: "fe80::1", Name: "Hue Bridge"},
		{ID: "abc", IP: "192.168.1.9", Name: "Living room bridge", Model: "BSB002"},
	})
	if len(got) != 1 {
		t.Fatalf("len %d", len(got))
	}
	if got[0].IP != "192.168.1.9" || got[0].Name != "Living room bridge" {
		t.Fatalf("%+v", got[0])
	}
}
