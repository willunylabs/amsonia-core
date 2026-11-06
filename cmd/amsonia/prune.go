package main

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willunylabs/amsonia-core/internal/coreapp"
	"github.com/willunylabs/amsonia-core/postgres"
)

func sessionPruneOptions(args []string) (coreapp.SessionPruneOptions, error) {
	var options coreapp.SessionPruneOptions
	flags := flag.NewFlagSet("prune-sessions", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.BoolVar(&options.Apply, "apply", false, "delete eligible rows; default is preview")
	flags.IntVar(&options.RetentionDays, "retention-days", 30, "retain records this many days after expiry")
	flags.IntVar(&options.BatchSize, "batch-size", 1000, "maximum rows per table per invocation")
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	if flags.NArg() != 0 {
		return options, errUsage
	}
	return options, options.Validate()
}

func pruneSessions(ctx context.Context, pool *pgxpool.Pool, options coreapp.SessionPruneOptions) error {
	if err := postgres.VerifyMigrationState(ctx, pool); err != nil {
		return err
	}
	result, err := coreapp.PruneSessions(ctx, pool, options)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
