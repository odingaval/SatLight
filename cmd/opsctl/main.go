// Command opsctl operates the kill switch: it proposes and approves
// two-person resumes and reports the effective pause state.
//
// Usage:
//
//	opsctl status
//	opsctl propose --flag outflows_paused --as alice --reason "reconciliation green"
//	opsctl approve --id 3 --as bob
//	opsctl reject --id 3 --as bob
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/satlight/satlight/internal/config"
	"github.com/satlight/satlight/internal/db"
	"github.com/satlight/satlight/internal/ops"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cfg := config.MustLoad()
	pool, err := db.NewPool(cfg)
	if err != nil {
		fatal(err)
	}
	defer pool.Close()
	service := ops.NewService(cfg, pool)
	ctx := context.Background()

	switch os.Args[1] {
	case "status":
		status, err := service.Status(ctx)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("funding_paused: %v\noutflows_paused: %v (env override: %v)\n", status.FundingPaused, status.OutflowsPaused, status.OutflowsPausedByEnv)
		if status.Message != "" {
			fmt.Println(status.Message)
		}
	case "propose":
		fs := flag.NewFlagSet("propose", flag.ExitOnError)
		flagName := fs.String("flag", "", "funding_paused or outflows_paused")
		as := fs.String("as", "", "operator identity, e.g. alice@example.com")
		reason := fs.String("reason", "", "why it is safe to resume")
		_ = fs.Parse(os.Args[2:])
		id, err := service.ProposeResume(ctx, *flagName, *as, *reason)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("proposal %d opened for %s; a second operator must approve it\n", id, *flagName)
	case "approve":
		fs := flag.NewFlagSet("approve", flag.ExitOnError)
		id := fs.Int64("id", 0, "proposal id from propose")
		as := fs.String("as", "", "operator identity (must differ from proposer)")
		_ = fs.Parse(os.Args[2:])
		if *id <= 0 {
			fatal(fmt.Errorf("approve requires --id"))
		}
		if err := service.ApproveResume(ctx, *id, *as); err != nil {
			fatal(err)
		}
		fmt.Printf("proposal %d approved; flag cleared\n", *id)
	case "reject":
		fs := flag.NewFlagSet("reject", flag.ExitOnError)
		id := fs.Int64("id", 0, "proposal id from propose")
		as := fs.String("as", "", "operator identity")
		_ = fs.Parse(os.Args[2:])
		if *id <= 0 {
			fatal(fmt.Errorf("reject requires --id"))
		}
		if err := service.RejectResume(ctx, *id, *as); err != nil {
			fatal(err)
		}
		fmt.Printf("proposal %d rejected\n", *id)
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: opsctl <status|propose|approve|reject> [flags]")
	fmt.Fprintln(os.Stderr, "  propose --flag <name> --as <operator> --reason <why>")
	fmt.Fprintln(os.Stderr, "  approve --id <proposal> --as <operator>")
	fmt.Fprintln(os.Stderr, "  reject --id <proposal> --as <operator>")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "opsctl:", err)
	os.Exit(1)
}
