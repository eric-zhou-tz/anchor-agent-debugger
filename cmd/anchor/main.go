package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"anchor/internal/ingest"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run executes the CLI using injected streams so command behavior stays testable.
func run(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: anchor <enable|disable|status|ingest>")
	}

	switch args[0] {
	case "enable":
		return runEnable(args[1:], stdout)
	case "disable":
		return runDisable(args[1:], stdout)
	case "status":
		return runStatus(args[1:], stdout)
	case "ingest":
		return runIngest(args[1:], stdin, stdout)
	default:
		return errors.New("usage: anchor <enable|disable|status|ingest>")
	}
}

func runIngest(args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("ingest", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	// check if source exists?
	source := fs.String("source", "", "event source")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *source == "" {
		return ingest.ErrMissingSource
	}

	// read input stream
	raw, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}

	// Capture and persist the hook event. The CLI still echoes the captured
	// event envelope for direct local use.
	event, _, err := ingest.Ingest(raw, *source, ingest.Options{})
	if err != nil {
		return err
	}
	if event == nil {
		return nil
	}

	// convert to json
	encoded, err := json.MarshalIndent(event, "", "  ")
	if err != nil {
		return fmt.Errorf("encode event: %w", err)
	}

	// write output
	_, err = fmt.Fprintln(stdout, string(encoded))
	return err
}

func runEnable(args []string, stdout io.Writer) error {
	cwd, err := cwdFlag("enable", args)
	if err != nil {
		return err
	}
	if err := ingest.EnableProject(cwd); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "Anchor enabled for %s\n", cwd)
	return err
}

func runDisable(args []string, stdout io.Writer) error {
	cwd, err := cwdFlag("disable", args)
	if err != nil {
		return err
	}
	if err := ingest.DisableProject(cwd); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "Anchor disabled for %s\n", cwd)
	return err
}

func runStatus(args []string, stdout io.Writer) error {
	cwd, err := cwdFlag("status", args)
	if err != nil {
		return err
	}
	enabled, err := ingest.IsProjectEnabled(cwd)
	if err != nil {
		return err
	}

	state := "disabled"
	if enabled {
		state = "enabled"
	}
	_, err = fmt.Fprintf(stdout, "Anchor %s for %s\nstorage: %s\n", state, cwd, ingest.ProjectStorageRoot(cwd))
	return err
}

func cwdFlag(name string, args []string) (string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cwd := fs.String("cwd", ".", "project cwd")
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	return filepath.Abs(*cwd)
}
