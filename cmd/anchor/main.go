package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

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
	if len(args) == 0 || args[0] != "ingest" {
		return errors.New("usage: anchor ingest --source codex")
	}

	fs := flag.NewFlagSet("ingest", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	// check if source exists?
	source := fs.String("source", "", "event source")
	if err := fs.Parse(args[1:]); err != nil {
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

	// capture ther raw event
	event, err := ingest.Capture(raw, *source, ingest.Options{})
	if err != nil {
		return err
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
