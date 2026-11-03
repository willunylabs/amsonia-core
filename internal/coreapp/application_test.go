package coreapp

import (
	"strings"
	"testing"
)

func TestApplicationOptions(t *testing.T) {
	options, err := ApplicationOptionsFromEnv(func(string) string { return "" })
	if err != nil || options.TenancyMode != "single" || options.OrganizationName != "My organization" {
		t.Fatalf("defaults: %+v %v", options, err)
	}
	for _, input := range []ApplicationOptions{
		{TenancyMode: "unknown"},
		{TenancyMode: "multi", OrganizationID: "existing"},
		{OrganizationID: "bad\nID"},
		{OrganizationName: strings.Repeat("x", 81)},
	} {
		if _, err := normalizeApplicationOptions(input); err == nil {
			t.Fatalf("accepted invalid config: %+v", input)
		}
	}
	options, err = normalizeApplicationOptions(ApplicationOptions{TenancyMode: " multi "})
	if err != nil || options.TenancyMode != "multi" {
		t.Fatalf("multi mode: %+v %v", options, err)
	}
}
