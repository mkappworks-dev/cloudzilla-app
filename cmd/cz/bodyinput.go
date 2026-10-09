package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

type bodyFlags struct {
	body string
	file string
}

func addBodyFlags(cmd *cobra.Command, b *bodyFlags) {
	cmd.Flags().StringVarP(&b.body, "body", "b", "", "body text")
	cmd.Flags().StringVarP(&b.file, "body-file", "F", "", "read the body from a file (- for standard input)")
}

// Overridden in tests; the real one runs the editor on the file with the terminal attached.
var runEditor = func(ctx context.Context, a *app, editor, path string) error {
	parts := strings.Fields(editor)
	cmd := exec.CommandContext(ctx, parts[0], append(parts[1:], path)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, a.stdout, a.stderr
	return cmd.Run()
}

// readBody returns the body from --body, --body-file or, on a terminal with neither, $VISUAL/$EDITOR; otherwise it is empty.
func (a *app) readBody(ctx context.Context, b bodyFlags, cmd *cobra.Command) (string, error) {
	hasBody, hasFile := cmd.Flags().Changed("body"), b.file != ""
	switch {
	case hasBody && hasFile:
		return "", errors.New("use either --body or --body-file, not both")
	case hasBody:
		return b.body, nil
	case hasFile:
		var data []byte
		var err error
		if b.file == "-" {
			data, err = io.ReadAll(a.stdin)
		} else {
			data, err = os.ReadFile(b.file)
		}
		return string(data), err
	case a.stdinTTY && a.stdoutTTY:
		return a.editBody(ctx)
	}
	return "", nil
}

func (a *app) editBody(ctx context.Context) (string, error) {
	editor := a.getenv("VISUAL")
	if editor == "" {
		editor = a.getenv("EDITOR")
	}
	if strings.TrimSpace(editor) == "" {
		return "", errors.New("no body given and neither $VISUAL nor $EDITOR is set; use --body or --body-file")
	}
	f, err := os.CreateTemp("", "cz-body-*.md")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	if err := f.Close(); err != nil {
		return "", err
	}
	if err := runEditor(ctx, a, editor, f.Name()); err != nil {
		return "", errors.New("editor failed: " + err.Error())
	}
	data, err := os.ReadFile(f.Name())
	return strings.TrimSpace(string(data)), err
}
