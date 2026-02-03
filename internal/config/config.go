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

	// Ubersmith
	UbersmithURL  string
	UbersmithUser string
	UbersmithPass string

	// Configuración del Worker
	WorkerCount int

	// Modo de Prueba (Dry-Run): Si es true, no actualiza la base de datos
	DryRun bool
}

// Load lee el archivo .env y las variables de entorno del sistema
func Load() *Config {
	// 1. Intentamos cargar el archivo .env
	// Si no existe (producción con Docker envs), no pasa nada.
	_ = godotenv.Load()

	// 2. Construcción del DSN de MySQL
	// Es mejor pedir host, user, pass por separado para evitar errores de formato en el string
	dbHost := getEnvRequired("DB_HOST")
	dbPort := getEnv("DB_PORT", "3306") // Puerto por defecto de MySQL
	dbUser := getEnvRequired("DB_USER")
	dbPass := getEnvRequired("DB_PASS")
	dbName := getEnvRequired("DB_NAME")

	// Parámetros adicionales de MySQL (parseTime=true para manejar fechas correctamente)
	dbParams := getEnv("DB_PARAMS", "parseTime=true&charset=utf8mb4")

	// Formato MySQL: user:password@tcp(host:port)/dbname?params
	databaseURL := fmt.Sprintf(
		"%s:%s@tcp(%s:%s)/%s?%s",
		dbUser, dbPass, dbHost, dbPort, dbName, dbParams,
	)

	// 3. Configuración de Workers
	workersStr := getEnv("WORKER_COUNT", "5")
	workers, err := strconv.Atoi(workersStr)
	if err != nil {
		workers = 5
		log.Printf("Advertencia: WORKER_COUNT inválido, usando default: %d", workers)
	}

	// 4. Modo Dry-Run (Prueba sin modificar DB)
	dryRunStr := getEnv("DRY_RUN", "false")
	dryRun := dryRunStr == "true" || dryRunStr == "1" || dryRunStr == "yes"
	if dryRun {
		log.Println("⚠️  MODO PRUEBA ACTIVADO (DRY_RUN=true) - NO se actualizará la base de datos")
	}

	// 5. Retornar Configuración Validada
	return &Config{
		DatabaseURL:   databaseURL,
		NotionKey:     getEnvRequired("NOTION_API_KEY"),
		NotionDBID:    getEnvRequired("NOTION_DATABASE_ID"),
		ZabbixURL:     getEnvRequired("ZABBIX_URL"),
		ZabbixUser:    getEnvRequired("ZABBIX_USER"),
		ZabbixPass:    getEnvRequired("ZABBIX_PASS"),
		UbersmithURL:  getEnvRequired("UBERSMITH_URL"),
		UbersmithUser: getEnvRequired("UBERSMITH_USER"),
		UbersmithPass: getEnvRequired("UBERSMITH_PASS"),
		WorkerCount:   workers,
		DryRun:        dryRun,
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
