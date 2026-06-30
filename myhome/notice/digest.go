package notice

import (
	"fmt"
	"strings"
	"time"

	"github.com/asnowfix/home-automation/myhome/events"
)

// formatDigest renders the subject and plain-text body of the daily digest
// email from a set of notice-severity events. Pure function for testability;
// notices are expected pre-sorted newest-first (events.Storage.Query orders
// by ts DESC).
func formatDigest(notices []events.Event, now time.Time) (subject, body string) {
	count := len(notices)
	plural := "s"
	if count == 1 {
		plural = ""
	}
	subject = fmt.Sprintf("MyHome notice digest — %s (%d notice%s)", now.Format("2006-01-02"), count, plural)

	var b strings.Builder
	if count == 0 {
		b.WriteString("No notices in the last 24 hours.\n")
		return subject, b.String()
	}

	for _, n := range notices {
		ts := time.Unix(int64(n.Ts), 0).Format("15:04")
		fmt.Fprintf(&b, "%s  %-20s %-10s %s", ts, n.DeviceID, n.Component, n.Event)
		if n.Data != nil && *n.Data != "" {
			fmt.Fprintf(&b, "  %s", *n.Data)
		}
		b.WriteString("\n")
	}
	return subject, b.String()
}
