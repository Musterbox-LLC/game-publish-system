// game-publish-system/services/auth_service_client.go
package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

type AuthServiceClient struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

type ValidateResponse struct {
	UserID                string `json:"user_id"`
	DeviceID              string `json:"device_id"`
	OTPNotRequiredForDevice bool `json:"otp_not_required_for_device"`
	Roles                 []string `json:"roles"`
}

func NewAuthServiceClient(baseURL, token string) *AuthServiceClient {
	return &AuthServiceClient{
		BaseURL: baseURL,
		Token:   token,
		Client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// ValidateToken calls /validate on auth service
func (c *AuthServiceClient) ValidateToken(accessToken, deviceID string) (*ValidateResponse, error) {
	url := fmt.Sprintf("%s/auth/validate", c.BaseURL)

	reqBody := map[string]interface{}{
		"access_token": accessToken,
		"device_id":    deviceID,
	}
	jsonData, _ := json.Marshal(reqBody)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token) // Gateway → Auth service token

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		log.Printf("AuthService /validate returned %d: %s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("auth validation failed: %d", resp.StatusCode)
	}

	var out ValidateResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}

	return &out, nil
}