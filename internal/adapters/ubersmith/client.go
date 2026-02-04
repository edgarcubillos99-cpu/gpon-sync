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

// GetServiceDetails busca credenciales PPPoE por CID (Service ID en Ubersmith)
func (u *UbersmithAdapter) GetServiceDetails(cid string) (user, pass string, err error) {
	// ESTRATEGIA 1: Custom Fields (pack meta_type)
	user, pass, _ = u.getServiceCustomFields(cid)

	// ESTRATEGIA 2: Obtener datos completos del servicio para buscar en campos directos
	serviceData, err := u.getServiceData(cid)
	if err != nil {
		// Si falla pero tenemos datos de custom fields, los retornamos
		if user != "" || pass != "" {
			return user, pass, nil
		}
		return "", "", err
	}

	// Buscar username y password en campos directos del servicio
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

	return user, pass, nil
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

// getServiceCustomFields obtiene los custom fields del servicio usando metadata_field_list y metadata_bulk_get
func (u *UbersmithAdapter) getServiceCustomFields(serviceID string) (user, pass string, err error) {
	// Obtener los nombres de las variables de custom fields
	customFieldVars := u.getCustomFieldVariables("pack")

	// Obtener los valores usando los nombres encontrados
	if customFieldVars.userVar != "" {
		user = u.getCustomFieldValue(customFieldVars.userVar, "pack", serviceID)
	}
	if customFieldVars.passVar != "" {
		pass = u.getCustomFieldValue(customFieldVars.passVar, "pack", serviceID)
	}

	// Fallback: intentar con nombres conocidos si no encontramos
	if user == "" || pass == "" {
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

	return user, pass, nil
}

type customFieldVars struct {
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
