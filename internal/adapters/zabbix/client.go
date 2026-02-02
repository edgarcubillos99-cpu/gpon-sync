package zabbix

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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
func (z *ZabbixAdapter) GetOpticalInfo(oltHost, ontID string) (string, string, error) {
	// 1. LÓGICA DE PARSEO: 1/2/3 -> [1, 2, 3]
	parts := strings.Split(ontID, "/")
	if len(parts) < 3 {
		return "", "", fmt.Errorf("formato ONT ID inválido: %s", ontID)
	}

	segundo := parts[1] // El "2" para el status
	tercero := parts[2] // El "3" para la potencia

	// 🚧 KEYS SEGÚN TU REQUERIMIENTO:
	powerKey := fmt.Sprintf("rx power:%s/%s", segundo, tercero)
	statusKey := fmt.Sprintf("status_gpon_%s", segundo)

	params := map[string]interface{}{
		"output": []string{"lastvalue", "key_"},
		"host":   oltHost,
		"filter": map[string]interface{}{
			"key_": []string{powerKey, statusKey},
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
	json.Unmarshal(resultBytes, &items)

	var rx, status string
	for _, item := range items {
		if item.Key == powerKey {
			rx = item.LastValue + " dBm"
		}
		if item.Key == statusKey {
			status = item.LastValue // Ej: "1" o "Up"
		}
	}

	return status, rx, nil
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
