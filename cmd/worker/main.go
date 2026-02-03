package main

import (
	"gpon-sync/internal/adapters/notion"
	"gpon-sync/internal/adapters/postgres"
	"gpon-sync/internal/adapters/ubersmith"
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
	ubersmithClient := ubersmith.NewUbersmithAdapter(cfg.UbersmithURL, cfg.UbersmithUser, cfg.UbersmithPass)

	// Autenticación de Zabbix antes de procesar circuitos
	log.Println("Autenticando con Zabbix...")
	if err := zabbixClient.Authenticate(); err != nil {
		log.Fatalf("Error autenticando con Zabbix: %v", err)
	}
	log.Println("Autenticación con Zabbix exitosa")

	// 3. Core
	pool := core.NewWorkerPool(cfg.WorkerCount, notionClient, zabbixClient, ubersmithClient)

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

	// Contador para seguimiento de pruebas
	processedCount := 0
	successCount := 0
	errorCount := 0

	for res := range resultsCh {
		processedCount++

		// Log detallado para cada instancia (pruebas)
		log.Printf("\n=== INSTANCIA %d: CID=%s ===", processedCount, res.CircuitID)

		if res.Error != nil {
			errorCount++
			log.Printf("[ERROR] CID %s: %v", res.CircuitID, res.Error)
			log.Printf("[DETALLE] VLAN=%s, PPPoEUser=%s, StatusGpon=%s, RxPower=%s",
				res.VLAN, res.PPPoEUsername, res.StatusGpon, res.RxPower)
			// Continuamos para intentar guardar datos parciales si existen
		} else {
			successCount++
			log.Printf("[OK] CID %s procesado exitosamente", res.CircuitID)
			log.Printf("[DETALLE] VLAN=%s, PPPoEUser=%s, StatusGpon=%s, RxPower=%s",
				res.VLAN, res.PPPoEUsername, res.StatusGpon, res.RxPower)
		}

		batch = append(batch, res)

		if len(batch) >= batchSize {
			if cfg.DryRun {
				log.Printf("[DRY-RUN] Se actualizaría batch de %d items (NO se guardó)", len(batch))
				// Mostrar qué se actualizaría
				for _, item := range batch {
					log.Printf("[DRY-RUN]   CID=%s → RxPower=%s, StatusGpon=%s, VLAN=%s, PPPoEUser=%s",
						item.CircuitID, item.RxPower, item.StatusGpon, item.VLAN, item.PPPoEUsername)
				}
			} else {
				if err := dbRepo.UpdateCircuitBatch(batch); err != nil {
					log.Printf("[CRITICAL] Fallo al guardar batch: %v", err)
				} else {
					log.Printf("✅ Batch guardado en DB (%d items)", len(batch))
				}
			}
			batch = nil // Limpiar batch
		}
	}

	// Guardar remanentes
	if len(batch) > 0 {
		if cfg.DryRun {
			log.Printf("[DRY-RUN] Se actualizaría batch final de %d items (NO se guardó)", len(batch))
			// Mostrar qué se actualizaría
			for _, item := range batch {
				log.Printf("[DRY-RUN]   CID=%s → RxPower=%s, StatusGpon=%s, VLAN=%s, PPPoEUser=%s",
					item.CircuitID, item.RxPower, item.StatusGpon, item.VLAN, item.PPPoEUsername)
			}
		} else {
			if err := dbRepo.UpdateCircuitBatch(batch); err != nil {
				log.Printf("[CRITICAL] Fallo al guardar batch final: %v", err)
			} else {
				log.Printf("✅ Batch final guardado en DB (%d items)", len(batch))
			}
		}
	}

	log.Printf("\n=== RESUMEN FINAL ===")
	log.Printf("Total procesados: %d", processedCount)
	log.Printf("Exitosos: %d", successCount)
	log.Printf("Con errores: %d", errorCount)
	log.Println("Trabajo finalizado.")
}
