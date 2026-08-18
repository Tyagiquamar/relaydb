package main

import (
	"fmt"
	"os"

	"github.com/tyagiquamar/relaydb/internal/config"
	"github.com/tyagiquamar/relaydb/internal/telemetry"
)

const usage = `relayctl - RelayDB CLI

Usage:
  relayctl <command> [options]

Commands:
  source list           List sources
  source status <id>    Show source status
  events tail           Tail events
  replay start          Start a replay
  replay status <id>    Show replay status
  dlq list              List dead-letter events
  dlq retry <id>        Retry a dead-letter event

Use "relayctl <command> --help" for more information.
`

func main() {
	cfg := config.MustLoad()
	_ = cfg

	telemetry.SetBuildInfo("0.1.0", "relayctl")

	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "source":
		fmt.Println("source commands not yet implemented")
	case "events":
		fmt.Println("events commands not yet implemented")
	case "replay":
		fmt.Println("replay commands not yet implemented")
	case "dlq":
		fmt.Println("dlq commands not yet implemented")
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n%s", cmd, usage)
		os.Exit(1)
	}
}