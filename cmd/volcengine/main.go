// Package main provides a command-line tool to fetch models from Volcengine
// Ark (Coding Plan) and generate a configuration file for the provider.
package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"charm.land/catwalk/pkg/catwalk"
)

const (
	host    = "open.volcengineapi.com"
	region  = "cn-beijing"
	service = "ark"
	action  = "ListArkCodingPlanModel"
	version = "2024-01-01"
)

// ListArkCodingPlanModelResponse represents the response of the
// ListArkCodingPlanModel API.
type ListArkCodingPlanModelResponse struct {
	ResponseMetadata struct {
		RequestID string `json:"RequestId"`
		Error     *struct {
			Code    string `json:"Code"`
			Message string `json:"Message"`
		} `json:"Error,omitempty"`
	} `json:"ResponseMetadata"`
	Result struct {
		Datas []struct {
			ModelID string `json:"ModelID"`
		} `json:"Datas"`
	} `json:"Result"`
}

func hmacSHA256(key []byte, content string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(content))
	return mac.Sum(nil)
}

func hashSHA256(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// signedRequest builds a Volcengine V4 signed request for the given action.
func signedRequest(ctx context.Context, accessKey, secretKey, securityToken string, body []byte) (*http.Request, error) {
	now := time.Now().UTC()
	xDate := now.Format("20060102T150405Z")
	shortDate := xDate[:8]
	contentType := "application/json; charset=utf-8"
	payloadHash := hashSHA256(body)

	query := url.Values{}
	query.Set("Action", action)
	query.Set("Version", version)
	canonicalQuery := query.Encode()

	// Canonical headers must be sorted by header name.
	headers := map[string]string{
		"content-type":     contentType,
		"host":             host,
		"x-content-sha256": payloadHash,
		"x-date":           xDate,
	}
	if securityToken != "" {
		headers["x-security-token"] = securityToken
	}
	signedHeaderNames := make([]string, 0, len(headers))
	for name := range headers {
		signedHeaderNames = append(signedHeaderNames, name)
	}
	sort.Strings(signedHeaderNames)

	var canonicalHeaders strings.Builder
	for _, name := range signedHeaderNames {
		canonicalHeaders.WriteString(name)
		canonicalHeaders.WriteString(":")
		canonicalHeaders.WriteString(headers[name])
		canonicalHeaders.WriteString("\n")
	}
	signedHeaders := strings.Join(signedHeaderNames, ";")

	canonicalRequest := strings.Join([]string{
		http.MethodPost,
		"/",
		canonicalQuery,
		canonicalHeaders.String(),
		signedHeaders,
		payloadHash,
	}, "\n")

	credentialScope := strings.Join([]string{shortDate, region, service, "request"}, "/")
	stringToSign := strings.Join([]string{
		"HMAC-SHA256",
		xDate,
		credentialScope,
		hashSHA256([]byte(canonicalRequest)),
	}, "\n")

	kDate := hmacSHA256([]byte(secretKey), shortDate)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	kSigning := hmacSHA256(kService, "request")
	signature := hex.EncodeToString(hmacSHA256(kSigning, stringToSign))

	authorization := fmt.Sprintf(
		"HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		accessKey, credentialScope, signedHeaders, signature,
	)

	endpoint := "https://" + host + "/?" + canonicalQuery
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err //nolint:wrapcheck
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Host", host)
	req.Header.Set("X-Content-Sha256", payloadHash)
	req.Header.Set("X-Date", xDate)
	req.Header.Set("Authorization", authorization)
	if securityToken != "" {
		req.Header.Set("X-Security-Token", securityToken)
	}
	return req, nil
}

func fetchVolcengineModels() (*ListArkCodingPlanModelResponse, error) {
	accessKey := strings.TrimSpace(os.Getenv("VOLCENGINE_ACCESS_KEY"))
	secretKey := strings.TrimSpace(os.Getenv("VOLCENGINE_SECRET_KEY"))
	if accessKey == "" || secretKey == "" {
		return nil, fmt.Errorf("VOLCENGINE_ACCESS_KEY and VOLCENGINE_SECRET_KEY are required")
	}
	securityToken := strings.TrimSpace(os.Getenv("VOLCENGINE_SESSION_TOKEN"))

	body := []byte("{}")
	req, err := signedRequest(context.Background(), accessKey, secretKey, securityToken, body)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err //nolint:wrapcheck
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("unable to read response body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, respBody)
	}

	var mr ListArkCodingPlanModelResponse
	if err := json.Unmarshal(respBody, &mr); err != nil {
		return nil, err //nolint:wrapcheck
	}
	if mr.ResponseMetadata.Error != nil {
		return nil, fmt.Errorf("%s: %s", mr.ResponseMetadata.Error.Code, mr.ResponseMetadata.Error.Message)
	}
	return &mr, nil
}

func prettyName(id string) string {
	parts := strings.Split(id, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

func main() {
	modelsResp, err := fetchVolcengineModels()
	if err != nil {
		log.Fatal("Error fetching Volcengine models:", err)
	}

	provider := catwalk.Provider{
		Name:                "Volcengine Ark (Coding Plan)",
		ID:                  catwalk.InferenceProviderVolcengine,
		APIKey:              "$ARK_API_KEY",
		APIEndpoint:         "https://ark.cn-beijing.volces.com/api/v3",
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "ark-code-latest",
		DefaultSmallModelID: "doubao-seed-2.0-lite",
	}

	for _, model := range modelsResp.Result.Datas {
		if model.ModelID == "" {
			continue
		}
		m := catwalk.Model{
			ID:               model.ModelID,
			Name:             prettyName(model.ModelID),
			CostPer1MIn:      0,
			CostPer1MOut:     0,
			ContextWindow:    262144,
			DefaultMaxTokens: 32768,
			CanReason:        true,
			SupportsImages:   false,
		}
		provider.Models = append(provider.Models, m)
	}

	slices.SortFunc(provider.Models, func(a, b catwalk.Model) int {
		return strings.Compare(a.ID, b.ID)
	})

	data, err := json.MarshalIndent(provider, "", "  ")
	if err != nil {
		log.Fatal("Error marshaling Volcengine provider:", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile("internal/providers/configs/volcengine.json", data, 0o600); err != nil {
		log.Fatal("Error writing Volcengine provider config:", err)
	}

	fmt.Printf("Generated volcengine.json with %d models\n", len(provider.Models))
}
