package router

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestMatchesString(t *testing.T) {
	tests := []struct {
		name            string
		operator        string
		pattern         string
		caseInsensitive bool
		input           string
		want            bool
	}{
		// equals
		{name: "equals: match", operator: "equals", pattern: "Felixstowe", input: "Felixstowe", want: true},
		{name: "equals: no match", operator: "equals", pattern: "Felixstowe", input: "Rotterdam", want: false},
		{name: "equals case-insensitive: match", operator: "equals", caseInsensitive: true, pattern: "Felixstowe", input: "FELIXSTOWE", want: true},

		// matches
		{name: "matches: * wildcard", operator: "matches", pattern: "*stowe", input: "Felixstowe", want: true},
		{name: "matches: ? wildcard", operator: "matches", pattern: "Felix?towe", input: "Felixstowe", want: true},
		{name: "matches: no match", operator: "matches", pattern: "*Rotterdam*", input: "Felixstowe", want: false},
		{name: "matches case-insensitive: match", operator: "matches", caseInsensitive: true, pattern: "*FELIX*", input: "port of felixstowe", want: true},

		// does_not_match
		{name: "does_not_match: non-matching input returns true", operator: "does_not_match", pattern: "*Rotterdam*", input: "Felixstowe", want: true},
		{name: "does_not_match: matching input returns false", operator: "does_not_match", pattern: "*Felix*", input: "Felixstowe", want: false},
		{name: "does_not_match case-insensitive: matching returns false", operator: "does_not_match", caseInsensitive: true, pattern: "*FELIX*", input: "felixstowe", want: false},

		// does_not_equal
		{name: "does_not_equal: different string returns true", operator: "does_not_equal", pattern: "Felixstowe", input: "Rotterdam", want: true},
		{name: "does_not_equal: same string returns false", operator: "does_not_equal", pattern: "Felixstowe", input: "Felixstowe", want: false},
		{name: "does_not_equal case-insensitive: same string different case returns false", operator: "does_not_equal", caseInsensitive: true, pattern: "Felixstowe", input: "FELIXSTOWE", want: false},

		// unknown operator
		{name: "unknown operator returns false", operator: "unknown", pattern: "Felixstowe", input: "Felixstowe", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := routingRule{
				matchPattern:      tt.pattern,
				operator:          tt.operator,
				isCaseInsensitive: tt.caseInsensitive,
			}
			got := matchesString(r, tt.input)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchesResult(t *testing.T) {
	tests := []struct {
		name     string
		operator string
		pattern  string
		json     string
		path     string
		want     bool
	}{
		{
			name:     "scalar: match",
			operator: "equals",
			pattern:  "Felixstowe",
			json:     `{"port": "Felixstowe"}`,
			path:     "port",
			want:     true,
		},
		{
			name:     "array: one element matches",
			operator: "equals",
			pattern:  "Felixstowe",
			json:     `{"ports": ["Rotterdam", "Felixstowe", "Hamburg"]}`,
			path:     "ports",
			want:     true,
		},
		{
			name:     "array: no element matches",
			operator: "equals",
			pattern:  "Felixstowe",
			json:     `{"ports": ["Rotterdam", "Hamburg"]}`,
			path:     "ports",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := routingRule{
				matchPattern: tt.pattern,
				operator:     tt.operator,
			}
			got := matchesResult(r, gjson.Get(tt.json, tt.path))
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestResolveAtSignKeys checks that a routing field entered by the user cannot invoke a gjson modifier:
// segments starting with '@' (e.g. JSON-LD "@value") must always be matched as literal keys.
// Without escaping, "@values", "@this" and "@reverse" would be applied as gjson modifiers.
//
// Each case has a decoy rule, checked first, that matches the value of a sibling key.
// A modifier that returns more than the target key (e.g. "@values" returns all the sibling values)
// would hit the decoy and route to the wrong ISN.
func TestResolveAtSignKeys(t *testing.T) {
	content := []byte(`{"location": {"identifier": {"@value": "GBDVR", "@values": "GBFXT", "@this": "GBSOU", "@reverse": "GBLIV"}}}`)

	tests := []struct {
		field        string
		pattern      string
		decoyPattern string
	}{
		{field: "location.identifier.@value", pattern: "GBDVR", decoyPattern: "GBFXT"},
		{field: "location.identifier.@values", pattern: "GBFXT", decoyPattern: "GBDVR"},
		{field: "location.identifier.@this", pattern: "GBSOU", decoyPattern: "GBDVR"},
		{field: "location.identifier.@reverse", pattern: "GBLIV", decoyPattern: "GBDVR"},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			c := &Cache{SignalRoutingConfigs: map[string]signalRoutingConfig{
				"test/v1.0.0": {
					routingField: tt.field, // as entered by the user
					routingRules: []routingRule{
						{matchPattern: tt.decoyPattern, operator: "equals", isnSlug: "wrong-isn"},
						{matchPattern: tt.pattern, operator: "equals", isnSlug: "isn-a"},
					},
				},
			}}

			_, isnSlug, ok := c.Resolve("test/v1.0.0", content)
			if !ok || isnSlug != "isn-a" {
				t.Errorf("want route to isn-a, got ok=%v isnSlug=%q", ok, isnSlug)
			}
		})
	}
}
