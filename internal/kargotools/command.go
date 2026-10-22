package kargotools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

const (
	maximumDiffBytes       = 16 << 20
	maximumCommandErrorLen = 64 << 10
)

func runCommand(ctx context.Context, binary string, arguments []string, environment []string) ([]byte, error) {
	command := exec.CommandContext(ctx, binary, arguments...)
	command.Env = environment
	stdout := &cappedBuffer{limit: maximumDiffBytes}
	stderr := &cappedBuffer{limit: maximumCommandErrorLen}
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	if stdout.exceeded {
		return nil, fmt.Errorf("command output exceeds %d bytes", maximumDiffBytes)
	}
	if stderr.exceeded {
		return nil, fmt.Errorf("command error output exceeds %d bytes", maximumCommandErrorLen)
	}
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %s", err, message)
	}

	return stdout.Bytes(), nil
}

func setEnvironment(environment []string, name, value string) []string {
	prefix := name + "="
	filtered := make([]string, 0, len(environment)+1)
	for _, item := range environment {
		if !strings.HasPrefix(item, prefix) {
			filtered = append(filtered, item)
		}
	}

	return append(filtered, prefix+value)
}

type cappedBuffer struct {
	data     bytes.Buffer
	limit    int
	exceeded bool
}

func (b *cappedBuffer) Write(value []byte) (int, error) {
	written := len(value)
	remaining := b.limit - b.data.Len()
	if remaining > 0 {
		end := min(len(value), remaining)
		_, _ = b.data.Write(value[:end])
	}
	if len(value) > remaining {
		b.exceeded = true
	}

	return written, nil
}

func (b *cappedBuffer) Bytes() []byte {
	return bytes.Clone(b.data.Bytes())
}

func (b *cappedBuffer) String() string {
	return b.data.String()
}
