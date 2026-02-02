package main

import (
	"gpon-sync/internal/adapters/notion"
	"gpon-sync/internal/adapters/postgres"
	"gpon-sync/internal/adapters/zabbix"
	"gpon-sync/internal/config"
	"gpon-sync/internal/core"
	"log"
)

func main() {
	// 1. Configuración
	cfg := config.Load() // Asumimos que carga de .env

	// 2. Adaptadores
	dbRepo, err := postgres.NewPostgresRepo(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Fallo DB: %v", err)
	}

	notionClient := notion.NewNotionAdapter(cfg.NotionKey, cfg.NotionDBID)
	zabbixClient := zabbix.NewZabbixAdapter(cfg.ZabbixURL, cfg.ZabbixUser, cfg.ZabbixPass)

	// 3. Core
	pool := core.NewWorkerPool(cfg.WorkerCount, notionClient, zabbixClient)

	// 4. Ejecución
	log.Println("Obteniendo circuitos...")
	circuits, err := dbRepo.FetchPendingCircuits()
	if err != nil {
		log.Fatalf("Error obteniendo circuitos: %v", err)
	}

	log.Printf("Procesando %d circuitos...", len(circuits))
	resultsCh := pool.Run(circuits)

	// 5. Acumulador para Batch Update
	var batch []core.EnrichedData
	batchSize := 100

	for res := range resultsCh {
		if res.Error != nil {
			log.Printf("[ERR] %s: %v", res.CircuitID, res.Error)
			continue
		}

		batch = append(batch, res)

		if len(batch) >= batchSize {
			if err := dbRepo.UpdateCircuitBatch(batch); err != nil {
				log.Printf("[CRITICAL] Fallo al guardar batch: %v", err)
			} else {
				log.Printf("Batch guardado (%d items)", len(batch))
			}
			batch = nil // Limpiar batch
		}
	}

	// Guardar remanentes
	if len(batch) > 0 {
		if err := dbRepo.UpdateCircuitBatch(batch); err != nil {
			log.Printf("[CRITICAL] Fallo al guardar batch final: %v", err)
		} else {
			log.Printf("Batch final guardado (%d items)", len(batch))
		}
	}

	log.Println("Trabajo finalizado.")
}
