package ubersmith

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
func (u *UbersmithAdapter) GetServiceDetails(cid string) (vlan, user, pass string, err error) {
	// Primero intentamos obtener los custom fields usando metadata_field_list y metadata_bulk_get
	vlan, user, pass, err = u.getServiceCustomFields(cid)
	if err == nil && (vlan != "" || user != "" || pass != "") {
		return vlan, user, pass, nil
	}

	// Si no funcionó, intentamos con client.service_get como fallback
	url := fmt.Sprintf("%s?method=client.service_get&service_id=%s", u.baseURL, cid)
	req, _ := http.NewRequest("GET", url, nil)
	req.SetBasicAuth(u.user, u.pass)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", "", err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return "", "", "", err
	}

	if status, ok := result["status"].(bool); !ok || !status {
		return "", "", "", fmt.Errorf("respuesta de Ubersmith indica error")
	}

	if data, ok := result["data"].(map[string]interface{}); ok {
		// Buscamos campos directos
		if usernameVal, ok := data["username"].(string); ok && usernameVal != "" {
			user = usernameVal
		}
		if passwordVal, ok := data["password"].(string); ok && passwordVal != "" {
			pass = passwordVal
		}
	}

	return vlan, user, pass, nil
}

// getServiceCustomFields obtiene los custom fields del servicio usando metadata_field_list y metadata_bulk_get
func (u *UbersmithAdapter) getServiceCustomFields(serviceID string) (vlan, user, pass string, err error) {
	// Obtener los nombres de las variables de custom fields
	customFieldVars := u.getCustomFieldVariables("pack")

	// Obtener los valores usando los nombres encontrados
	if customFieldVars.vlanVar != "" {
		vlan = u.getCustomFieldValue(customFieldVars.vlanVar, "pack", serviceID)
	}
	if customFieldVars.userVar != "" {
		user = u.getCustomFieldValue(customFieldVars.userVar, "pack", serviceID)
	}
	if customFieldVars.passVar != "" {
		pass = u.getCustomFieldValue(customFieldVars.passVar, "pack", serviceID)
	}

	// Fallback: intentar con nombres conocidos si no encontramos
	if vlan == "" || user == "" || pass == "" {
		fallbackVars := []struct {
			vlanVar string
			userVar string
			passVar string
		}{
			{"vlan_id", "username", "password"},
		}

		for _, vars := range fallbackVars {
			if vlan == "" {
				vlan = u.getCustomFieldValue(vars.vlanVar, "pack", serviceID)
			}
			if user == "" {
				user = u.getCustomFieldValue(vars.userVar, "pack", serviceID)
			}
			if pass == "" {
				pass = u.getCustomFieldValue(vars.passVar, "pack", serviceID)
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

					// Buscar VLAN
					if vars.vlanVar == "" {
						if strings.Contains(variableLower, "vlan") || strings.Contains(label, "vlan") {
							vars.vlanVar = variable
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
			if valStr, ok := val.(string); ok {
				return valStr
			}
		}

		// Buscar como número
		serviceIDNum, err := strconv.Atoi(serviceID)
		if err == nil {
			serviceIDStr := fmt.Sprintf("%d", serviceIDNum)
			if val, ok := data[serviceIDStr]; ok {
				if valStr, ok := val.(string); ok {
					return valStr
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
