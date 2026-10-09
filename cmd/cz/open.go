package main

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	// xdg-open can outlive the call on some desktops; reap it without blocking the poll loop.
	go func() { _ = cmd.Wait() }()
	return nil
}

// copyToClipboard tries each tool in turn. A headless box has none, so callers ignore
// the error: the code is printed regardless.
func copyToClipboard(text string) error {
	var tools [][]string
	switch runtime.GOOS {
	case "darwin":
		tools = [][]string{{"pbcopy"}}
	case "windows":
		tools = [][]string{{"clip.exe"}}
	default:
		tools = [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}, {"clip.exe"}}
	}
	err := exec.ErrNotFound
	for _, t := range tools {
		path, lookErr := exec.LookPath(t[0])
		if lookErr != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		cmd := exec.CommandContext(ctx, path, t[1:]...)
		cmd.Stdin = strings.NewReader(text)
		err = cmd.Run()
		cancel()
		if err == nil {
			return nil
		}
	}
	return err
}
