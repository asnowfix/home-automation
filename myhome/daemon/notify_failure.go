package daemon

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/asnowfix/home-automation/myhome/notify"
	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// notify-failure is the independent half of the daemon watchdog (#715). It is
// run by myhome-failure@.service (OnFailure= of myhome.service), as its own
// process, so it works while the daemon is down or hung. It sends through the
// same notify.Mailer, with the SMTP settings read exactly as `daemon run`
// reads them (env, .env, config file), and sends nothing for a scheduled
// RuntimeMaxSec restart, which systemd also reports as a failure (measured,
// see docs/715-daemon-watchdog-plan.md).

var notifyFailureDryRun bool

func init() {
	Cmd.AddCommand(notifyFailureCmd)
	notifyFailureCmd.Flags().BoolVar(&notifyFailureDryRun, "dry-run", false, "print the email instead of sending it")
}

var notifyFailureCmd = &cobra.Command{
	Use:   "notify-failure <unit>",
	Short: "Email an alert that a systemd unit failed (run by OnFailure=)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		log, err := logr.FromContext(ctx)
		if err != nil {
			return err
		}
		v := viper.New()
		v.SetConfigName("myhome")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("/etc/myhome/")
		v.AddConfigPath("$HOME/.myhome/")
		v.SetEnvPrefix("MYHOME")
		v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
		v.AutomaticEnv()
		v.SetDefault("smtp.host", "smtp.gmail.com")
		v.SetDefault("smtp.port", 587)
		if err := loadConfigFile(v, log); err != nil {
			return err
		}
		cfg := notify.Config{
			Host:     v.GetString("smtp.host"),
			Port:     v.GetInt("smtp.port"),
			Username: v.GetString("smtp.username"),
			Password: v.GetString("smtp.password"),
			From:     v.GetString("smtp.from"),
			To:       v.GetString("smtp.to"),
		}
		var mailer notify.Mailer = notify.New(log, cfg)
		if notifyFailureDryRun {
			mailer = printMailer{cmd.OutOrStdout()}
		} else if cfg.From == "" {
			fmt.Fprintln(cmd.ErrOrStderr(), "smtp.from is empty: email disabled, nothing sent")
		}
		return handleFailure(ctx, args[0], systemctlShow, mailer, cmd.OutOrStdout())
	},
}

// printMailer is the --dry-run sender: it never touches the network.
type printMailer struct{ w io.Writer }

func (p printMailer) Send(_ context.Context, subject, body string) error {
	_, err := fmt.Fprintf(p.w, "Subject: %s\n\n%s\n", subject, body)
	return err
}

// showFunc returns the `systemctl show` properties of a unit.
type showFunc func(ctx context.Context, unit string) (map[string]string, error)

func systemctlShow(ctx context.Context, unit string) (map[string]string, error) {
	out, err := exec.CommandContext(ctx, "systemctl", "show", unit,
		"-p", "Result,ExecMainCode,ExecMainStatus,NRestarts,RuntimeMaxUSec,"+
			"ActiveEnterTimestampMonotonic,InactiveEnterTimestampMonotonic").Output()
	if err != nil {
		return nil, fmt.Errorf("systemctl show %s: %w", unit, err)
	}
	props := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if k, val, ok := strings.Cut(line, "="); ok {
			props[k] = strings.TrimSpace(val)
		}
	}
	return props, nil
}

// handleFailure classifies the failure and sends one email, unless it was a
// scheduled RuntimeMaxSec restart. If systemd cannot be queried it alerts
// anyway: a false alarm is cheaper than a missed failure.
func handleFailure(ctx context.Context, unit string, show showFunc, mailer notify.Mailer, out io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	props, err := show(ctx, unit)
	if err != nil {
		props = map[string]string{"Result": "unknown (" + err.Error() + ")"}
	} else if isScheduledRestart(props) {
		fmt.Fprintf(out, "%s: scheduled RuntimeMaxSec restart, no alert\n", unit)
		return nil
	}

	host, _ := os.Hostname()
	subject := fmt.Sprintf("[myhome] %s failed on %s: %s", unit, host, props["Result"])
	body := fmt.Sprintf("Unit %s failed at %s.\n\nResult: %s\nExit code/status: %s/%s\nRestarts so far: %s\n\n"+
		"Result values: signal = killed (crash), watchdog = hung (heartbeat stopped), "+
		"exit-code = exited with an error, start-limit-hit = restart loop given up.\n",
		unit, time.Now().Format(time.RFC3339), props["Result"],
		props["ExecMainCode"], props["ExecMainStatus"], props["NRestarts"])
	if err := mailer.Send(ctx, subject, body); err != nil {
		// Never print the config: only the error text, which carries no credentials.
		return fmt.Errorf("sending failure email: %w", err)
	}
	return nil
}

// isScheduledRestart reports whether systemd's failure was the unit hitting
// its RuntimeMaxSec. Result=timeout alone also covers start and stop
// timeouts, so the runtime must also have reached the limit.
func isScheduledRestart(p map[string]string) bool {
	if p["Result"] != "timeout" {
		return false
	}
	limit, ok := parseTimespan(p["RuntimeMaxUSec"])
	if !ok || limit <= 0 {
		return false
	}
	active, err1 := strconv.ParseInt(p["ActiveEnterTimestampMonotonic"], 10, 64)
	inactive, err2 := strconv.ParseInt(p["InactiveEnterTimestampMonotonic"], 10, 64)
	if err1 != nil || err2 != nil || active <= 0 {
		return false
	}
	ran := time.Duration(inactive-active) * time.Microsecond
	return ran >= limit-time.Second
}

var timespanUnits = map[string]time.Duration{
	"us": time.Microsecond, "ms": time.Millisecond, "s": time.Second,
	"min": time.Minute, "h": time.Hour, "d": 24 * time.Hour,
	"w": 7 * 24 * time.Hour, "month": 30*24*time.Hour + 10*time.Hour + 30*time.Minute,
	"y": 365*24*time.Hour + 6*time.Hour,
}

// parseTimespan parses the human form `systemctl show` prints for durations
// ("4s", "2d", "1min 30s"). "infinity" and anything unparseable give false.
func parseTimespan(s string) (time.Duration, bool) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return 0, false
	}
	var total time.Duration
	for _, f := range fields {
		i := strings.IndexFunc(f, func(r rune) bool { return !unicode.IsDigit(r) && r != '.' })
		if i <= 0 {
			return 0, false
		}
		n, err := strconv.ParseFloat(f[:i], 64)
		unit, ok := timespanUnits[f[i:]]
		if err != nil || !ok {
			return 0, false
		}
		total += time.Duration(n * float64(unit))
	}
	return total, true
}
