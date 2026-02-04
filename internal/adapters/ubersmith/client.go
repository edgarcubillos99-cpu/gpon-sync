package ubersmith

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

type UbersmithAdapter struct {
	baseURL string
	user    string
	pass    string
}

func NewUbersmithAdapter(baseURL, user, pass string) *UbersmithAdapter {
	return &UbersmithAdapter{
		baseURL: baseURL,
		user:    user,
		pass:    pass,
	}
}

// GetServiceDetails busca VLAN y PPPoE por CID (Service ID en Ubersmith)
// Implementa múltiples estrategias para encontrar el VLAN:
// 1. Custom fields (pack meta_type)
// 2. Campos directos del servicio (vlan, vlan_id, etc.)
// 3. Comments del servicio
// 4. Descripción/notas del servicio
// 5. Expresiones regulares en texto libre
func (u *UbersmithAdapter) GetServiceDetails(cid string) (vlan, user, pass string, err error) {
	// ESTRATEGIA 1: Custom Fields (pack meta_type)
	vlan, user, pass, _ = u.getServiceCustomFields(cid)

	// ESTRATEGIA 2: Obtener datos completos del servicio para buscar en múltiples lugares
	serviceData, err := u.getServiceData(cid)
	if err != nil {
		// Si falla pero tenemos datos de custom fields, los retornamos
		if vlan != "" || user != "" || pass != "" {
			return vlan, user, pass, nil
		}
		return "", "", "", err
	}

	// Buscar VLAN en campos directos del servicio
	if vlan == "" {
		vlan = u.extractVLANFromServiceFields(serviceData)
	}

	// Buscar username y password en campos directos
	if user == "" {
		if usernameVal, ok := serviceData["username"].(string); ok && usernameVal != "" {
			user = usernameVal
		}
	}
	if pass == "" {
		if passwordVal, ok := serviceData["password"].(string); ok && passwordVal != "" {
			pass = passwordVal
		}
	}

	// ESTRATEGIA 3: Buscar VLAN en comments si aún no lo encontramos
	if vlan == "" {
		vlan = u.getVLANFromComments(cid)
	}

	// ESTRATEGIA 4: Buscar VLAN en descripción/notas usando expresiones regulares
	if vlan == "" {
		vlan = u.extractVLANFromText(serviceData)
	}

	// ESTRATEGIA 5: Intentar custom fields con otros meta_types (service, client)
	if vlan == "" {
		vlan = u.getVLANFromOtherMetaTypes(cid)
	}

	return vlan, user, pass, nil
}

// getServiceData obtiene los datos completos del servicio usando client.service_get
func (u *UbersmithAdapter) getServiceData(serviceID string) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s?method=client.service_get&service_id=%s", u.baseURL, serviceID)
	req, _ := http.NewRequest("GET", url, nil)
	req.SetBasicAuth(u.user, u.pass)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return nil, err
	}

	if status, ok := result["status"].(bool); !ok || !status {
		return nil, fmt.Errorf("respuesta de Ubersmith indica error")
	}

	if data, ok := result["data"].(map[string]interface{}); ok {
		return data, nil
	}

	return nil, fmt.Errorf("no se encontraron datos en la respuesta")
}

// extractVLANFromServiceFields busca VLAN en campos directos del servicio
func (u *UbersmithAdapter) extractVLANFromServiceFields(serviceData map[string]interface{}) string {
	// Lista de posibles nombres de campos donde puede estar el VLAN
	// Priorizar "vlan_id" que es el más común en Ubersmith
	vlanFieldNames := []string{
		"vlan_id", "vlanid", "vlan", "vlan_number", "vlan_num",
		"vlan_tag", "vlantag", "vlan_tag_id",
	}

	for _, fieldName := range vlanFieldNames {
		if val, ok := serviceData[fieldName]; ok {
			if valStr, ok := val.(string); ok && valStr != "" {
				// Extraer número de formato "623 [Untagged]" o "623 [Tagged]"
				cleaned := u.extractVLANNumberFromText(valStr)
				if cleaned != "" {
					return cleaned
				}
				// Si no tiene formato con corchetes, retornar el valor limpio
				return strings.TrimSpace(valStr)
			}
			// También intentar como número
			if valNum, ok := val.(float64); ok {
				return fmt.Sprintf("%.0f", valNum)
			}
			if valNum, ok := val.(int); ok {
				return strconv.Itoa(valNum)
			}
		}
	}

	return ""
}

