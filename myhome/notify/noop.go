package notify

import (
	"context"

	"github.com/go-logr/logr"
)

// noopMailer is returned by New when no From address is configured. It
// never touches the network — email is fully disabled, not erroring — so
// the daemon and CLI keep working exactly the same whether or not SMTP
// credentials have been set up via dpkg-reconfigure.
type noopMailer struct {
	log logr.Logger
}

func (m *noopMailer) Send(_ context.Context, subject, _ string) error {
	m.log.Info("email disabled (no SMTP From address configured in .env); skipping send", "subject", subject)
	return nil
}
