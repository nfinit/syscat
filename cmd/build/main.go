// Build Syscat from the repository root with a modified-source timestamp.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	output := "syscat"
	if os.Getenv("GOOS") == "windows" || (os.Getenv("GOOS") == "" && runtime.GOOS == "windows") {
		output += ".exe"
	}
	flag.StringVar(&output, "o", output, "output executable")
	flag.Parse()
	if flag.NArg() != 0 {
		return fmt.Errorf("unexpected arguments; use --help for options")
	}
	stamp, err := modifiedTimestamp()
	if err != nil {
		return err
	}
	args := []string{"build", "-o", output}
	if stamp != "" {
		args = append(args, "-ldflags", "-X syscat/internal/buildinfo.modifiedAt="+stamp)
	}
	args = append(args, "./cmd/syscat")
	cmd := exec.Command("go", args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func modifiedTimestamp() (string, error) {
	rootBytes, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		// Source archives and hosts without Git can still produce an executable.
		return "", nil
	}
	root := strings.TrimSpace(string(rootBytes))
	var latest time.Time
	changed := false
	for _, args := range [][]string{
		{"diff", "--name-only", "--no-renames", "-z", "HEAD"},
		{"ls-files", "--others", "--exclude-standard", "-z"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		output, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("read modified source paths: %w", err)
		}
		for _, name := range bytes.Split(output, []byte{0}) {
			if len(name) == 0 {
				continue
			}
			changed = true
			info, err := os.Lstat(filepath.Join(root, string(name)))
			if os.IsNotExist(err) {
				continue // A deleted file has no remaining modification time.
			}
			if err != nil {
				return "", fmt.Errorf("inspect modified source %q: %w", name, err)
			}
			if !info.IsDir() && info.ModTime().After(latest) {
				latest = info.ModTime()
			}
		}
	}
	if !changed {
		return "", nil
	}
	if latest.IsZero() {
		// Deletion-only or submodule-only changes have no usable file timestamp.
		latest = time.Now()
	}
	return strconv.FormatInt(latest.Unix(), 10), nil
}
