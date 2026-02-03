package ubersmith

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
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
// En Ubersmith, el CID corresponde al Service ID (Service #157591)
func (u *UbersmithAdapter) GetServiceDetails(cid string) (vlan, user, pass string, err error) {
	// Intentamos diferentes métodos comunes de Ubersmith API para obtener detalles del servicio
	// El CID es el Service ID en Ubersmith
	methods := []struct {
		method string
		param  string
	}{
		{"client.service_get", "service_id"},  // Método más común: obtener servicio por ID
		{"service.get", "service_id"},         // Alternativa común
		{"uber.service_get", "service_id"},    // Otra alternativa
		{"client.service_list", "service_id"}, // Listar servicios filtrados por ID
		{"service.list", "service_id"},        // Alternativa de listado
	}

	var lastErr error
	for _, methodInfo := range methods {
		// Construimos la URL con el parámetro correcto (service_id en lugar de custom_field_CID)
		// Intentamos incluir custom fields si es posible
		url := fmt.Sprintf("%s?method=%s&%s=%s", u.baseURL, methodInfo.method, methodInfo.param, cid)
		if methodInfo.method == "client.service_get" {
			// Intentamos también con include_custom_fields
			urlWithCustomFields := fmt.Sprintf("%s?method=%s&%s=%s&include_custom_fields=1", u.baseURL, methodInfo.method, methodInfo.param, cid)
			// Primero intentamos con custom fields
			if vlan, user, pass, err := u.tryGetServiceWithCustomFields(urlWithCustomFields, cid); err == nil && (vlan != "" || user != "" || pass != "") {
				return vlan, user, pass, nil
			}
		}

		req, _ := http.NewRequest("GET", url, nil)
		req.SetBasicAuth(u.user, u.pass)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()

		// Leemos la respuesta completa para debugging
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			lastErr = fmt.Errorf("error leyendo respuesta: %v", err)
			continue
		}

		// Log para debugging: mostrar la respuesta raw (limitado a primeros 500 caracteres)
		if len(bodyBytes) > 0 {
			preview := string(bodyBytes)
			if len(preview) > 500 {
				preview = preview[:500] + "..."
			}
			log.Printf("[DEBUG Ubersmith] CID %s - Método '%s' (Service ID) - Respuesta: %s", cid, methodInfo.method, preview)
		}

		// Intentamos parsear la respuesta de manera flexible
		var result map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			lastErr = fmt.Errorf("error parseando JSON: %v", err)
			continue
		}

		// Verificamos el status
		if status, ok := result["status"].(bool); !ok || !status {
			// Si hay un error_message, lo mostramos
			if errMsg, ok := result["error_message"].(string); ok {
				log.Printf("[DEBUG Ubersmith] CID %s - Método '%s' (Service ID) falló: %s", cid, methodInfo.method, errMsg)
			}
			lastErr = fmt.Errorf("respuesta de Ubersmith indica error o status=false")
			continue
		}

		// Obtenemos el campo data
		dataRaw, ok := result["data"]
		if !ok {
			lastErr = fmt.Errorf("campo 'data' no encontrado en respuesta")
			continue
		}

		// El campo data puede ser un objeto/mapa o un array
		var foundVlan, foundPPPoEUser, foundPPPoEPass string

		// Caso 1: data es un mapa/objeto (respuesta directa del servicio)
		if serviceObj, ok := dataRaw.(map[string]interface{}); ok {
			log.Printf("[DEBUG Ubersmith] CID %s - Analizando servicio directamente", cid)
			allKeys := getKeys(serviceObj)
			log.Printf("[DEBUG Ubersmith] CID %s - Campos disponibles en data (%d): %v", cid, len(allKeys), allKeys)

			// Primero buscamos campos directos comunes
			// username y password pueden estar directamente en el servicio
			if usernameVal, ok := serviceObj["username"]; ok {
				log.Printf("[DEBUG Ubersmith] CID %s - Campo 'username' encontrado, tipo: %T, valor: '%v'", cid, usernameVal, usernameVal)
				if usernameStr, ok := usernameVal.(string); ok {
					if usernameStr != "" {
						foundPPPoEUser = usernameStr
						log.Printf("[DEBUG Ubersmith] CID %s - ✅ Encontrado username directo: %s", cid, foundPPPoEUser)
					} else {
						log.Printf("[DEBUG Ubersmith] CID %s - Campo 'username' está vacío", cid)
					}
				}
			} else {
				log.Printf("[DEBUG Ubersmith] CID %s - Campo 'username' NO encontrado en respuesta", cid)
			}
			if passwordVal, ok := serviceObj["password"]; ok {
				log.Printf("[DEBUG Ubersmith] CID %s - Campo 'password' encontrado, tipo: %T, valor: '%v'", cid, passwordVal, maskPassword(fmt.Sprintf("%v", passwordVal)))
				if passwordStr, ok := passwordVal.(string); ok {
					if passwordStr != "" {
						foundPPPoEPass = passwordStr
						log.Printf("[DEBUG Ubersmith] CID %s - ✅ Encontrado password directo: %s", cid, maskPassword(foundPPPoEPass))
					} else {
						log.Printf("[DEBUG Ubersmith] CID %s - Campo 'password' está vacío", cid)
					}
				}
			} else {
				log.Printf("[DEBUG Ubersmith] CID %s - Campo 'password' NO encontrado en respuesta", cid)
			}

			// Buscamos campos que contengan palabras clave en su nombre
			for key, val := range serviceObj {
				keyLower := strings.ToLower(key)
				valStr := ""

				// Convertir el valor a string si es posible
				if str, ok := val.(string); ok {
					valStr = str
				} else if num, ok := val.(float64); ok {
					valStr = fmt.Sprintf("%.0f", num)
				} else if num, ok := val.(int); ok {
					valStr = fmt.Sprintf("%d", num)
				}

				if valStr != "" {
					// Buscar VLAN
					if strings.Contains(keyLower, "vlan") && foundVlan == "" {
						foundVlan = valStr
						log.Printf("[DEBUG Ubersmith] CID %s - Encontrado VLAN en campo '%s': %s", cid, key, foundVlan)
					}
					// Buscar PPPoE User (puede estar como username o en campos personalizados)
					if (strings.Contains(keyLower, "pppoe") || strings.Contains(keyLower, "pppo")) &&
						(strings.Contains(keyLower, "user") || strings.Contains(keyLower, "username")) &&
						foundPPPoEUser == "" {
						foundPPPoEUser = valStr
						log.Printf("[DEBUG Ubersmith] CID %s - Encontrado PPPoE User en campo '%s': %s", cid, key, foundPPPoEUser)
					}
					// Buscar PPPoE Pass
					if (strings.Contains(keyLower, "pppoe") || strings.Contains(keyLower, "pppo")) &&
						(strings.Contains(keyLower, "pass") || strings.Contains(keyLower, "password")) &&
						foundPPPoEPass == "" {
						foundPPPoEPass = valStr
						log.Printf("[DEBUG Ubersmith] CID %s - Encontrado PPPoE Pass en campo '%s': %s", cid, key, maskPassword(foundPPPoEPass))
					}
				}
			}

			// Si encontramos al menos un campo, retornamos
			if foundVlan != "" || foundPPPoEUser != "" || foundPPPoEPass != "" {
				log.Printf("[DEBUG Ubersmith] CID %s - ✅ Encontrados: VLAN=%s, User=%s, Pass=%s", cid, foundVlan, foundPPPoEUser, maskPassword(foundPPPoEPass))
				return foundVlan, foundPPPoEUser, foundPPPoEPass, nil
			}

			// Si no encontramos, los campos pueden estar en custom fields (IDs numéricos como claves)
			// Los custom fields en Ubersmith pueden estar como claves numéricas en la respuesta
			// Buscamos todas las claves que sean números (custom field IDs)
			customFieldValues := make(map[string]string)
			for key, val := range serviceObj {
				// Verificamos si la clave es un número (custom field ID)
				var testNum int
				if _, err := fmt.Sscanf(key, "%d", &testNum); err == nil {
					// Es un número, es un custom field ID
					valStr := ""
					if str, ok := val.(string); ok {
						valStr = str
					} else if num, ok := val.(float64); ok {
						valStr = fmt.Sprintf("%.0f", num)
					} else if num, ok := val.(int); ok {
						valStr = fmt.Sprintf("%d", num)
					}
					if valStr != "" {
						customFieldValues[key] = valStr
						log.Printf("[DEBUG Ubersmith] CID %s - Custom field ID %s = '%s'", cid, key, valStr)
					}
				}
			}

			// Buscamos en los valores de custom fields por contenido (VLAN, PPPoE, etc.)
			if len(customFieldValues) > 0 {
				log.Printf("[DEBUG Ubersmith] CID %s - Analizando %d custom fields...", cid, len(customFieldValues))
				for cfID, cfVal := range customFieldValues {
					cfValLower := strings.ToLower(cfVal)
					// Buscamos VLAN (puede ser solo números)
					if foundVlan == "" {
						// VLAN suele ser un número, pero también puede tener formato "VLAN 123"
						if strings.Contains(cfValLower, "vlan") || (len(cfVal) > 0 && cfVal[0] >= '0' && cfVal[0] <= '9' && len(cfVal) <= 10) {
							// Puede ser VLAN si es un número corto o contiene "vlan"
							foundVlan = cfVal
							log.Printf("[DEBUG Ubersmith] CID %s - ✅ Posible VLAN encontrado en custom field %s: %s", cid, cfID, foundVlan)
						}
					}
					// Buscamos PPPoE User (puede contener @, ser alfanumérico, etc.)
					if foundPPPoEUser == "" {
						if strings.Contains(cfValLower, "@") || (len(cfVal) > 3 && len(cfVal) < 50) {
							// Puede ser username si tiene @ o es alfanumérico de longitud razonable
							foundPPPoEUser = cfVal
							log.Printf("[DEBUG Ubersmith] CID %s - ✅ Posible PPPoE User encontrado en custom field %s: %s", cid, cfID, foundPPPoEUser)
						}
					}
					// Buscamos PPPoE Pass (puede ser alfanumérico)
					if foundPPPoEPass == "" {
						if len(cfVal) >= 4 && len(cfVal) <= 50 && !strings.Contains(cfVal, "@") {
							// Puede ser password si no tiene @ y tiene longitud razonable
							foundPPPoEPass = cfVal
							log.Printf("[DEBUG Ubersmith] CID %s - ✅ Posible PPPoE Pass encontrado en custom field %s: %s", cid, cfID, maskPassword(foundPPPoEPass))
						}
					}
				}
			}

			// Si encontramos al menos un campo en custom fields, retornamos
			if foundVlan != "" || foundPPPoEUser != "" || foundPPPoEPass != "" {
				log.Printf("[DEBUG Ubersmith] CID %s - ✅ Encontrados en custom fields: VLAN=%s, User=%s, Pass=%s", cid, foundVlan, foundPPPoEUser, maskPassword(foundPPPoEPass))
				return foundVlan, foundPPPoEUser, foundPPPoEPass, nil
			}

			// Si aún no encontramos, intentamos obtener los custom fields con una llamada específica
			if foundVlan == "" && foundPPPoEUser == "" && foundPPPoEPass == "" {
				log.Printf("[DEBUG Ubersmith] CID %s - ⚠️  No se encontraron en campos directos. Intentando obtener custom fields...", cid)
				vlan2, user2, pass2, err2 := u.getServiceCustomFields(cid)
				if err2 == nil && (vlan2 != "" || user2 != "" || pass2 != "") {
					log.Printf("[DEBUG Ubersmith] CID %s - ✅ Encontrados en custom fields: VLAN=%s, User=%s, Pass=%s", cid, vlan2, user2, maskPassword(pass2))
					return vlan2, user2, pass2, nil
				}
			} else {
				log.Printf("[DEBUG Ubersmith] CID %s - ⚠️  Campos parciales encontrados: VLAN=%s, User=%s, Pass=%s", cid, foundVlan, foundPPPoEUser, maskPassword(foundPPPoEPass))
			}
		}

		// Caso 1b: data es un mapa que contiene otros servicios (estructura anidada)
		if dataMap, ok := dataRaw.(map[string]interface{}); ok {
			// Buscamos en el primer servicio del mapa
			for serviceID, serviceData := range dataMap {
				if serviceObj, ok := serviceData.(map[string]interface{}); ok {
					log.Printf("[DEBUG Ubersmith] CID %s - Analizando servicio ID: %s", cid, serviceID)
					log.Printf("[DEBUG Ubersmith] CID %s - Campos disponibles: %v", cid, getKeys(serviceObj))

					// Buscamos los campos de manera flexible
					foundVlan = findField(serviceObj, []string{"vlan", "vlan_field", "vlan_id", "VLAN", "Vlan", "vlan_tag"})
					foundPPPoEUser = findField(serviceObj, []string{"pppoe_user", "pppoe_username", "pppoe_user_field", "PPPoEUser", "pppoe_user_name", "pppoe_usr"})
					foundPPPoEPass = findField(serviceObj, []string{"pppoe_password", "pppoe_pass", "pppoe_pass_field", "PPPoEPass", "pppoe_password_field", "pppoe_passwd"})

					if foundVlan != "" || foundPPPoEUser != "" || foundPPPoEPass != "" {
						log.Printf("[DEBUG Ubersmith] CID %s - ✅ Encontrados: VLAN=%s, User=%s, Pass=%s", cid, foundVlan, foundPPPoEUser, maskPassword(foundPPPoEPass))
						return foundVlan, foundPPPoEUser, foundPPPoEPass, nil
					}
				}
			}
		}

		// Caso 2: data es un array
		if dataArray, ok := dataRaw.([]interface{}); ok {
			for i, item := range dataArray {
				if serviceObj, ok := item.(map[string]interface{}); ok {
					log.Printf("[DEBUG Ubersmith] CID %s - Analizando servicio índice: %d", cid, i)
					log.Printf("[DEBUG Ubersmith] CID %s - Campos disponibles: %v", cid, getKeys(serviceObj))

					foundVlan = findField(serviceObj, []string{"vlan", "vlan_field", "vlan_id", "VLAN", "Vlan"})
					foundPPPoEUser = findField(serviceObj, []string{"pppoe_user", "pppoe_username", "pppoe_user_field", "PPPoEUser", "pppoe_user_name"})
					foundPPPoEPass = findField(serviceObj, []string{"pppoe_password", "pppoe_pass", "pppoe_pass_field", "PPPoEPass", "pppoe_password_field"})

					if foundVlan != "" || foundPPPoEUser != "" || foundPPPoEPass != "" {
						log.Printf("[DEBUG Ubersmith] CID %s - Encontrados: VLAN=%s, User=%s, Pass=%s", cid, foundVlan, foundPPPoEUser, maskPassword(foundPPPoEPass))
						return foundVlan, foundPPPoEUser, foundPPPoEPass, nil
					}
				}
			}
		}

		// Caso 3: data es un string (error común)
		if dataStr, ok := dataRaw.(string); ok {
			log.Printf("[WARN] CID %s - Ubersmith devolvió data como string: %s", cid, dataStr)
			lastErr = fmt.Errorf("respuesta de Ubersmith tiene formato inesperado (data es string): %s", dataStr)
			continue
		}

		// Si llegamos aquí, no encontramos los campos en este método
		lastErr = fmt.Errorf("servicio no encontrado o campos no disponibles para CID: %s", cid)
		continue
	}

	// Si todos los métodos fallaron, retornamos el último error
	return "", "", "", lastErr
}

