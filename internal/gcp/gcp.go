package gcp

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type Credentials struct {
	ClientEmail  string `json:"client_email"   mapstructure:"clientEmail"   structs:"ClientEmail"`
	ClientID     string `json:"client_id"      mapstructure:"clientID"      structs:"ClientID"`
	PrivateKeyID string `json:"private_key_id" mapstructure:"privateKeyID" structs:"PrivateKeyID"`
	PrivateKey   string `json:"private_key"    mapstructure:"privateKey"    structs:"PrivateKey"`
	ProjectID    string `json:"project_id"     mapstructure:"projectID"     structs:"ProjectID"`
	Type         string `json:"type"           mapstructure:"type"           structs:"Type"`
}

func GetCredentials() ([]byte, error) {
	projectID := os.Getenv("GCP_PROJECT_ID")
	privateKey := strings.Join(strings.Split(os.Getenv("GCP_PRIVATE_KEY"), "\\n"), "\n")

	credentials := &Credentials{
		ClientEmail:  os.Getenv("GCP_CLIENT_EMAIL"),
		ClientID:     os.Getenv("GCP_CLIENT_ID"),
		PrivateKeyID: os.Getenv("GCP_PRIVATE_KEY_ID"),
		PrivateKey:   privateKey,
		ProjectID:    projectID,
		Type:         "service_account",
	}

	data, err := json.Marshal(credentials)
	if err != nil {
		return nil, fmt.Errorf("error marshalling json %w", err)
	}

	return data, nil
}
