// Command rso manages RSO cloud resources from the command line.
package main

import (
	"context"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/theorigamicorporation/rso-cli/internal/cli"
)

// version is set at build time (-X main.version=...).
var version = "dev"

// buildVersion is the stamped version, or the module version when the
// binary was built with "go install module@version".
func buildVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Run(ctx, cli.OSEnv(buildVersion()), os.Args[1:])
	stop()
	os.Exit(code)
}
