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
  events count          Count events
  replay start          Start a replay
  replay status <id>    Show replay status
  dlq list              List dead-letter events
  dlq retry <id>        Retry a dead-letter event

Use "relayctl <command> --help" for more information.
`

var (
	cfg    config.Config
	apiURL string
	apiKey string
)

func main() {
	cfg = config.MustLoad()
	telemetry.SetBuildInfo("0.1.0", "relayctl")

	// Get API URL and key from env or flags
	apiURL = os.Getenv("RELAYDB_API_URL")
	if apiURL == "" {
		apiURL = "http://localhost:8080"
	}

	apiKey = os.Getenv("RELAYDB_API_KEY")
	if apiKey == "" {
		apiKey = cfg.ReaderAPIKeyID + ":" + cfg.ReaderAPIKey
	}

	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "source":
		handleSource(args)
	case "events":
		handleEvents(args)
	case "replay":
		handleReplay(args)
	case "dlq":
		handleDLQ(args)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n%s", cmd, usage)
		os.Exit(1)
	}
}

func handleSource(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "source command required: list, status")
		os.Exit(1)
	}

	subcmd := args[0]
	switch subcmd {
	case "list":
		sourceList()
	case "status":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "source id required")
			os.Exit(1)
		}
		sourceStatus(args[1])
	default:
		fmt.Fprintf(os.Stderr, "unknown source command: %s\n", subcmd)
		os.Exit(1)
	}
}

func handleEvents(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "events command required: tail, count")
		os.Exit(1)
	}

	subcmd := args[0]
	switch subcmd {
	case "tail":
		eventsTail(args[1:])
	case "count":
		eventsCount(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown events command: %s\n", subcmd)
		os.Exit(1)
	}
}

func handleReplay(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "replay command required: start, status, list")
		os.Exit(1)
	}

	subcmd := args[0]
	switch subcmd {
	case "start":
		fmt.Println("replay start not yet implemented")
	case "status":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "replay id required")
			os.Exit(1)
		}
		fmt.Printf("replay status %s not yet implemented\n", args[1])
	case "list":
		fmt.Println("replay list not yet implemented")
	default:
		fmt.Fprintf(os.Stderr, "unknown replay command: %s\n", subcmd)
		os.Exit(1)
	}
}

func handleDLQ(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "dlq command required: list, retry")
		os.Exit(1)
	}

	subcmd := args[0]
	switch subcmd {
	case "list":
		dlqList()
	case "retry":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "dlq entry id required")
			os.Exit(1)
		}
		dlqRetry(args[1])
	default:
		fmt.Fprintf(os.Stderr, "unknown dlq command: %s\n", subcmd)
		os.Exit(1)
	}
}

func dlqList() {
	fmt.Println("DLQ list:")
	fmt.Println("  (not yet implemented)")
}

func dlqRetry(id string) {
	fmt.Printf("Retrying DLQ entry %s (not yet implemented)\n", id)
}

func sourceList() {
	fmt.Println("source list not yet implemented")
}

func sourceStatus(id string) {
	fmt.Printf("source status %s not yet implemented\n", id)
}

func eventsTail(args []string) {
	fmt.Println("events tail not yet implemented")
}

func eventsCount(args []string) {
	fmt.Println("events count not yet implemented")
}