// findField busca un campo en el objeto usando diferentes nombres posibles
func findField(obj map[string]interface{}, fieldNames []string) string {
	for _, fieldName := range fieldNames {
		if val, ok := obj[fieldName]; ok {
			if str, ok := val.(string); ok && str != "" {
				return str
			}
			// También intentamos convertir números a string
			if num, ok := val.(float64); ok {
				return fmt.Sprintf("%.0f", num)
			}
		}
	}
	return ""
}

// getKeys obtiene todas las claves de un mapa para debugging
func getKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// tryGetServiceWithCustomFields intenta obtener el servicio incluyendo custom fields
func (u *UbersmithAdapter) tryGetServiceWithCustomFields(url string, cid string) (vlan, user, pass string, err error) {
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
		return "", "", "", fmt.Errorf("status false")
	}

	if data, ok := result["data"].(map[string]interface{}); ok {
		return u.extractFieldsFromServiceData(data, cid)
	}

	return "", "", "", fmt.Errorf("data no es un mapa")
}

// extractFieldsFromServiceData extrae VLAN, PPPoE User y PPPoE Pass de los datos del servicio
func (u *UbersmithAdapter) extractFieldsFromServiceData(serviceObj map[string]interface{}, cid string) (vlan, user, pass string, err error) {
	// Buscamos campos directos primero
	if usernameVal, ok := serviceObj["username"]; ok {
		if usernameStr, ok := usernameVal.(string); ok && usernameStr != "" {
			user = usernameStr
		}
	}
	if passwordVal, ok := serviceObj["password"]; ok {
		if passwordStr, ok := passwordVal.(string); ok && passwordStr != "" {
			pass = passwordStr
		}
	}

	// Buscamos en custom fields (claves numéricas)
	for key, val := range serviceObj {
		// Verificamos si la clave es un número (custom field ID)
		var testNum int
		if _, err := fmt.Sscanf(key, "%d", &testNum); err == nil {
			// Es un custom field ID, obtenemos su valor
			valStr := ""
			if str, ok := val.(string); ok {
				valStr = str
			} else if num, ok := val.(float64); ok {
				valStr = fmt.Sprintf("%.0f", num)
			} else if num, ok := val.(int); ok {
				valStr = fmt.Sprintf("%d", num)
			}

			if valStr != "" {
				// Necesitamos obtener el nombre del custom field para saber qué es
				// Por ahora, intentamos obtenerlo con una llamada adicional
				cfName, cfValue := u.getCustomFieldNameAndValue(key, valStr, cid)
				if cfName != "" && cfValue != "" {
					cfNameLower := strings.ToLower(cfName)
					if strings.Contains(cfNameLower, "vlan") && vlan == "" {
						vlan = cfValue
						log.Printf("[DEBUG Ubersmith] CID %s - ✅ VLAN encontrado en custom field %s (%s): %s", cid, key, cfName, vlan)
					}
					if (strings.Contains(cfNameLower, "pppoe") || strings.Contains(cfNameLower, "pppo")) &&
						(strings.Contains(cfNameLower, "user") || strings.Contains(cfNameLower, "username")) &&
						user == "" {
						user = cfValue
						log.Printf("[DEBUG Ubersmith] CID %s - ✅ PPPoE User encontrado en custom field %s (%s): %s", cid, key, cfName, user)
					}
					if (strings.Contains(cfNameLower, "pppoe") || strings.Contains(cfNameLower, "pppo")) &&
						(strings.Contains(cfNameLower, "pass") || strings.Contains(cfNameLower, "password")) &&
						pass == "" {
						pass = cfValue
						log.Printf("[DEBUG Ubersmith] CID %s - ✅ PPPoE Pass encontrado en custom field %s (%s): %s", cid, key, cfName, maskPassword(pass))
					}
				}
			}
		}
	}

	return vlan, user, pass, nil
}

