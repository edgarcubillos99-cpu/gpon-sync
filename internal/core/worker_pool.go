// aqui implementamos la logica del worker pool
package core

import (
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

// worker: Procesa un circuito por vez, comenzando con los datos de Notion y luego Zabbix
func (wp *WorkerPool) worker(jobs <-chan Circuit, results chan<- EnrichedData, wg *sync.WaitGroup) {
	defer wg.Done()
	for c := range jobs {
		// 1. Notion: Obtenemos OLT y ONT ID
		olt, ont, _ := wp.notion.GetNetworkInfo(c.CID)

		// 2. Ubersmith: 🆕 Obtenemos VLAN y PPPoE
		vlan, p_user, p_pass, err := wp.ubersmith.GetServiceDetails(c.CID)
		if err != nil {
			// Log error pero continuar para al menos traer datos de Zabbix
		}

		// 3. Zabbix: Potencia y Estado
		status, rx, _ := wp.zabbix.GetOpticalInfo(olt, ont)

		// Enviamos todo enriquecido
		results <- EnrichedData{
			CircuitID:     c.CID,
			VLAN:          vlan,
			PPPoEUsername: p_user,
			PPPoEPassword: p_pass,
			StatusGpon:    status,
			RxPower:       rx,
		}
	}
}
