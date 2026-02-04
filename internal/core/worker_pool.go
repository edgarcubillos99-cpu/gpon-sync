// aqui implementamos la logica del worker pool
package core

import (
	"fmt"
	"log"
	"sync"
)

type WorkerPool struct {
	workerCount int
	notion      NotionClient
	zabbix      ZabbixClient
	ubersmith   UbersmithClient
}

func NewWorkerPool(count int, n NotionClient, z ZabbixClient, u UbersmithClient) *WorkerPool {
	return &WorkerPool{
		workerCount: count,
		notion:      n,
		zabbix:      z,
		ubersmith:   u,
	}
}

func (wp *WorkerPool) Run(circuits []Circuit) <-chan EnrichedData {
	jobs := make(chan Circuit, len(circuits))
	results := make(chan EnrichedData, len(circuits))

	for _, c := range circuits {
		jobs <- c
	}
	close(jobs)

	var wg sync.WaitGroup
	for i := 0; i < wp.workerCount; i++ {
		wg.Add(1)
		go wp.worker(jobs, results, &wg)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	return results
}

// worker: Procesa un circuito por vez, siguiendo el flujo de trabajo requerido
func (wp *WorkerPool) worker(jobs <-chan Circuit, results chan<- EnrichedData, wg *sync.WaitGroup) {
	defer wg.Done()
	for c := range jobs {
		enriched := EnrichedData{
			CircuitID: c.CID,
		}

		// 1. Notion: Obtenemos OLT y ONT ID usando CID en formato fx-CID-nombre
		olt, ont, err := wp.notion.GetNetworkInfo(c.CID)
		if err != nil {
			log.Printf("[ERROR] CID %s - Notion: %v", c.CID, err)
			enriched.Error = fmt.Errorf("notion error: %w", err)
			results <- enriched
			continue
		}

		// 2. Ubersmith: Obtenemos PPPoEUsername y PPPoEPassword usando CID
		p_user, p_pass, err := wp.ubersmith.GetServiceDetails(c.CID)
		if err != nil {
			log.Printf("[WARN] CID %s - Ubersmith: %v (continuando...)", c.CID, err)
			// Continuamos aunque falle Ubersmith para obtener al menos datos de Zabbix
		} else {
			enriched.PPPoEUsername = p_user
			enriched.PPPoEPassword = p_pass
		}

		// 3. Zabbix: Consultamos rx power y status gpon usando OLT y ONT
		// El formato ONT (1/2/3) se procesa dentro de GetOpticalInfo
		status, rx, err := wp.zabbix.GetOpticalInfo(olt, ont)
		if err != nil {
			log.Printf("[ERROR] CID %s - Zabbix (OLT:%s, ONT:%s): %v", c.CID, olt, ont, err)
			if enriched.Error == nil {
				enriched.Error = fmt.Errorf("zabbix error: %w", err)
			} else {
				enriched.Error = fmt.Errorf("%v; zabbix error: %w", enriched.Error, err)
			}
		} else {
			enriched.StatusGpon = status
			enriched.RxPower = rx
		}

		// Enviamos datos enriquecidos (pueden tener errores parciales)
		results <- enriched
	}
}
