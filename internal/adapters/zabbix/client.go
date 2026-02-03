package zabbix

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
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
	Key       string `json:"key_"`      // Ej: "rx power:1/1" - Nota: Zabbix usa "key_" en el JSON
	LastValue string `json:"lastvalue"` // Ej: "-26.7"
}

// Authenticate: Realiza el login y guarda el token
func (z *ZabbixAdapter) Authenticate() error {
	// Según la documentación de Zabbix API, los parámetros pueden ser "user" o "username"
	// Probamos con "username" que es más común en versiones recientes
	body := zabbixRequest{
		Jsonrpc: "2.0",
		Method:  "user.login",
		Params: map[string]interface{}{
			"username": z.user,
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

	segundo := parts[1] // El "2" para el status (segundo número)
	tercero := parts[2] // El "3" para la potencia (tercer número)

	// Keys según requerimiento: rx power:2/3 y gpon_2_status
	// Ejemplo: </>=1/2/3 entonces rx power:2/3 y gpon_2_status
	// Formato exacto en Zabbix: "rx power:1/1", "rx power:1/2", etc.
	powerKey := fmt.Sprintf("rx power:%s/%s", segundo, tercero)
	statusKey := fmt.Sprintf("gpon_%s_status", segundo)

	// Buscamos ambas keys directamente por nombre exacto
	// Hacemos dos consultas separadas porque el filtro con array puede no funcionar correctamente
	// Primero el status
	paramsStatus := map[string]interface{}{
		"output": []string{"lastvalue", "key_"},
		"host":   oltHost,
		"filter": map[string]interface{}{
			"key_": statusKey,
		},
	}

	reqBodyStatus := zabbixRequest{
		Jsonrpc: "2.0",
		Method:  "item.get",
		Params:  paramsStatus,
		ID:      2,
		Auth:    z.token,
	}

	resultBytesStatus, err := z.doRequest(reqBodyStatus)
	if err != nil {
		return "", "", err
	}

	var statusItems []zabbixItem
	if err := json.Unmarshal(resultBytesStatus, &statusItems); err != nil {
		return "", "", fmt.Errorf("error parseando status items: %v", err)
	}

	var status string
	for _, item := range statusItems {
		if item.Key == statusKey {
			status = item.LastValue
			break
		}
	}

	// Ahora buscamos el RxPower
	// Obtenemos todas las keys del host y buscamos la key exacta en memoria
	paramsPower := map[string]interface{}{
		"output": []string{"lastvalue", "key_"},
		"host":   oltHost,
	}

	reqBodyPower := zabbixRequest{
		Jsonrpc: "2.0",
		Method:  "item.get",
		Params:  paramsPower,
		ID:      3,
		Auth:    z.token,
	}

	resultBytesPower, err := z.doRequest(reqBodyPower)
	var rx string
	if err == nil {
		var allItems []zabbixItem
		if err := json.Unmarshal(resultBytesPower, &allItems); err == nil {
			// Buscar la key exacta
			for _, item := range allItems {
				if item.Key == powerKey {
					rx = item.LastValue
					fmt.Printf("[DEBUG Zabbix] ✅ Encontrada key de RxPower: '%s' = '%s'\n", item.Key, item.LastValue)
					// Solo agregar " dBm" si hay un valor y no es "0"
					if rx != "" && rx != "0" {
						rx = rx + " dBm"
					} else if rx == "0" {
						rx = "" // Si es 0, probablemente no hay señal activa
					}
					break
				}
			}

			// Si no encontramos la key exacta, buscamos ms_item_ont_rx_power_7m y parseamos el JSON
			if rx == "" {
				for _, item := range allItems {
					if strings.Contains(strings.ToLower(item.Key), "ms_item_ont_rx_power") {
						// El valor es un JSON array con objetos que tienen "interface" y valores numéricos
						// Ejemplo: [{"interface":"1/6","...":"-20.4"}, ...]
						var powerData []map[string]interface{}
						if err := json.Unmarshal([]byte(item.LastValue), &powerData); err == nil {
							// Buscar el objeto que tenga interface igual a nuestro patrón (segundo/tercero)
							ontPattern := fmt.Sprintf("%s/%s", segundo, tercero)
							for _, entry := range powerData {
								if iface, ok := entry["interface"].(string); ok && iface == ontPattern {
									// Buscar el valor numérico (puede estar en diferentes campos)
									for key, val := range entry {
										if key != "interface" && key != "onustatus" && key != "indice" && key != "contador" {
											if valStr, ok := val.(string); ok {
												// Intentar convertir a número para verificar que es un valor válido
												if valFloat, err := strconv.ParseFloat(valStr, 64); err == nil {
													// Si el valor es 0, probablemente no hay señal
													if valFloat != 0 {
														// Los valores vienen en centésimas (ej: -158 = -15.8 dBm)
														// Dividimos por 10 para obtener el valor real
														rx = fmt.Sprintf("%.1f", valFloat/10.0)
														fmt.Printf("[DEBUG Zabbix] ✅ Encontrado RxPower en JSON: interface='%s' = '%s' dBm\n", ontPattern, rx)
														rx = rx + " dBm"
														break
													}
												}
											}
										}
									}
									if rx != "" {
										break
									}
								}
							}
						}
						if rx != "" {
							break
						}
					}
				}
			}
		} else {
			fmt.Printf("[DEBUG Zabbix] Error parseando respuesta de RxPower: %v\n", err)
		}
	} else {
		fmt.Printf("[DEBUG Zabbix] Error en consulta de RxPower: %v\n", err)
	}

	return status, rx, nil
}

// doRequest: Helper privado para hacer la llamada HTTP y manejar errores de Zabbix
func (z *ZabbixAdapter) doRequest(reqBody zabbixRequest) ([]byte, error) {
	jsonData, _ := json.Marshal(reqBody)
	req, err := http.NewRequest("POST", z.url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := z.client.Do(req)
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