// extractVLANNumberFromText extrae el número de VLAN de texto que puede contener "[Untagged]" o "[Tagged]"
// Ejemplos: "623 [Untagged]" -> "623", "100 [Tagged]" -> "100"
func (u *UbersmithAdapter) extractVLANNumberFromText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	// Patrón para "623 [Untagged]" o "623 [Tagged]" - Formato más común en Ubersmith
	pattern := regexp.MustCompile(`(\d+)\s*\[(?:Untagged|Tagged)\]`)
	matches := pattern.FindStringSubmatch(text)
	if len(matches) > 1 {
		vlanNum, err := strconv.Atoi(matches[1])
		if err == nil && vlanNum >= 1 && vlanNum <= 4094 {
			return matches[1]
		}
	}

	// Si no tiene corchetes, intentar extraer solo el número al inicio
	pattern2 := regexp.MustCompile(`^(\d+)\s*`)
	matches2 := pattern2.FindStringSubmatch(text)
	if len(matches2) > 1 {
		vlanNum, err := strconv.Atoi(matches2[1])
		if err == nil && vlanNum >= 1 && vlanNum <= 4094 {
			return matches2[1]
		}
	}

	return ""
}

// getVLANFromComments obtiene el VLAN desde los comments del servicio
func (u *UbersmithAdapter) getVLANFromComments(serviceID string) string {
	// Ubersmith API: client.service_comments_get
	url := fmt.Sprintf("%s?method=client.service_comments_get&service_id=%s", u.baseURL, serviceID)
	req, _ := http.NewRequest("GET", url, nil)
	req.SetBasicAuth(u.user, u.pass)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	var result map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return ""
	}

	if status, ok := result["status"].(bool); !ok || !status {
		return ""
	}

	// Los comments pueden venir en diferentes formatos
	// Intentar extraer de data (puede ser array o map)
	if data, ok := result["data"]; ok {
		var commentsText strings.Builder

		// Si es un array de comments
		if commentsArray, ok := data.([]interface{}); ok {
			for _, comment := range commentsArray {
				if commentMap, ok := comment.(map[string]interface{}); ok {
					// Buscar en diferentes campos del comment
					if text, ok := commentMap["comment"].(string); ok {
						commentsText.WriteString(text + " ")
					}
					if text, ok := commentMap["note"].(string); ok {
						commentsText.WriteString(text + " ")
					}
					if text, ok := commentMap["body"].(string); ok {
						commentsText.WriteString(text + " ")
					}
				}
			}
		}

		// Si es un map con comments
		if commentsMap, ok := data.(map[string]interface{}); ok {
			for _, comment := range commentsMap {
				if commentStr, ok := comment.(string); ok {
					commentsText.WriteString(commentStr + " ")
				}
			}
		}

		// Extraer VLAN del texto usando regex
		if commentsText.Len() > 0 {
			return u.extractVLANWithRegex(commentsText.String())
		}
	}

	return ""
}

// extractVLANFromText busca VLAN en campos de texto del servicio usando expresiones regulares
func (u *UbersmithAdapter) extractVLANFromText(serviceData map[string]interface{}) string {
	// Campos de texto donde buscar (incluyendo posibles variaciones de nombres)
	textFields := []string{
		"description", "desc", "notes", "note", "comments", "comment",
		"details", "detail", "info", "information", "remarks", "remark",
		"vlan_id", "vlanid", // Por si acaso viene como texto en lugar de campo directo
	}

	var textContent strings.Builder

	for _, fieldName := range textFields {
		if val, ok := serviceData[fieldName]; ok {
			if valStr, ok := val.(string); ok && valStr != "" {
				textContent.WriteString(valStr + " ")
			}
		}
	}

	if textContent.Len() > 0 {
		return u.extractVLANWithRegex(textContent.String())
	}

	return ""
}