// getCustomFieldNameAndValue obtiene el nombre y valor de un custom field
func (u *UbersmithAdapter) getCustomFieldNameAndValue(cfID, cfValue, cid string) (name, value string) {
	// Intentamos obtener el nombre del custom field
	methods := []string{
		"client.custom_field_get",
		"custom_field.get",
		"client.custom_field_list",
	}

	for _, method := range methods {
		url := fmt.Sprintf("%s?method=%s&custom_field_id=%s", u.baseURL, method, cfID)

		req, _ := http.NewRequest("GET", url, nil)
		req.SetBasicAuth(u.user, u.pass)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			continue
		}

		var result map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			continue
		}

		if status, ok := result["status"].(bool); !ok || !status {
			continue
		}

		if data, ok := result["data"].(map[string]interface{}); ok {
			if nameVal, ok := data["name"].(string); ok {
				return nameVal, cfValue
			}
			if labelVal, ok := data["label"].(string); ok {
				return labelVal, cfValue
			}
		}
	}

	// Si no podemos obtener el nombre, retornamos el valor directamente
	return "", cfValue
}

// getServiceCustomFields obtiene los custom fields del servicio
// Primero obtiene la lista de custom fields para encontrar los nombres exactos de las variables
func (u *UbersmithAdapter) getServiceCustomFields(serviceID string) (vlan, user, pass string, err error) {
	// Paso 1: Obtener la lista de custom fields de tipo "pack" (servicios) para encontrar los nombres exactos
	customFieldVars := u.getCustomFieldVariables("pack")

	log.Printf("[DEBUG Ubersmith] CID %s - Custom fields encontrados: VLAN=%s, User=%s, Pass=%s",
		serviceID, customFieldVars.vlanVar, customFieldVars.userVar, customFieldVars.passVar)

	// Paso 2: Si encontramos los nombres de las variables, obtener los valores
	if customFieldVars.vlanVar != "" {
		vlan = u.getCustomFieldValue(customFieldVars.vlanVar, "pack", serviceID)
		if vlan != "" {
			log.Printf("[DEBUG Ubersmith] CID %s - ✅ VLAN encontrado en custom field '%s': %s", serviceID, customFieldVars.vlanVar, vlan)
		}
	}

	if customFieldVars.userVar != "" {
		user = u.getCustomFieldValue(customFieldVars.userVar, "pack", serviceID)
		if user != "" {
			log.Printf("[DEBUG Ubersmith] CID %s - ✅ PPPoE User encontrado en custom field '%s': %s", serviceID, customFieldVars.userVar, user)
		}
	}

	if customFieldVars.passVar != "" {
		pass = u.getCustomFieldValue(customFieldVars.passVar, "pack", serviceID)
		if pass != "" {
			log.Printf("[DEBUG Ubersmith] CID %s - ✅ PPPoE Pass encontrado en custom field '%s': %s", serviceID, customFieldVars.passVar, maskPassword(pass))
		}
	}

	// Si no encontramos con metadata_field_list, intentamos con nombres conocidos como fallback
	if vlan == "" || user == "" || pass == "" {
		possibleVars := []struct {
			vlanVar string
			userVar string
			passVar string
		}{
			{"vlan_id", "username", "password"}, // Los más comunes según las imágenes
			{"VLAN_ID", "username", "password"},
			{"vlan", "username", "password"},
			{"VLAN", "username", "password"},
		}

		for _, vars := range possibleVars {
			if vlan == "" {
				vlanVal := u.getCustomFieldValue(vars.vlanVar, "pack", serviceID)
				if vlanVal != "" {
					vlan = vlanVal
					log.Printf("[DEBUG Ubersmith] CID %s - ✅ VLAN encontrado (fallback) en custom field '%s': %s", serviceID, vars.vlanVar, vlan)
				}
			}
			if user == "" {
				userVal := u.getCustomFieldValue(vars.userVar, "pack", serviceID)
				if userVal != "" {
					user = userVal
					log.Printf("[DEBUG Ubersmith] CID %s - ✅ PPPoE User encontrado (fallback) en custom field '%s': %s", serviceID, vars.userVar, user)
				}
			}
			if pass == "" {
				passVal := u.getCustomFieldValue(vars.passVar, "pack", serviceID)
				if passVal != "" {
					pass = passVal
					log.Printf("[DEBUG Ubersmith] CID %s - ✅ PPPoE Pass encontrado (fallback) en custom field '%s': %s", serviceID, vars.passVar, maskPassword(pass))
				}
			}
		}
	}

	return vlan, user, pass, nil
}

