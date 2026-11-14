package ui

import (
	"strings"
	"testing"
)

// htmx reads a trigger's `from:` selector up to the next space, so a descendant
// selector there ("from:#form input") leaves a stray word and raises
// htmx:syntax:error (#822). The filter form listens on itself instead: change and
// keyup bubble up from its inputs.
func TestEventsFilterTriggerParses(t *testing.T) {
	const attr = `hx-trigger="`
	i := strings.Index(eventsTableTemplate, attr)
	if i < 0 {
		t.Fatal("no hx-trigger on the events filter form")
	}
	trig := eventsTableTemplate[i+len(attr):]
	trig = trig[:strings.Index(trig, `"`)]
	for _, part := range strings.Split(trig, ",") {
		fields := strings.Fields(part)
		for j, f := range fields {
			if strings.HasPrefix(f, "from:") && j+1 < len(fields) && !strings.Contains(fields[j+1], ":") {
				t.Errorf("trigger %q: from: selector is followed by stray %q (spaces end the selector)", strings.TrimSpace(part), fields[j+1])
			}
		}
	}
	if !strings.Contains(trig, "change") || !strings.Contains(trig, "keyup delay:400ms") {
		t.Errorf("filter must still react to change and to typing: %q", trig)
	}
}
