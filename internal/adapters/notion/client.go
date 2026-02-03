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
type notionProperty struct {
	Type string `json:"type"`
	// Para Title (Description)
	Title []struct {
		PlainText string `json:"plain_text"`
	} `json:"title,omitempty"`
	// Para RichText (</>)
	RichText []struct {
		PlainText string `json:"plain_text"`
	} `json:"rich_text,omitempty"`
	// Para Select (OLT)
	Select *struct {
		Name string `json:"name"`
	} `json:"select,omitempty"`
}

type notionQueryResp struct {
	Results []struct {
		Properties map[string]notionProperty `json:"properties"`
	} `json:"results"`
}

// queryNotion busca en Notion usando un filtro específico
func (n *NotionAdapter) queryNotion(filter map[string]interface{}) (*notionQueryResp, error) {
	url := fmt.Sprintf("https://api.notion.com/v1/databases/%s/query", n.databaseID)
	
	jsonData, _ := json.Marshal(filter)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	req.Header.Set("Authorization", "Bearer "+n.apiKey)
	req.Header.Set("Notion-Version", "2022-06-28")
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("notion api error: %d", resp.StatusCode)
	}

	var result notionQueryResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// GetCredentials: Obtiene las credenciales del circuito
func (n *NotionAdapter) GetNetworkInfo(circuitID string) (string, string, error) {
	// ESTRATEGIA DE BÚSQUEDA EN DOS PASOS:
	// 1. Primero intentamos buscar con el formato específico fx-CID-nombre
	// 2. Si no encontramos, buscamos cualquier campo que contenga el número CID

	// PASO 1: Buscar con formato fx-CID-nombre (o fxCID)
	formats := []string{
		fmt.Sprintf("fx-%s-", circuitID),  // fx-CID-nombre
		fmt.Sprintf("fx%s", circuitID),   // fxCID
		fmt.Sprintf("fx-%s", circuitID),  // fx-CID
	}

	var result *notionQueryResp
	var err error

	// Intentamos cada formato con Title primero
	for _, format := range formats {
		filterBody := map[string]interface{}{
			"filter": map[string]interface{}{
				"property": "Description",
				"title": map[string]string{
					"contains": format,
				},
			},
		}
		
		result, err = n.queryNotion(filterBody)
		if err == nil && result != nil && len(result.Results) > 0 {
			break
		}

		// Si no encontramos con Title, intentamos con RichText
		if err == nil && (result == nil || len(result.Results) == 0) {
			filterBodyRichText := map[string]interface{}{
				"filter": map[string]interface{}{
					"property": "Description",
					"rich_text": map[string]string{
						"contains": format,
					},
				},
			}
			result, err = n.queryNotion(filterBodyRichText)
			if err == nil && result != nil && len(result.Results) > 0 {
				break
			}
		}
	}

	// PASO 2: Si no encontramos con formato específico, buscamos solo el número CID
	if result == nil || len(result.Results) == 0 {
		// Buscar solo el número CID en cualquier parte del campo Description
		filterBody := map[string]interface{}{
			"filter": map[string]interface{}{
				"property": "Description",
				"title": map[string]string{
					"contains": circuitID,
				},
			},
		}
		
		result, err = n.queryNotion(filterBody)
		if err != nil {
			return "", "", err
		}

		// Si no encontramos con Title, intentamos con RichText
		if len(result.Results) == 0 {
			filterBodyRichText := map[string]interface{}{
				"filter": map[string]interface{}{
					"property": "Description",
					"rich_text": map[string]string{
						"contains": circuitID,
					},
				},
			}
			result, err = n.queryNotion(filterBodyRichText)
			if err != nil {
				return "", "", err
			}
		}
	}

	if result == nil || len(result.Results) == 0 {
		return "", "", fmt.Errorf("circuit not found in notion")
	}

	props := result.Results[0].Properties

	// EXTRACCIÓN: Obtenemos OLT y ONT ID (1/2/3) de las columnas de Notion
	// OLT es de tipo "select" según la respuesta real de Notion
	oltProp, ok := props["OLT"]
	if !ok {
		return "", "", fmt.Errorf("propiedad OLT no encontrada en Notion")
	}
	
	var olt string
	if oltProp.Select != nil && oltProp.Select.Name != "" {
		// OLT es un campo select
		olt = oltProp.Select.Name
	} else if len(oltProp.RichText) > 0 {
		// Fallback: OLT como RichText
		olt = oltProp.RichText[0].PlainText
	} else if len(oltProp.Title) > 0 {
		// Fallback: OLT como Title
		olt = oltProp.Title[0].PlainText
	} else {
		return "", "", fmt.Errorf("propiedad OLT vacía en Notion")
	}
	
	// La columna </> tiene nombre vacío "" (no "</>") según la respuesta real
	ontProp, ok := props[""]
	if !ok {
		// Intentamos también con "</>" por si acaso
		ontProp, ok = props["</>"]
		if !ok {
			return "", "", fmt.Errorf("propiedad </> (ONT ID) no encontrada en Notion")
		}
	}
	
	// </> es de tipo rich_text según la respuesta real
	var ont string
	if len(ontProp.RichText) > 0 {
		ont = ontProp.RichText[0].PlainText
	} else if len(ontProp.Title) > 0 {
		// Fallback: </> como Title
		ont = ontProp.Title[0].PlainText
	} else {
		return "", "", fmt.Errorf("propiedad </> (ONT ID) vacía en Notion")
	}

	return olt, ont, nil
}
