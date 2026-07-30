package config

import "os"

// ИСПРАВЛЕНО: раньше здесь дублировались Rabbit-поля (RabbitHost, RabbitPort...)
// с другими именами, чем в messaging.Config у коллеги (Host, Port...).
// Два источника правды для одного и того же конфига — плохо, оставляем
// Rabbit-конфиг только в messaging.Config, здесь — только то, что
// специфично для БД.

type Config struct {
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func Load() Config {
	return Config{
		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     getEnv("DB_PORT", "5432"),
		DBUser:     getEnv("DB_USER", "ridehail_user"),
		DBPassword: getEnv("DB_PASSWORD", "ridehail_pass"),
		DBName:     getEnv("DB_NAME", "ridehail_db"),
	}
}

func (c Config) PostgresDSN() string {
	return "postgres://" + c.DBUser + ":" + c.DBPassword + "@" + c.DBHost + ":" + c.DBPort + "/" + c.DBName
}
