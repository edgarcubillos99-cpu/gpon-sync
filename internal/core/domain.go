package core

type Circuit struct {
	ID           int
	CircuitID    string // Identificador único (ej. "CIR-100")
	OLT_Hostname string // Necesario para buscar en Zabbix
	NotionPageID string // Si ya lo tienes, ayuda. Si no, buscaremos por query.
}

type EnrichedData struct {
	CircuitID  string
	VLAN       string
	PPPoEUser  string
	PPPoEPass  string
	StatusGpon string
	RxPower    string
	Error      error
}

// Interfaces (Ports)
type CircuitRepository interface {
	FetchPendingCircuits() ([]Circuit, error)
	UpdateCircuitBatch(data []EnrichedData) error
}

type NotionClient interface {
	GetCredentials(circuitID string) (vlan, user, pass string, err error)
}

type ZabbixClient interface {
	GetOpticalInfo(oltHost, circuitID string) (status, rx string, err error)
}
