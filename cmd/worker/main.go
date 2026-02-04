package main

import (
	"context"
	"gpon-sync/internal/adapters/notion"
	"gpon-sync/internal/adapters/postgres"
	"gpon-sync/internal/adapters/ubersmith"
	"gpon-sync/internal/adapters/zabbix"
	"gpon-sync/internal/config"
	"gpon-sync/internal/core"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	// 1. Configuración
	cfg := config.Load()

	// 2. Adaptadores
	dbRepo, err := postgres.NewPostgresRepo(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Fallo DB: %v", err)
	}

	notionClient := notion.NewNotionAdapter(cfg.NotionKey, cfg.NotionDBID)
	zabbixClient := zabbix.NewZabbixAdapter(cfg.ZabbixURL, cfg.ZabbixUser, cfg.ZabbixPass)
	ubersmithClient := ubersmith.NewUbersmithAdapter(cfg.UbersmithURL, cfg.UbersmithUser, cfg.UbersmithPass)

	// 3. Core
	pool := core.NewWorkerPool(cfg.WorkerCount, notionClient, zabbixClient, ubersmithClient)

	// 4. Configurar canal para señales de interrupción
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// 5. Configurar ticker para ejecución cada 10 minutos
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	// Contexto para controlar la ejecución
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Función para ejecutar el proceso
	runProcess := func() {
		log.Println("\n" + strings.Repeat("=", 60))
		log.Println("🚀 Iniciando proceso de sincronización...")
		log.Println(strings.Repeat("=", 60))

		// Autenticación de Zabbix (reautenticar cada vez por si expira el token)
		log.Println("Autenticando con Zabbix...")
		if err := zabbixClient.Authenticate(); err != nil {
			log.Printf("[ERROR] Error autenticando con Zabbix: %v", err)
			return
		}
		log.Println("✅ Autenticación con Zabbix exitosa")

		// Obtener circuitos
		log.Println("Obteniendo circuitos...")
		circuits, err := dbRepo.FetchPendingCircuits()
		if err != nil {
			log.Printf("[ERROR] Error obteniendo circuitos: %v", err)
			return
		}

		if len(circuits) == 0 {
			log.Println("⚠️  No hay circuitos pendientes para procesar")
			return
		}

		log.Printf("Procesando %d circuitos...", len(circuits))
		resultsCh := pool.Run(circuits)

		// Acumulador para Batch Update
		var batch []core.EnrichedData
		batchSize := 100

		// Contador para seguimiento
		processedCount := 0
		successCount := 0
		errorCount := 0

		for res := range resultsCh {
			processedCount++

			// Log detallado para cada instancia
			log.Printf("\n=== INSTANCIA %d: CID=%s ===", processedCount, res.CircuitID)

			if res.Error != nil {
				errorCount++
				log.Printf("[ERROR] CID %s: %v", res.CircuitID, res.Error)
				log.Printf("[DETALLE] PPPoEUser=%s, StatusGpon=%s, RxPower=%s",
					res.PPPoEUsername, res.StatusGpon, res.RxPower)
			} else {
				successCount++
				log.Printf("[OK] CID %s procesado exitosamente", res.CircuitID)
				log.Printf("[DETALLE] PPPoEUser=%s, StatusGpon=%s, RxPower=%s",
					res.PPPoEUsername, res.StatusGpon, res.RxPower)
			}

			batch = append(batch, res)

			if len(batch) >= batchSize {
				if cfg.DryRun {
					log.Printf("[DRY-RUN] Se actualizaría batch de %d items (NO se guardó)", len(batch))
					for _, item := range batch {
						log.Printf("[DRY-RUN]   CID=%s → RxPower=%s, StatusGpon=%s, PPPoEUser=%s",
							item.CircuitID, item.RxPower, item.StatusGpon, item.PPPoEUsername)
					}
				} else {
					if err := dbRepo.UpdateCircuitBatch(batch); err != nil {
						log.Printf("[CRITICAL] Fallo al guardar batch: %v", err)
					} else {
						log.Printf("✅ Batch guardado en DB (%d items)", len(batch))
					}
				}
				batch = nil
			}
		}

		// Guardar remanentes
		if len(batch) > 0 {
			if cfg.DryRun {
				log.Printf("[DRY-RUN] Se actualizaría batch final de %d items (NO se guardó)", len(batch))
				for _, item := range batch {
					log.Printf("[DRY-RUN]   CID=%s → RxPower=%s, StatusGpon=%s, PPPoEUser=%s",
						item.CircuitID, item.RxPower, item.StatusGpon, item.PPPoEUsername)
				}
			} else {
				if err := dbRepo.UpdateCircuitBatch(batch); err != nil {
					log.Printf("[CRITICAL] Fallo al guardar batch final: %v", err)
				} else {
					log.Printf("✅ Batch final guardado en DB (%d items)", len(batch))
				}
			}
		}

		log.Printf("\n=== RESUMEN ===")
		log.Printf("Total procesados: %d", processedCount)
		log.Printf("Exitosos: %d", successCount)
		log.Printf("Con errores: %d", errorCount)
		log.Println("✅ Proceso completado")
		log.Printf("⏰ Próxima ejecución en 10 minutos...\n")
	}

	// Ejecutar inmediatamente al inicio
	log.Println("🎯 Iniciando worker de sincronización GPON")
	log.Println("📅 Ejecución automática cada 10 minutos")
	log.Printf("⏰ Primera ejecución inmediata, luego cada 10 minutos\n")
	runProcess()

	// Loop principal: ejecutar cada 10 minutos
	for {
		select {
		case <-ticker.C:
			runProcess()
		case <-sigChan:
			log.Println("\n🛑 Señal de interrupción recibida. Cerrando gracefully...")
			cancel()
			log.Println("✅ Worker detenido correctamente")
			return
		case <-ctx.Done():
			log.Println("✅ Contexto cancelado. Cerrando...")
			return
		}
	}
}
