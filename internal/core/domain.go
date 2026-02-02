// aqui estamos definiendo las entidades y las interfaces
package core

import "time"

type Circuit struct {
	ID        int       `json:"id"`
	CID       string    `json:"cid"`
	Name      string    `json:"name"`
	PlanName  string    `json:"plan_name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Datos de Notion
	OLT           string `json:"olt"`
	VLAN          int    `json:"vlan"`
	PPPoEUsername string `json:"pppoe_username"`
	PPPoEPassword string `json:"pppoe_password"`

	// Datos de Zabbix (campos de entrada)
	OLT_Hostname string `json:"olt_hostname"`
	PonPort      string `json:"pon_port"`
	OnuIndex     string `json:"onu_index"`

	// Datos de Zabbix (resultados)
	StatusGpon string  `json:"status_gpon"`
	RxPower    float64 `json:"rx_power"`
}

// EnrichedData representa los datos enriquecidos de un circuito después del procesamiento
type EnrichedData struct {
	CircuitID  string `json:"circuit_id"`
	VLAN       string `json:"vlan"`
	PPPoEUser  string `json:"pppoe_user"`
	PPPoEPass  string `json:"pppoe_pass"`
	StatusGpon string `json:"status_gpon"`
	RxPower    string `json:"rx_power"`
	Error      error  `json:"-"`
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