// extractVLANWithRegex extrae el VLAN de un texto usando expresiones regulares
// Busca patrones como: "623 [Untagged]", "623 [Tagged]", VLAN 123, vlan:123, etc.
func (u *UbersmithAdapter) extractVLANWithRegex(text string) string {
	// Patrones regex para encontrar VLAN (usando flag (?i) para case-insensitive)
	// Ordenados por prioridad: primero los más específicos y comunes
	patterns := []*regexp.Regexp{
		// "VLAN ID: 623 [Untagged]" o "VLAN ID: 623 [Tagged]" - Formato exacto de Ubersmith (PRIORIDAD MÁXIMA)
		regexp.MustCompile(`(?i)vlan\s+id\s*[:=\-\s]*(\d+)\s*\[(?:Untagged|Tagged)\]`),
		// "623 [Untagged]", "623 [Tagged]" - Formato común en Ubersmith
		regexp.MustCompile(`(\d+)\s*\[(?:Untagged|Tagged)\]`),
		// "vlan_id: 623 [Untagged]" o "vlanid: 623 [Tagged]"
		regexp.MustCompile(`(?i)vlan[_\-]?id\s*[:=\-\s]*(\d+)\s*\[(?:Untagged|Tagged)\]`),
		// "vlan_id:123", "vlanid:123"
		regexp.MustCompile(`(?i)vlan[_\-]?id\s*[:=\-\s]+(\d+)`),
		// "vlan 123", "vlan:123", "vlan=123", "vlan-123"
		regexp.MustCompile(`(?i)vlan\s*[:=\-\s]+(\d+)`),
		// "vlan123", "VLAN123"
		regexp.MustCompile(`(?i)vlan\s*(\d{1,4})`),
		// Solo números de 1-4 dígitos precedidos por "vlan" o seguidos de contexto
		regexp.MustCompile(`(?i)(?:^|\s)vlan\s*(\d{1,4})(?:\s|$|[^\d])`),
		// "VLAN tag: 123"
		regexp.MustCompile(`(?i)vlan\s*tag\s*[:=\-\s]+(\d+)`),
	}

	for _, pattern := range patterns {
		matches := pattern.FindStringSubmatch(text)
		if len(matches) > 1 && matches[1] != "" {
			vlan := strings.TrimSpace(matches[1])
			// Validar que sea un número válido de VLAN (1-4094)
			if vlanNum, err := strconv.Atoi(vlan); err == nil {
				if vlanNum >= 1 && vlanNum <= 4094 {
					return vlan
				}
			}
		}
	}

	return ""
}

// getVLANFromOtherMetaTypes busca VLAN en custom fields de otros meta_types
func (u *UbersmithAdapter) getVLANFromOtherMetaTypes(serviceID string) string {
	// Intentar con diferentes meta_types
	metaTypes := []string{"service", "client", "order"}

	for _, metaType := range metaTypes {
		customFieldVars := u.getCustomFieldVariables(metaType)
		if customFieldVars.vlanVar != "" {
			vlanRaw := u.getCustomFieldValue(customFieldVars.vlanVar, metaType, serviceID)
			if vlanRaw != "" {
				// Procesar el valor para extraer el número si viene con formato "623 [Untagged]"
				vlan := u.extractVLANNumberFromText(vlanRaw)
				if vlan != "" {
					return vlan
				}
				// Si no se pudo extraer (no tiene formato con corchetes), usar el valor original
				return vlanRaw
			}
		}

		// Fallback: intentar con nombres conocidos
		fallbackVars := []string{"vlan", "vlan_id", "vlanid", "vlan_number", "vlan_tag"}
		for _, varName := range fallbackVars {
			vlanRaw := u.getCustomFieldValue(varName, metaType, serviceID)
			if vlanRaw != "" {
				// Procesar el valor para extraer el número si viene con formato "623 [Untagged]"
				vlan := u.extractVLANNumberFromText(vlanRaw)
				if vlan != "" {
					return vlan
				}
				// Si no se pudo extraer (no tiene formato con corchetes), usar el valor original
				return vlanRaw
			}
		}
	}

	return ""
}

