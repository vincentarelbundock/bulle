//go:build darwin

package backends

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/vincentarelbundock/bulle/internal/policy"
)

type seatbeltBackend struct{}

func newSeatbeltBackend() Backend { return seatbeltBackend{} }

func (seatbeltBackend) Run(p policy.Policy) error {
	if len(p.Command) == 0 {
		return fmt.Errorf("missing command")
	}
	profile := BuildSeatbeltProfile(p)
	file, err := os.CreateTemp("", "bulle-*.sbpl")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.WriteString(profile); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := closeUnexpectedFileDescriptors(); err != nil {
		return err
	}
	args := append([]string{"-f", file.Name()}, p.Command...)
	cmd := exec.Command("/usr/bin/sandbox-exec", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = envSlice(p.Env)
	cmd.Dir = p.ProjectPath
	return cmd.Run()
}
