package zabbix

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"
)

type ZabbixAdapter struct {
	url      string
	user     string
	password string
	token    string // Cacheamos el token de auth
	client   *http.Client
}

func NewZabbixAdapter(url, user, pass string) *ZabbixAdapter {
	return &ZabbixAdapter{
		url:      url,
		user:     user,
		password: pass,
		client:   &http.Client{Timeout: 5 * time.Second},
	}
}

type zabbixRequest struct {
	Jsonrpc string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
	ID      int         `json:"id"`
	Auth    string      `json:"auth,omitempty"`
}

type zabbixResponse struct {
	Result interface{} `json:"result"`
	Error  interface{} `json:"error"`
}

func (z *ZabbixAdapter) Authenticate() error {
	// Lógica de login para obtener el Auth Token
	// Omito detalles standard de "user.login" para ahorrar espacio,
	// pero aquí debes hacer POST method: "user.login" y guardar el result en z.token
	// ...
	z.token = "auth_token_simulado_por_brevedad_implementar_real"
	return nil
}

func (z *ZabbixAdapter) GetOpticalInfo(oltHost, circuitID string) (string, string, error) {
	if z.token == "" {
		z.Authenticate()
	}

	// 🚧 NECESITO INFO: ¿Cómo identificas el item en Zabbix?
	// Opción A: Buscas por "key_" que contenga el circuitID?
	// Opción B: Buscas por "host" (OLT) y luego filtras items?

	// Ejemplo: Buscamos items específicos de esa OLT que coincidan con la Key del circuito
	params := map[string]interface{}{
		"output": []string{"lastvalue", "key_"},
		"host":   oltHost,
		"search": map[string]string{
			"key_": circuitID, // Asumiendo que la Key del item contiene el ID del circuito
		},
	}

	reqBody := zabbixRequest{
		Jsonrpc: "2.0",
		Method:  "item.get",
		Params:  params,
		ID:      1,
		Auth:    z.token,
	}

	jsonData, _ := json.Marshal(reqBody)
	resp, err := z.client.Post(z.url, "application/json-rpc", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	// Parsear respuesta...
	// Aquí debes iterar sobre los items retornados y extraer RxPower y Status.
	// Depende mucho de cómo se llamen tus items (ej: "pon.rx_power[CIR-100]")

	return "Up", "-18dBm", nil
}
