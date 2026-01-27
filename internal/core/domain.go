// aqui estamos definiendo las entidades y las interfaces
package core

type Circuit struct {
	ID           int
	CircuitID    string // Identificador único (ej. "CIR-100")
	OLT_Hostname string // Necesario para buscar en Zabbix
	PonPort      string // Ej: "1" o "2"
	OnuIndex     string // Ej: "51", "52", "1"
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
	GetOpticalInfo(oltHost, port, index string) (status, rx string, err error)
}
