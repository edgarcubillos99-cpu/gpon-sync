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

func (wp *WorkerPool) worker(id int, jobs <-chan Circuit, results chan<- EnrichedData, wg *sync.WaitGroup) {
	defer wg.Done()
	for c := range jobs {
		data := EnrichedData{CircuitID: c.CircuitID}

		// 1. Notion
		vlan, user, pass, err := wp.notion.GetCredentials(c.CircuitID)
		if err != nil {
			data.Error = fmt.Errorf("notion: %w", err)
			results <- data
			continue
		}
		data.VLAN = vlan
		data.PPPoEUser = user
		data.PPPoEPass = pass

		// 2. Zabbix
		status, rx, err := wp.zabbix.GetOpticalInfo(c.OLT_Hostname, c.CircuitID)
		if err != nil {
			// Nota: A veces quieres guardar los datos de Notion aunque falle Zabbix.
			// Si es así, elimina el 'continue' y maneja el error parcial.
			data.Error = fmt.Errorf("zabbix: %w", err)
			results <- data
			continue
		}
		data.StatusGpon = status
		data.RxPower = rx

		results <- data
	}
}
