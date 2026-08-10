package main

import (
	"os/exec"
)

// runCommand runs a command and returns an error including its output.
func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return &execError{name: name, args: args, out: string(out), err: err}
	}
	return nil
}

type execError struct {
	name string
	args []string
	out  string
	err  error
}

func (e *execError) Error() string {
	return e.name + ": " + e.err.Error() + ": " + e.out
}
