package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/willunylabs/amsonia/internal/coresync"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	flags := flag.NewFlagSet("core-sync", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	mode := flags.String("mode", "sync", "operation mode")
	manifestPath := flags.String("manifest", "", "manifest path")
	sourceRoot := flags.String("source-root", "", "source root")
	destinationRoot := flags.String("destination-root", "", "destination root")
	sourceCommit := flags.String("source-commit", "", "source commit")
	provenancePath := flags.String("provenance", "", "provenance path relative to the destination root")
	if err := flags.Parse(arguments); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("positional arguments are not allowed: %q", strings.Join(flags.Args(), " "))
	}

	switch *mode {
	case "sync", "check":
		required := []struct {
			name  string
			value string
		}{
			{name: "manifest", value: *manifestPath},
			{name: "source-root", value: *sourceRoot},
			{name: "destination-root", value: *destinationRoot},
			{name: "source-commit", value: *sourceCommit},
			{name: "provenance", value: *provenancePath},
		}
		for _, option := range required {
			if strings.TrimSpace(option.value) == "" {
				return fmt.Errorf("--%s is required for %s mode", option.name, *mode)
			}
		}

		manifest, err := coresync.LoadManifest(*manifestPath)
		if err != nil {
			return err
		}
		return coresync.Sync(manifest, coresync.Options{
			SourceRoot:      *sourceRoot,
			DestinationRoot: *destinationRoot,
			SourceCommit:    *sourceCommit,
			ProvenancePath:  *provenancePath,
			Check:           *mode == "check",
		})
	case "verify":
		if strings.TrimSpace(*destinationRoot) == "" {
			return fmt.Errorf("--destination-root is required for verify mode")
		}
		if strings.TrimSpace(*provenancePath) == "" {
			return fmt.Errorf("--provenance is required for verify mode")
		}
		return coresync.Verify(*destinationRoot, *provenancePath)
	default:
		return fmt.Errorf("unknown mode %q: allowed modes are sync, check, verify", *mode)
	}
}
