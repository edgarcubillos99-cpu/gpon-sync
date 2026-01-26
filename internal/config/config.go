package config

import (
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config contiene todas las variables necesarias para la ejecución
type Config struct {
	// Base de Datos (DSN formateado)
	DatabaseURL string

	// Notion
	NotionKey  string
	NotionDBID string

	// Zabbix
	ZabbixURL  string
	ZabbixUser string
	ZabbixPass string

	// Configuración del Worker
	WorkerCount int
}

// Load lee el archivo .env y las variables de entorno del sistema
func Load() *Config {
	// 1. Intentamos cargar el archivo .env (útil para desarrollo local)
	// Si no existe (producción con Docker envs), no pasa nada.
	_ = godotenv.Load()

	// 2. Construcción del DSN de Postgres
	// Es mejor pedir host, user, pass por separado para evitar errores de formato en el string
	dbHost := getEnvRequired("DB_HOST")
	dbPort := getEnv("DB_PORT", "5432")
	dbUser := getEnvRequired("DB_USER")
	dbPass := getEnvRequired("DB_PASS")
	dbName := getEnvRequired("DB_NAME")
	dbSSL := getEnv("DB_SSLMODE", "disable") // disable para local, require para prod

	// Formato: postgres://user:password@host:port/dbname?sslmode=disable
	databaseURL := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s",
		dbUser, dbPass, dbHost, dbPort, dbName, dbSSL,
	)

	// 3. Configuración de Workers
	workersStr := getEnv("WORKER_COUNT", "5")
	workers, err := strconv.Atoi(workersStr)
	if err != nil {
		workers = 5
		log.Printf("Advertencia: WORKER_COUNT inválido, usando default: %d", workers)
	}

	// 4. Retornar Configuración Validada
	return &Config{
		DatabaseURL: databaseURL,
		NotionKey:   getEnvRequired("NOTION_API_KEY"),
		NotionDBID:  getEnvRequired("NOTION_DATABASE_ID"),
		ZabbixURL:   getEnvRequired("ZABBIX_URL"),
		ZabbixUser:  getEnvRequired("ZABBIX_USER"),
		ZabbixPass:  getEnvRequired("ZABBIX_PASS"),
		WorkerCount: workers,
	}
}

// --- Helpers ---

// getEnv obtiene una variable o retorna un valor por defecto
func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

// getEnvRequired obtiene una variable o detiene el programa si no existe (Fail Fast)
func getEnvRequired(key string) string {
	value, exists := os.LookupEnv(key)
	if !exists || value == "" {
		log.Fatalf("[FATAL] La variable de entorno requerida '%s' no está definida.", key)
	}
	return value
}
