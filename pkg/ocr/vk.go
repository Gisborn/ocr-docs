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
	vkVisionEndpoint = "https://vision.api.cloud.vk.com/vision/v1/batchAnalyze"
	vkDefaultTimeout = 30 * time.Second
)

// VKVision клиент для VK Vision API
type VKVision struct {
	apiKey     string
	httpClient *http.Client
	folderID   string
}

// NewVKVision создает новый клиент VK Vision
func NewVKVision(apiKey, folderID string) *VKVision {
	return &VKVision{
		apiKey:   apiKey,
		folderID: folderID,
		httpClient: &http.Client{
			Timeout: vkDefaultTimeout,
		},
	}
}

func (v *VKVision) Name() string {
	return "vk-vision"
}

// Recognize отправляет изображение в VK Vision API
func (v *VKVision) Recognize(ctx context.Context, image []byte) (*Result, error) {
	reqBody := vkRequest{
		FolderID: v.folderID,
		AnalyzeSpecs: []vkAnalyzeSpec{
			{
				Content: image,
				Features: []vkFeature{
					{
						Type: "TEXT_DETECTION",
						TextDetectionConfig: &vkTextDetectionConfig{
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
			Provider: v.Name(),
			Type:     ErrorTypeInvalid,
			Message:  "failed to marshal request",
			Cause:    err,
		}
	}

	req, err := http.NewRequestWithContext(ctx, "POST", vkVisionEndpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, &ProviderError{
			Provider: v.Name(),
			Type:     ErrorTypeInvalid,
			Message:  "failed to create request",
			Cause:    err,
		}
	}

	req.Header.Set("Authorization", "Api-Key "+v.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return nil, &ProviderError{
			Provider: v.Name(),
			Type:     ErrorTypeNetwork,
			Message:  "request failed",
			Cause:    err,
		}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &ProviderError{
			Provider: v.Name(),
			Type:     ErrorTypeNetwork,
			Message:  "failed to read response body",
			Cause:    err,
		}
	}

	// Проверяем HTTP статус
	if resp.StatusCode >= 500 {
		return nil, &ProviderError{
			Provider: v.Name(),
			Type:     ErrorTypeAPI,
			Message:  fmt.Sprintf("server error: %d", resp.StatusCode),
		}
	}

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, &ProviderError{
			Provider: v.Name(),
			Type:     ErrorTypeAuth,
			Message:  "authentication failed",
		}
	}

	if resp.StatusCode == 429 {
		return nil, &ProviderError{
			Provider: v.Name(),
			Type:     ErrorTypeRateLimit,
			Message:  "rate limit exceeded",
		}
	}

	if resp.StatusCode >= 400 {
		return nil, &ProviderError{
			Provider: v.Name(),
			Type:     ErrorTypeInvalid,
			Message:  fmt.Sprintf("client error: %d, body: %s", resp.StatusCode, string(body)),
		}
	}

	var vkResp vkResponse
	if err := json.Unmarshal(body, &vkResp); err != nil {
		return nil, &ProviderError{
			Provider: v.Name(),
			Type:     ErrorTypeUnknown,
			Message:  "failed to parse response",
			Cause:    err,
		}
	}

	// Конвертируем ответ VK Vision в наш формат
	return v.convertResponse(&vkResp), nil
}

// convertResponse конвертирует ответ VK Vision в наш формат Result
// VK Vision API имеет ту же структуру, что и Yandex Vision
func (v *VKVision) convertResponse(resp *vkResponse) *Result {
	result := &Result{
		Fields:       make(map[string]Field),
		DocumentType: "passport_rf",
	}

	if len(resp.Results) == 0 {
		return result
	}

	// Используем ту же логику парсинга, что и для Yandex
	// Т.к. API идентичны, конвертируем структуры
	yandexResp := yandexResponse{
		Results: make([]resultItem, len(resp.Results)),
	}

	for i, r := range resp.Results {
		if r.Error != nil {
			yandexResp.Results[i].Error = &yandexError{
				Code:    r.Error.Code,
				Message: r.Error.Message,
			}
		}

		for _, res := range r.Results {
			if res.TextDetection != nil {
				td := &textDetection{
					Pages: make([]page, len(res.TextDetection.Pages)),
				}
				for j, p := range res.TextDetection.Pages {
					td.Pages[j].Blocks = make([]block, len(p.Blocks))
					for k, b := range p.Blocks {
						td.Pages[j].Blocks[k] = block{
							Confidence: b.Confidence,
							Lines:      make([]line, len(b.Lines)),
						}
						for l, ln := range b.Lines {
							td.Pages[j].Blocks[k].Lines[l].Words = make([]word, len(ln.Words))
							for m, w := range ln.Words {
								td.Pages[j].Blocks[k].Lines[l].Words[m] = word{
									Text:       w.Text,
									Confidence: w.Confidence,
								}
							}
						}
					}
				}
				yandexResp.Results[i].Results = append(yandexResp.Results[i].Results, detectionResult{
					TextDetection: td,
				})
			}
		}
	}

	// Переиспользуем логику извлечения полей из Yandex
	yandex := &YandexVision{}
	converted := yandex.convertResponse(&yandexResp)
	return &Result{
		RawText:      converted.RawText,
		Fields:       converted.Fields,
		DocumentType: converted.DocumentType,
	}
}

// Структуры для VK Vision API (идентичны Yandex Vision)
type vkRequest struct {
	FolderID     string          `json:"folderId"`
	AnalyzeSpecs []vkAnalyzeSpec `json:"analyzeSpecs"`
}

type vkAnalyzeSpec struct {
	Content  []byte       `json:"content"`
	Features []vkFeature  `json:"features"`
}

type vkFeature struct {
	Type                string                  `json:"type"`
	TextDetectionConfig *vkTextDetectionConfig  `json:"textDetectionConfig,omitempty"`
}

type vkTextDetectionConfig struct {
	LanguageCodes []string `json:"languageCodes"`
}

type vkResponse struct {
	Results []vkResultItem `json:"results"`
}

type vkResultItem struct {
	Error   *vkError            `json:"error,omitempty"`
	Results []vkDetectionResult `json:"results"`
}

type vkError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type vkDetectionResult struct {
	TextDetection *vkTextDetection `json:"textDetection,omitempty"`
}

type vkTextDetection struct {
	Pages []vkPage `json:"pages"`
}

type vkPage struct {
	Blocks []vkBlock `json:"blocks"`
}

type vkBlock struct {
	Confidence float64  `json:"confidence"`
	Lines      []vkLine `json:"lines"`
}

type vkLine struct {
	Words []vkWord `json:"words"`
}

type vkWord struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
}
