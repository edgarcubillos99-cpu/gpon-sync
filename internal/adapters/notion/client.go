package notion

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type NotionAdapter struct {
	apiKey     string
	databaseID string
	client     *http.Client
}

func NewNotionAdapter(apiKey, databaseID string) *NotionAdapter {
	return &NotionAdapter{
		apiKey:     apiKey,
		databaseID: databaseID,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

// Estructuras internas para parsear la respuesta compleja de Notion
type notionQueryResp struct {
	Results []struct {
		Properties map[string]struct {
			RichText []struct {
				PlainText string `json:"plain_text"`
			} `json:"rich_text"`
			// Notion devuelve tipos distintos (Title, RichText, Number).
			// He asumido RichText para simplificar.
		} `json:"properties"`
	} `json:"results"`
}

// GetCredentials: Obtiene las credenciales del circuito
func (n *NotionAdapter) GetCredentials(circuitID string) (string, string, string, error) {
	url := fmt.Sprintf("https://api.notion.com/v1/databases/%s/query", n.databaseID)

	// Filtro JSON para buscar por CircuitID
	// 🚧 NECESITO INFO: ¿Cómo se llama exactamente la columna 'CircuitID' en Notion?
	filterBody := map[string]interface{}{
		"filter": map[string]interface{}{
			"property": "Circuit Code", // <--- CAMBIAR ESTO POR EL NOMBRE REAL EN NOTION
			"rich_text": map[string]string{
				"equals": circuitID,
			},
		},
	}

	jsonData, _ := json.Marshal(filterBody)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	req.Header.Set("Authorization", "Bearer "+n.apiKey)
	req.Header.Set("Notion-Version", "2022-06-28")
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", "", "", fmt.Errorf("notion api error: %d", resp.StatusCode)
	}

	var result notionQueryResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", "", err
	}

	if len(result.Results) == 0 {
		return "", "", "", fmt.Errorf("circuit not found in notion")
	}

	props := result.Results[0].Properties

	// 🚧 NECESITO INFO: Nombres exactos de las propiedades (columnas) en Notion
	// Notion devuelve estructuras anidadas complejas. Aquí asumo que son texto simple.
	// Tendrás que ajustar este parseo según tus tipos de datos en Notion.
	vlan := "" // props["VLAN"].RichText[0].PlainText
	user := "" // props["PPPoE User"].RichText[0].PlainText
	pass := "" // props["PPPoE Pass"].RichText[0].PlainText

	return vlan, user, pass, nil
}
