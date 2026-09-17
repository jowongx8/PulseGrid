package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

const defaultPort = 8080

type Config struct {
	Port int
}

func Load() (Config, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("load .env: %w", err)
	}

	port, err := loadPort()
	if err != nil {
		return Config{}, err
	}

	return Config{
		Port: port,
	}, nil
}

func loadPort() (int, error) {
	rawPort, ok := os.LookupEnv("PORT")
	if !ok {
		return defaultPort, nil
	}

	port, err := strconv.Atoi(rawPort)
	if err != nil {
		return 0, fmt.Errorf("invalid PORT %q: must be a number between 1 and 65535", rawPort)
	}

	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("invalid PORT %d: must be between 1 and 65535", port)
	}

	return port, nil
}
