// env.go: the bridge keeps its configuration in a dotenv file, and this process has to read the
// same one rather than grow a second copy of the credentials.
package main

import (
	"os"
	"strings"
)

// loadEnv is the smallest dotenv reader that works on the bridge's file: KEY=VALUE lines, optional
// quotes, `#` comments, no expansion. Values already in the environment win, so the launcher can
// override without editing the file.
func loadEnv(path string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(body), "\n") {
		key, value, ok := parseEnvLine(line)
		if !ok {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, value)
	}
	return nil
}

func parseEnvLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	at := strings.Index(line, "=")
	if at <= 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:at])
	value = strings.TrimSpace(line[at+1:])
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
	}
	return key, value, key != ""
}
