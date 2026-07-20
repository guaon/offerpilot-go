package env

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

func findEnvFile(dir string) string {
	for {
		parentDir := filepath.Dir(dir)
		if parentDir == dir {
			return ""
		}
		envPath := filepath.Join(parentDir, ".env")
		if _, err := os.Stat(envPath); err == nil {
			return envPath
		}
		dir = parentDir
	}
}

func LoadEnvFile(path ...string) {
	var envPath string
	if len(path) > 0 && path[0] != "" {
		envPath = path[0]
	} else {
		if pwd := os.Getenv("PWD"); pwd != "" {
			envPath = filepath.Join(pwd, ".env")
			if _, err := os.Stat(envPath); os.IsNotExist(err) {
				envPath = findEnvFile(pwd)
			}
		} else if wd, err := os.Getwd(); err == nil {
			envPath = filepath.Join(wd, ".env")
			if _, err := os.Stat(envPath); os.IsNotExist(err) {
				envPath = findEnvFile(wd)
			}
		} else {
			return
		}
	}

	if envPath == "" {
		return
	}

	file, err := os.Open(envPath)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		equalsIndex := strings.Index(line, "=")
		if equalsIndex <= 0 {
			continue
		}

		key := strings.TrimSpace(line[:equalsIndex])
		value := strings.TrimSpace(line[equalsIndex+1:])

		if strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
			value = value[1 : len(value)-1]
		} else if strings.HasPrefix(value, "`") && strings.HasSuffix(value, "`") {
			value = value[1 : len(value)-1]
		}
		if os.Getenv(key) == "" {
			os.Setenv(key, value)
		}
	}
}
