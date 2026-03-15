package ocr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	yandexVisionEndpoint = "https://vision.api.cloud.yandex.net/vision/v1/batchAnalyze"
	yandexDefaultTimeout = 30 * time.Second
)

// YandexVision клиент для Yandex Vision API
type YandexVision struct {
	apiKey     string
	httpClient *http.Client
	folderID   string
}

// NewYandexVision создает новый клиент Yandex Vision
func NewYandexVision(apiKey, folderID string) *YandexVision {
	return &YandexVision{
		apiKey:   apiKey,
		folderID: folderID,
		httpClient: &http.Client{
			Timeout: yandexDefaultTimeout,
		},
	}
}

func (y *YandexVision) Name() string {
	return "yandex-vision"
}

// Recognize отправляет изображение в Yandex Vision API
func (y *YandexVision) Recognize(ctx context.Context, image []byte) (*Result, error) {
	reqBody := yandexRequest{
		FolderID: y.folderID,
		AnalyzeSpecs: []analyzeSpec{
			{
				Content: image,
				Features: []feature{
					{
						Type: "TEXT_DETECTION",
						TextDetectionConfig: &textDetectionConfig{
							LanguageCodes: []string{"ru"},
						},
					},
				},
			},
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, &ProviderError{
			Provider: y.Name(),
			Type:     ErrorTypeInvalid,
			Message:  "failed to marshal request",
			Cause:    err,
		}
	}

	req, err := http.NewRequestWithContext(ctx, "POST", yandexVisionEndpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, &ProviderError{
			Provider: y.Name(),
			Type:     ErrorTypeInvalid,
			Message:  "failed to create request",
			Cause:    err,
		}
	}

	req.Header.Set("Authorization", "Api-Key "+y.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := y.httpClient.Do(req)
	if err != nil {
		return nil, &ProviderError{
			Provider: y.Name(),
			Type:     ErrorTypeNetwork,
			Message:  "request failed",
			Cause:    err,
		}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &ProviderError{
			Provider: y.Name(),
			Type:     ErrorTypeNetwork,
			Message:  "failed to read response body",
			Cause:    err,
		}
	}

	// Проверяем HTTP статус
	if resp.StatusCode >= 500 {
		return nil, &ProviderError{
			Provider: y.Name(),
			Type:     ErrorTypeAPI,
			Message:  fmt.Sprintf("server error: %d", resp.StatusCode),
		}
	}

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, &ProviderError{
			Provider: y.Name(),
			Type:     ErrorTypeAuth,
			Message:  "authentication failed",
		}
	}

	if resp.StatusCode == 429 {
		return nil, &ProviderError{
			Provider: y.Name(),
			Type:     ErrorTypeRateLimit,
			Message:  "rate limit exceeded",
		}
	}

	if resp.StatusCode >= 400 {
		return nil, &ProviderError{
			Provider: y.Name(),
			Type:     ErrorTypeInvalid,
			Message:  fmt.Sprintf("client error: %d, body: %s", resp.StatusCode, string(body)),
		}
	}

	var yandexResp yandexResponse
	if err := json.Unmarshal(body, &yandexResp); err != nil {
		return nil, &ProviderError{
			Provider: y.Name(),
			Type:     ErrorTypeUnknown,
			Message:  "failed to parse response",
			Cause:    err,
		}
	}

	// Конвертируем ответ Yandex Vision в наш формат
	return y.convertResponse(&yandexResp), nil
}

// convertResponse конвертирует ответ Yandex Vision в наш формат Result
func (y *YandexVision) convertResponse(resp *yandexResponse) *Result {
	result := &Result{
		Fields: make(map[string]Field),
	}

	if len(resp.Results) == 0 {
		return result
	}

	// Извлекаем текст из результатов
	for _, r := range resp.Results {
		if r.Error != nil {
			continue
		}
		for _, resultItem := range r.Results {
			if resultItem.TextDetection != nil {
				result.RawText = resultItem.TextDetection.FullText
				// TODO: извлечь поля из блоков текста
			}
		}
	}

	return result
}

// Структуры для Yandex Vision API

type yandexRequest struct {
	FolderID     string         `json:"folderId"`
	AnalyzeSpecs []analyzeSpec  `json:"analyzeSpecs"`
}

type analyzeSpec struct {
	Content  []byte    `json:"content"`
	Features []feature `json:"features"`
}

type feature struct {
	Type                string               `json:"type"`
	TextDetectionConfig *textDetectionConfig `json:"textDetectionConfig,omitempty"`
}

type textDetectionConfig struct {
	LanguageCodes []string `json:"languageCodes"`
}

type yandexResponse struct {
	Results []resultItem `json:"results"`
}

type resultItem struct {
	Error   *yandexError    `json:"error,omitempty"`
	Results []detectionResult `json:"results"`
}

type yandexError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type detectionResult struct {
	TextDetection *textDetection `json:"textDetection,omitempty"`
}

type textDetection struct {
	FullText string `json:"fullText"`
	// TODO: blocks, lines, words для извлечения полей
}
