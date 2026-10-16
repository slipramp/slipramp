package server

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Address         string
	ShutdownTimeout time.Duration
}

func DefaultConfig() Config {
	return Config{
		Address:         ":8080",
		ShutdownTimeout: 10 * time.Second,
	}
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Address) == "" {
		return fmt.Errorf("address must not be empty")
	}

	_, port, err := net.SplitHostPort(c.Address)
	if err != nil {
		return fmt.Errorf("validate address: %w", err)
	}

	portNumber, err := strconv.ParseUint(port, 10, 16)
	if err != nil || portNumber == 0 {
		return fmt.Errorf(
			"address must contain a numeric port between 1 and 65535",
		)
	}

	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("shutdown timeout must be positive")
	}

	return nil
}