// getServiceCustomFields obtiene los custom fields del servicio usando metadata_field_list y metadata_bulk_get
func (u *UbersmithAdapter) getServiceCustomFields(serviceID string) (vlan, user, pass string, err error) {
	// Obtener los nombres de las variables de custom fields
	customFieldVars := u.getCustomFieldVariables("pack")

	// Obtener los valores usando los nombres encontrados
	if customFieldVars.vlanVar != "" {
		vlanRaw := u.getCustomFieldValue(customFieldVars.vlanVar, "pack", serviceID)
		// Procesar el valor para extraer el número si viene con formato "623 [Untagged]"
		if vlanRaw != "" {
			vlan = u.extractVLANNumberFromText(vlanRaw)
			// Si no se pudo extraer (no tiene formato con corchetes), usar el valor original
			if vlan == "" {
				vlan = vlanRaw
			}
		}
	}
	if customFieldVars.userVar != "" {
		user = u.getCustomFieldValue(customFieldVars.userVar, "pack", serviceID)
	}
	if customFieldVars.passVar != "" {
		pass = u.getCustomFieldValue(customFieldVars.passVar, "pack", serviceID)
	}

	// Fallback: intentar con nombres conocidos si no encontramos
	if vlan == "" || user == "" || pass == "" {
		// Más variaciones de nombres de campos para VLAN
		vlanFallbacks := []string{
			"vlan_id", "vlanid", "vlan", "vlan_number", "vlan_num",
			"vlan_tag", "vlantag", "vlan_tag_id", "vlanid_number",
		}
		// Más variaciones para username
		userFallbacks := []string{
			"username", "user", "pppoe_user", "pppoe_username",
			"pppo_user", "pppo_username", "ppp_username", "ppp_user",
		}
		// Más variaciones para password
		passFallbacks := []string{
			"password", "pass", "pppoe_password", "pppoe_pass",
			"pppo_password", "pppo_pass", "ppp_password", "ppp_pass",
		}

		for _, varName := range vlanFallbacks {
			if vlan == "" {
				vlanRaw := u.getCustomFieldValue(varName, "pack", serviceID)
				if vlanRaw != "" {
					// Procesar el valor para extraer el número si viene con formato "623 [Untagged]"
					vlan = u.extractVLANNumberFromText(vlanRaw)
					// Si no se pudo extraer (no tiene formato con corchetes), usar el valor original
					if vlan == "" {
						vlan = vlanRaw
					}
					if vlan != "" {
						break
					}
				}
			}
		}

		for _, varName := range userFallbacks {
			if user == "" {
				user = u.getCustomFieldValue(varName, "pack", serviceID)
				if user != "" {
					break
				}
			}
		}

		for _, varName := range passFallbacks {
			if pass == "" {
				pass = u.getCustomFieldValue(varName, "pack", serviceID)
				if pass != "" {
					break
				}
			}
		}
	}

	return vlan, user, pass, nil
}

type customFieldVars struct {
	vlanVar string
	userVar string
	passVar string
}