// customFieldVars almacena los nombres de las variables de custom fields encontradas
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
		log.Printf("[DEBUG Ubersmith] Error obteniendo lista de custom fields: %v", err)
		return vars
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[DEBUG Ubersmith] Error leyendo respuesta de custom fields: %v", err)
		return vars
	}

	var result map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		log.Printf("[DEBUG Ubersmith] Error parseando JSON de custom fields: %v", err)
		return vars
	}

	if status, ok := result["status"].(bool); !ok || !status {
		log.Printf("[DEBUG Ubersmith] Status false al obtener lista de custom fields")
		return vars
	}

	// La respuesta tiene formato: {"data": {"44": {"variable": "hide_address_in_whois", ...}}}
	if data, ok := result["data"].(map[string]interface{}); ok {
		for cfID, cfData := range data {
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
							log.Printf("[DEBUG Ubersmith] ✅ Variable de VLAN encontrada: '%s' (ID: %s, Label: %s)",
								variable, cfID, cfObj["label"])
						}
					}

					// Buscar PPPoE User
					if vars.userVar == "" {
						if (strings.Contains(variableLower, "pppoe") || strings.Contains(variableLower, "pppo") ||
							strings.Contains(variableLower, "username") || strings.Contains(variableLower, "user")) &&
							!strings.Contains(variableLower, "pass") && !strings.Contains(variableLower, "password") {
							vars.userVar = variable
							log.Printf("[DEBUG Ubersmith] ✅ Variable de PPPoE User encontrada: '%s' (ID: %s, Label: %s)",
								variable, cfID, cfObj["label"])
						}
					}

					// Buscar PPPoE Pass
					if vars.passVar == "" {
						if (strings.Contains(variableLower, "pppoe") || strings.Contains(variableLower, "pppo") ||
							strings.Contains(variableLower, "password") || strings.Contains(variableLower, "pass")) &&
							!strings.Contains(variableLower, "user") && !strings.Contains(variableLower, "username") {
							vars.passVar = variable
							log.Printf("[DEBUG Ubersmith] ✅ Variable de PPPoE Pass encontrada: '%s' (ID: %s, Label: %s)",
								variable, cfID, cfObj["label"])
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

	// La respuesta tiene formato: {"data": {"1092": "valor", "1039": "valor"}}
	// Las claves pueden ser strings o números, así que buscamos ambos formatos
	if data, ok := result["data"].(map[string]interface{}); ok {
		// Buscamos el valor para nuestro serviceID (como string)
		if val, ok := data[serviceID]; ok {
			if valStr, ok := val.(string); ok {
				log.Printf("[DEBUG Ubersmith] CID %s - Valor obtenido para variable '%s': '%s'", serviceID, variable, valStr)
				return valStr
			} else {
				log.Printf("[DEBUG Ubersmith] CID %s - Valor para variable '%s' no es string: %T = %v", serviceID, variable, val, val)
			}
		}

		// También intentamos buscar como número (por si la clave es numérica)
		serviceIDNum, err := strconv.Atoi(serviceID)
		if err == nil {
			serviceIDStr := fmt.Sprintf("%d", serviceIDNum)
			if val, ok := data[serviceIDStr]; ok {
				if valStr, ok := val.(string); ok {
					log.Printf("[DEBUG Ubersmith] CID %s - Valor obtenido (numérico) para variable '%s': '%s'", serviceID, variable, valStr)
					return valStr
				}
			}
		}

		// Si no encontramos, puede que el servicio no tenga valor para este custom field
		log.Printf("[DEBUG Ubersmith] CID %s - ServiceID '%s' no encontrado en respuesta de metadata_bulk_get para variable '%s'", serviceID, serviceID, variable)
		// Mostramos las claves disponibles para debugging (solo las primeras 10)
		keys := make([]string, 0, len(data))
		for k := range data {
			keys = append(keys, k)
		}
		if len(keys) > 0 {
			log.Printf("[DEBUG Ubersmith] CID %s - ServiceIDs disponibles en respuesta: %v", serviceID, keys[:min(10, len(keys))])
		}
	} else {
		log.Printf("[DEBUG Ubersmith] CID %s - Respuesta de metadata_bulk_get no tiene formato esperado para variable '%s'", serviceID, variable)
	}

	return ""
}

// min retorna el mínimo de dos enteros
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// getCustomFieldNames obtiene un mapa de custom field IDs a sus nombres
func (u *UbersmithAdapter) getCustomFieldNames() map[string]string {
	cfMap := make(map[string]string)

	methods := []string{
		"client.custom_field_list",
		"custom_field.list",
		"client.custom_field_get",
	}

	for _, method := range methods {
		url := fmt.Sprintf("%s?method=%s", u.baseURL, method)

		req, _ := http.NewRequest("GET", url, nil)
		req.SetBasicAuth(u.user, u.pass)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			continue
		}

		var result map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			continue
		}

		if status, ok := result["status"].(bool); !ok || !status {
			continue
		}

		// La respuesta puede ser un mapa o un array
		if data, ok := result["data"].(map[string]interface{}); ok {
			for cfID, cfData := range data {
				if cfObj, ok := cfData.(map[string]interface{}); ok {
					if name, ok := cfObj["name"].(string); ok {
						cfMap[cfID] = name
					} else if label, ok := cfObj["label"].(string); ok {
						cfMap[cfID] = label
					}
				}
			}
		} else if dataArray, ok := result["data"].([]interface{}); ok {
			for _, item := range dataArray {
				if cfObj, ok := item.(map[string]interface{}); ok {
					cfID := ""
					cfName := ""
					if id, ok := cfObj["id"].(string); ok {
						cfID = id
					} else if id, ok := cfObj["id"].(float64); ok {
						cfID = fmt.Sprintf("%.0f", id)
					}
					if name, ok := cfObj["name"].(string); ok {
						cfName = name
					} else if label, ok := cfObj["label"].(string); ok {
						cfName = label
					}
					if cfID != "" && cfName != "" {
						cfMap[cfID] = cfName
					}
				}
			}
		}

		if len(cfMap) > 0 {
			log.Printf("[DEBUG Ubersmith] Obtenidos %d custom field names", len(cfMap))
			break
		}
	}

	return cfMap
}

