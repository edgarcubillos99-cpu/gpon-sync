package ubersmith

import (
	"encoding/json"
	"fmt"
	"net/http"
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

// GetServiceDetails busca VLAN y PPPoE por CID
func (u *UbersmithAdapter) GetServiceDetails(cid string) (vlan, user, pass string, err error) {
	// 🚧 IMPORTANTE: 'custom_field_123' debe ser el ID real del campo CID en Ubersmith
	method := "uber.service_list"
	url := fmt.Sprintf("%s?method=%s&custom_field_CID=%s", u.baseURL, method, cid)

	req, _ := http.NewRequest("GET", url, nil)
	req.SetBasicAuth(u.user, u.pass)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()

	var result struct {
		Status bool `json:"status"`
		Data   map[string]struct {
			VLAN      string `json:"vlan_field"`     // 🚧 AJUSTAR: Nombre real del campo
			PPPoEUsr  string `json:"pppoe_user"`     // 🚧 AJUSTAR: Nombre real del campo
			PPPoEPass string `json:"pppoe_password"` // 🚧 AJUSTAR: Nombre real del campo
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", "", err
	}

	// Extraemos el primer resultado del mapa de servicios
	for _, srv := range result.Data {
		return srv.VLAN, srv.PPPoEUsr, srv.PPPoEPass, nil
	}

	return "", "", "", fmt.Errorf("servicio no encontrado en Ubersmith para CID: %s", cid)
}
