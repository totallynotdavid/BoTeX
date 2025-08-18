package util

import (
	"os"
	"strconv"
	"strings"
	"time"
)

func GetEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}

	return defaultValue
}

func GetEnvInt(key string, defaultValue int) int {
	str := GetEnv(key, "")
	if str == "" {
		return defaultValue
	}

	value, err := strconv.Atoi(str)
	if err != nil {
		return defaultValue
	}

	return value
}

func GetEnvInt64(key string, defaultValue int64) int64 {
	str := GetEnv(key, "")
	if str == "" {
		return defaultValue
	}

	value, err := strconv.ParseInt(str, 10, 64)
	if err != nil {
		return defaultValue
	}

	return value
}

func GetEnvDuration(key string, defaultValue time.Duration) time.Duration {
	str := GetEnv(key, "")
	if str == "" {
		return defaultValue
	}

	// try to parse duration string first (e.g., "5m", "1h30m")
	duration, err := time.ParseDuration(str)
	if err == nil {
		return duration
	}

	// fall back to parsing as milliseconds
	ms, err := strconv.ParseInt(str, 10, 64)
	if err == nil {
		return time.Duration(ms) * time.Millisecond
	}

	return defaultValue
}

func GetEnvBool(key string, defaultValue bool) bool {
	str := GetEnv(key, "")
	if str == "" {
		return defaultValue
	}

	return ParseBool(str)
}

func ParseBool(value string) bool {
	return strings.EqualFold(value, "true") || value == "1"
}
