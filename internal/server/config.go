package server

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/slipramp/slipramp/internal/objectstore"
	"github.com/slipramp/slipramp/internal/store"
)

type Config struct {
	Address         string
	ShutdownTimeout time.Duration
	Database        store.Config
	ObjectStore     objectstore.Config
}

func DefaultConfig() Config {
	return Config{
		Address:         ":8080",
		ShutdownTimeout: 10 * time.Second,
		Database:        store.DefaultConfig(),
		ObjectStore:     objectstore.DefaultConfig(),
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

	if err := c.Database.Validate(); err != nil {
		return fmt.Errorf("validate database: %w", err)
	}

	if err := c.ObjectStore.Validate(); err != nil {
		return fmt.Errorf("validate object store: %w", err)
	}

	return nil
}
