// aqui estamos definiendo las entidades y las interfaces
package core

type Circuit struct {
	ID           int
	CID          string // El circuit_id de la DB
	OLT_Hostname string // Vendrá de Notion
	OntID        string // El formato 1/2/3 de Notion

}

// EnrichedData representa los datos enriquecidos de un circuito después del procesamiento
type EnrichedData struct {
	CircuitID     string
	VLAN          string
	PPPoEUsername string
	PPPoEPassword string
	StatusGpon    string
	RxPower       string
	Error         error
}

// Interfaces (Ports)
type CircuitRepository interface {
	FetchPendingCircuits() ([]Circuit, error)
	UpdateCircuitBatch(data []EnrichedData) error
}

type NotionClient interface {
	// Ahora devuelve el Hostname de la OLT y el ONT ID (ej: 1/2/3)
	GetNetworkInfo(circuitID string) (olt, ont string, err error)
}

type ZabbixClient interface {
	// Procesa la lógica de los números del ONT ID
	GetOpticalInfo(oltHost, ontID string) (status, rx string, err error)
}

type UbersmithClient interface {
	// Obtiene los detalles del servicio: credenciales PPPoE
	GetServiceDetails(cid string) (user, pass string, err error)
}
