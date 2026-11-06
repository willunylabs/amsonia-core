package main

import "testing"

func TestPruneRequiresExplicitApply(t *testing.T) {
	options, err := sessionPruneOptions(nil)
	if err != nil || options.Apply || options.RetentionDays != 30 || options.BatchSize != 1000 {
		t.Fatalf("unsafe default: %#v %v", options, err)
	}
	options, err = sessionPruneOptions([]string{"--apply", "--batch-size=50", "--retention-days=60"})
	if err != nil || !options.Apply || options.BatchSize != 50 || options.RetentionDays != 60 {
		t.Fatalf("explicit options: %#v %v", options, err)
	}
	for _, args := range [][]string{{"--apply", "unexpected"}, {"--retention-days=0"}, {"--batch-size=5001"}, {"--unknown"}} {
		if _, err := sessionPruneOptions(args); err == nil {
			t.Fatalf("accepted %q", args)
		}
	}
}