// getCustomFieldVariables obtiene los nombres de las variables de custom fields usando uber.metadata_field_list
func (u *UbersmithAdapter) getCustomFieldVariables(metaType string) customFieldVars {
	vars := customFieldVars{}
	url := fmt.Sprintf("%s?method=uber.metadata_field_list&meta_type=%s", u.baseURL, metaType)

	req, _ := http.NewRequest("GET", url, nil)
	req.SetBasicAuth(u.user, u.pass)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return vars
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return vars
	}

	var result map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return vars
	}

	if status, ok := result["status"].(bool); !ok || !status {
		return vars
	}

	if data, ok := result["data"].(map[string]interface{}); ok {
		for _, cfData := range data {
			if cfObj, ok := cfData.(map[string]interface{}); ok {
				if variable, ok := cfObj["variable"].(string); ok {
					variableLower := strings.ToLower(variable)
					label := ""
					if labelVal, ok := cfObj["label"].(string); ok {
						label = strings.ToLower(labelVal)
					}

					// Buscar VLAN con más variaciones
					if vars.vlanVar == "" {
						vlanKeywords := []string{"vlan", "vlantag", "vlan_tag", "vlanid", "vlan_id"}
						for _, keyword := range vlanKeywords {
							if strings.Contains(variableLower, keyword) || strings.Contains(label, keyword) {
								vars.vlanVar = variable
								break
							}
						}
					}

					// Buscar PPPoE User
					if vars.userVar == "" {
						if (strings.Contains(variableLower, "pppoe") || strings.Contains(variableLower, "pppo") ||
							strings.Contains(variableLower, "username") || strings.Contains(variableLower, "user")) &&
							!strings.Contains(variableLower, "pass") && !strings.Contains(variableLower, "password") {
							vars.userVar = variable
						}
					}

					// Buscar PPPoE Pass
					if vars.passVar == "" {
						if (strings.Contains(variableLower, "pppoe") || strings.Contains(variableLower, "pppo") ||
							strings.Contains(variableLower, "password") || strings.Contains(variableLower, "pass")) &&
							!strings.Contains(variableLower, "user") && !strings.Contains(variableLower, "username") {
							vars.passVar = variable
						}
					}
				}
			}
		}
	}

	return vars
}

// getCustomFieldValue obtiene el valor de un custom field usando uber.metadata_bulk_get
func (u *UbersmithAdapter) getCustomFieldValue(variable, metaType, serviceID string) string {
	if variable == "" {
		return ""
	}
	url := fmt.Sprintf("%s?method=uber.metadata_bulk_get&variable=%s&meta_type=%s", u.baseURL, variable, metaType)

	req, _ := http.NewRequest("GET", url, nil)
	req.SetBasicAuth(u.user, u.pass)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	var result map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return ""
	}

	if status, ok := result["status"].(bool); !ok || !status {
		return ""
	}

	if data, ok := result["data"].(map[string]interface{}); ok {
		// Buscar como string
		if val, ok := data[serviceID]; ok {
			if valStr, ok := val.(string); ok && valStr != "" {
				return strings.TrimSpace(valStr)
			}
			// También intentar como número
			if valNum, ok := val.(float64); ok {
				return fmt.Sprintf("%.0f", valNum)
			}
			if valNum, ok := val.(int); ok {
				return strconv.Itoa(valNum)
			}
		}

		// Buscar como número (serviceID como número)
		serviceIDNum, err := strconv.Atoi(serviceID)
		if err == nil {
			serviceIDStr := fmt.Sprintf("%d", serviceIDNum)
			if val, ok := data[serviceIDStr]; ok {
				if valStr, ok := val.(string); ok && valStr != "" {
					return strings.TrimSpace(valStr)
				}
				// También intentar como número
				if valNum, ok := val.(float64); ok {
					return fmt.Sprintf("%.0f", valNum)
				}
				if valNum, ok := val.(int); ok {
					return strconv.Itoa(valNum)
				}
			}
		}
	}

	return ""
}

// maskPassword oculta la contraseña para logging
func maskPassword(pass string) string {
	if pass == "" {
		return ""
	}
	if len(pass) <= 4 {
		return "****"
	}
	return pass[:2] + "****" + pass[len(pass)-2:]
}
