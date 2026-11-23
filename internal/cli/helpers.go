package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/theorigamicorporation/rsodev-go/platform"
)

// isCobraUsage recognises cobra's own errors about the command line.
func isCobraUsage(err error) bool {
	message := err.Error()
	for _, marker := range []string{"unknown command", "unknown flag", "unknown shorthand flag", "flag needs an argument", "invalid argument", "accepts ", "requires at least", "requires at most", "required flag"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

// openBrowser asks the desktop to open a URL; failing is fine, the URL is
// printed as well.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

// isTerminal reports whether r is a terminal a person is typing into.
func isTerminal(r io.Reader) bool {
	file, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// confirm asks before something that cannot be undone. With --yes it does
// not ask; without a terminal to ask on it refuses instead of guessing.
func (a *app) confirm(cmd *cobra.Command, yes bool, question string) error {
	if yes {
		return nil
	}
	if !isTerminal(a.env.In) {
		return usagef("%s Pass --yes to confirm", question)
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s [y/N] ", question)
	answer, _ := bufio.NewReader(a.env.In).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return nil
	}
	return usagef("cancelled")
}

// readAll reads a value from standard input, for things that must not be on
// the command line (secrets) or do not fit there (message bodies).
func (a *app) readAll() (string, error) {
	if a.env.In == nil {
		return "", nil
	}
	raw, err := io.ReadAll(io.LimitReader(a.env.In, 8<<20))
	return string(raw), err
}

// when shortens an RFC 3339 time to minutes in UTC; anything else is shown
// as it came.
func when(value string) string {
	if value == "" {
		return ""
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC().Format("2006-01-02 15:04")
		}
	}
	return value
}

// count prints a whole number that the API sends as a float.
func count(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// bytesize prints a size the way people read it.
func bytesize(value float64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	i := 0
	for value >= 1024 && i < len(units)-1 {
		value /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f %s", value, units[i])
	}
	return fmt.Sprintf("%.1f %s", value, units[i])
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

// keyValues parses repeated --flag name=value pairs.
func keyValues(flag string, pairs []string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		name, value, found := strings.Cut(pair, "=")
		if !found || name == "" {
			return nil, usagef("--%s takes name=value, got %q", flag, pair)
		}
		out[name] = value
	}
	return out, nil
}

// exactlyOne checks that one and only one of two alternatives was given.
func exactlyOne(aName, a, bName, b string) error {
	if (a == "") == (b == "") {
		return usagef("give exactly one of --%s and --%s", aName, bName)
	}
	return nil
}

// sdk runs a command body with a client for the tenant in use.
func (a *app) sdk(run func(cmd *cobra.Command, api *platform.Client, args []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		api, err := a.client(false)
		if err != nil {
			return err
		}
		return run(cmd, api, args)
	}
}

// deleteCommand is a "delete <name>" that asks first. remove is the body,
// usually wrapped with sdk.
func (a *app) deleteCommand(use, short, question, done string, remove func(cmd *cobra.Command, args []string) error) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(strings.Count(use, "<")),
		RunE: func(cmd *cobra.Command, args []string) error {
			names := make([]any, len(args))
			for i, arg := range args {
				names[i] = arg
			}
			if err := a.confirm(cmd, yes, fmt.Sprintf(question, names...)); err != nil {
				return err
			}
			if err := remove(cmd, args); err != nil {
				return err
			}
			a.note(cmd, done, names...)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	return cmd
}

// deref reads an optional text of the API; absent is empty.
func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// notFoundf is the error for something the CLI looked up by name itself; it
// exits like a NOT_FOUND answer of the API.
func notFoundf(format string, args ...any) error {
	return &platform.Error{Errors: []platform.GraphQLError{{Message: fmt.Sprintf(format, args...), Extensions: map[string]any{"code": platform.CodeNotFound}}}}
}
