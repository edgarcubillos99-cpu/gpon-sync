package config

import "os"

type Config struct {
	WorkerCount int
	ZabbixURL   string
	// ...
}

func Load() *Config {
	return &Config{
		ZabbixURL: os.Getenv("ZABBIX_URL"),
		// ...
	}
}
