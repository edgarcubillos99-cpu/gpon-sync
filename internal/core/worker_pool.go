// aqui implementamos la logica del worker pool
package core

import (
	"fmt"
	"sync"
)

type WorkerPool struct {
	workerCount int
	notion      NotionClient
	zabbix      ZabbixClient
}

func NewWorkerPool(count int, n NotionClient, z ZabbixClient) *WorkerPool {
	return &WorkerPool{
		workerCount: count,
		notion:      n,
		zabbix:      z,
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
		go wp.worker(i, jobs, results, &wg)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	return results
}

// worker: Procesa un circuito por vez, comenzando con los datos de Notion y luego Zabbix
func (wp *WorkerPool) worker(id int, jobs <-chan Circuit, results chan<- EnrichedData, wg *sync.WaitGroup) {
	defer wg.Done()
	for c := range jobs {
		data := EnrichedData{CircuitID: c.CID}

		// 1. Notion
		vlan, user, pass, err := wp.notion.GetCredentials(c.CID)
		if err != nil {
			data.Error = fmt.Errorf("notion: %w", err)
			results <- data
			continue
		}
		data.VLAN = vlan
		data.PPPoEUser = user
		data.PPPoEPass = pass

		// 2. Zabbix
		status, rx, err := wp.zabbix.GetOpticalInfo(c.OLT_Hostname, c.PonPort, c.OnuIndex)

		if err != nil {
			data.Error = fmt.Errorf("zabbix err en %s/%s: %v", c.PonPort, c.OnuIndex, err)
			results <- data
			continue
		}

		data.StatusGpon = status
		data.RxPower = rx
		results <- data
	}
}