// getCustomFields intenta obtener los custom fields del servicio (método legacy)
func (u *UbersmithAdapter) getCustomFields(serviceID string) (vlan, user, pass string, err error) {
	// Intentamos obtener los custom fields usando diferentes métodos
	methods := []string{
		"client.custom_field_get",
		"custom_field.get",
		"client.service_get", // Con parámetro para incluir custom fields
	}

	for _, method := range methods {
		url := fmt.Sprintf("%s?method=%s&service_id=%s&include_custom_fields=1", u.baseURL, method, serviceID)

		req, _ := http.NewRequest("GET", url, nil)
		req.SetBasicAuth(u.user, u.pass)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			continue
		}

		var result map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			continue
		}

		if status, ok := result["status"].(bool); !ok || !status {
			continue
		}

		// Buscamos en la respuesta los custom fields
		if data, ok := result["data"].(map[string]interface{}); ok {
			// Buscamos campos que puedan contener VLAN, PPPoE User, PPPoE Pass
			for key, val := range data {
				keyLower := strings.ToLower(key)
				valStr := ""

				if str, ok := val.(string); ok {
					valStr = str
				} else if num, ok := val.(float64); ok {
					valStr = fmt.Sprintf("%.0f", num)
				}

				if valStr != "" {
					if strings.Contains(keyLower, "vlan") && vlan == "" {
						vlan = valStr
					}
					if (strings.Contains(keyLower, "pppoe") || strings.Contains(keyLower, "pppo")) &&
						(strings.Contains(keyLower, "user") || strings.Contains(keyLower, "username")) &&
						user == "" {
						user = valStr
					}
					if (strings.Contains(keyLower, "pppoe") || strings.Contains(keyLower, "pppo")) &&
						(strings.Contains(keyLower, "pass") || strings.Contains(keyLower, "password")) &&
						pass == "" {
						pass = valStr
					}
				}
			}

			if vlan != "" || user != "" || pass != "" {
				return vlan, user, pass, nil
			}
		}
	}

	return "", "", "", fmt.Errorf("no se pudieron obtener custom fields")
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
