package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

const (
	defaultPort         = 8080
	defaultDatabasePath = "./data/pulsegrid.db"
)

type Config struct {
	Port         int
	DatabasePath string
}

func Load() (Config, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("load .env: %w", err)
	}

	port, err := loadPort()
	if err != nil {
		return Config{}, err
	}
	databasePath, err := loadDatabasePath()
	if err != nil {
		return Config{}, err
	}

	return Config{
		Port:         port,
		DatabasePath: databasePath,
	}, nil
}

func loadDatabasePath() (string, error) {
	path, ok := os.LookupEnv("DATABASE_PATH")
	if !ok {
		return defaultDatabasePath, nil
	}
	if path == "" {
		return "", errors.New("invalid DATABASE_PATH: must not be empty")
	}
	return path, nil
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
