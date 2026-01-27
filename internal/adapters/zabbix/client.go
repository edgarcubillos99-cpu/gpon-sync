package zabbix

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type ZabbixAdapter struct {
	url      string
	user     string
	password string
	token    string
	client   *http.Client
}

func NewZabbixAdapter(url, user, pass string) *ZabbixAdapter {
	return &ZabbixAdapter{
		url:      url,
		user:     user,
		password: pass,
		client:   &http.Client{Timeout: 10 * time.Second}, // Timeout para Zabbix
	}
}

// Estructuras para Request/Response JSON-RPC 2.0
type zabbixRequest struct {
	Jsonrpc string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
	ID      int         `json:"id"`
	Auth    string      `json:"auth,omitempty"`
}

type zabbixResponse struct {
	Result json.RawMessage `json:"result"` // Usamos RawMessage para diferir el decode
	Error  *zabbixError    `json:"error,omitempty"`
}

type zabbixError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data"`
}

// Estructura para leer el Token de auth
type authResult string

// Estructura para leer los Items
type zabbixItem struct {
	ItemID    string `json:"itemid"`
	Name      string `json:"name"`
	Key       string `json:"key_"`      // Ej: "rx power:1/1"
	LastValue string `json:"lastvalue"` // Ej: "-26.7"
}

// Authenticate: Realiza el login y guarda el token
func (z *ZabbixAdapter) Authenticate() error {
	body := zabbixRequest{
		Jsonrpc: "2.0",
		Method:  "user.login",
		Params: map[string]string{
			"user":     z.user,
			"password": z.password,
		},
		ID: 1,
	}

	respBytes, err := z.doRequest(body)
	if err != nil {
		return err
	}

	// Parseamos el resultado que es un string simple (el token)
	var token string
	if err := json.Unmarshal(respBytes, &token); err != nil {
		return fmt.Errorf("fallo al parsear token: %v", err)
	}

	z.token = token
	return nil
}

// GetOpticalInfo construye la key exacta basada en puerto e indice
func (z *ZabbixAdapter) GetOpticalInfo(oltHost, port, index string) (string, string, error) {
	if z.token == "" {
		if err := z.Authenticate(); err != nil {
			return "", "", fmt.Errorf("auth error: %w", err)
		}
	}

	// 1. CONSTRUCCIÓN DE LA KEY EXACTA
	targetKey := fmt.Sprintf("rx power:%s/%s", port, index)

	params := map[string]interface{}{
		"output": []string{"lastvalue", "key_"},
		"host":   oltHost,
		// Buscamos EXACTAMENTE esa key, es mucho más rápido que usar 'search'
		"filter": map[string]string{
			"key_": targetKey,
		},
	}

	reqBody := zabbixRequest{
		Jsonrpc: "2.0",
		Method:  "item.get",
		Params:  params,
		ID:      2,
		Auth:    z.token,
	}

	resultBytes, err := z.doRequest(reqBody)
	if err != nil {
		return "", "", err
	}

	var items []zabbixItem
	if err := json.Unmarshal(resultBytes, &items); err != nil {
		return "", "", fmt.Errorf("error parseando: %v", err)
	}

	if len(items) == 0 {
		return "Unknown", "N/A", fmt.Errorf("item no encontrado: %s", targetKey)
	}

	// 2. INTERPRETACIÓN DE DATOS (Lógica de Negocio)
	rawValue := items[0].LastValue
	rxPower := rawValue + " dBm"
	status := "Online"

	// Si RxPower es "0", el cliente está caído.
	if rawValue == "0" || rawValue == "0.00" {
		status = "Offline"
		rxPower = "Sin Señal" // O mantener "0 dBm"
	} else {
	}

	return status, rxPower, nil
}

// doRequest: Helper privado para hacer la llamada HTTP y manejar errores de Zabbix
func (z *ZabbixAdapter) doRequest(reqBody zabbixRequest) ([]byte, error) {
	jsonData, _ := json.Marshal(reqBody)
	resp, err := z.client.Post(z.url, "application/json-rpc", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	var zResp zabbixResponse
	if err := json.Unmarshal(bodyBytes, &zResp); err != nil {
		return nil, err
	}

	if zResp.Error != nil {
		return nil, fmt.Errorf("zabbix api error %d: %s", zResp.Error.Code, zResp.Error.Message)
	}

	return zResp.Result, nil
}
