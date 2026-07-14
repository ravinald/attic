package cmd

import (
	"maps"
	"testing"
)

func TestParseLabels(t *testing.T) {
	in := `# a comment
# <fingerprint>  <label>

67c031190db7  ravinald/bodega
8b88ecad3aa9  capy.cat
`
	got, err := parseLabels(in)
	if err != nil {
		t.Fatalf("parseLabels: %v", err)
	}
	want := map[string]labelEntry{
		"67c031190db7": {Label: "ravinald/bodega"},
		"8b88ecad3aa9": {Label: "capy.cat"},
	}
	if !maps.Equal(got.Hosts, want) {
		t.Fatalf("parseLabels = %v, want %v", got.Hosts, want)
	}
}

func TestParseLabelsRejects(t *testing.T) {
	cases := map[string]string{
		"missing label":   "67c031190db7\n",
		"extra column":    "67c031190db7 a b\n",
		"space in label":  "67c031190db7  a b\n", // fields() sees 3 tokens
		"invalid label":   "67c031190db7  /bad\n",
		"dup fingerprint": "67c031190db7  a\n67c031190db7  b\n",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseLabels(in); err == nil {
				t.Fatalf("parseLabels(%q) = nil error, want error", in)
			}
		})
	}
}

func TestRenderParseRoundTrip(t *testing.T) {
	doc := labelsDoc{Hosts: map[string]labelEntry{
		"975b7529c91e": {Label: "scaleapi/core-infrastructure"},
		"ea0acc9a9028": {Label: "ravinald/wifimgr"},
	}}
	got, err := parseLabels(renderLabels(doc))
	if err != nil {
		t.Fatalf("round-trip parse: %v", err)
	}
	if !maps.Equal(got.Hosts, doc.Hosts) {
		t.Fatalf("round-trip = %v, want %v", got.Hosts, doc.Hosts)
	}
}
