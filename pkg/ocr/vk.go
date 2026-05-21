package ocr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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
func (v *VKVision) convertResponse(resp *vkResponse) *Result {
	result := &Result{
		Fields:       make(map[string]Field),
		DocumentType: "passport_rf",
	}

	if len(resp.Results) == 0 {
		return result
	}

	// Собираем текст постранично и извлекаем блоки для confidence
	var pageTexts []string
	var allBlocks []textBlock

	for _, r := range resp.Results {
		if r.Error != nil {
			continue
		}
		for _, res := range r.Results {
			if res.TextDetection != nil {
				for _, p := range res.TextDetection.Pages {
					var pageText strings.Builder
					for _, b := range p.Blocks {
						blockText := extractVKBlockText(b)
						if blockText != "" {
							blockConf := extractVKBlockConfidence(b)
							pageText.WriteString(blockText + "\n")
							allBlocks = append(allBlocks, textBlock{
								Text:       blockText,
								Confidence: blockConf,
							})
						}
					}
					pageTexts = append(pageTexts, pageText.String())
				}
			}
		}
	}

	result.RawText = strings.Join(pageTexts, "\n---PAGE---\n")

	// Извлекаем поля паспорта из текста
	fields := ExtractPassportFields(result.RawText, allBlocks)
	result.Fields = fields

	return result
}

// extractVKBlockText извлекает текст из VK блока
func extractVKBlockText(block vkBlock) string {
	var lines []string
	for _, line := range block.Lines {
		var words []string
		for _, word := range line.Words {
			words = append(words, word.Text)
		}
		if len(words) > 0 {
			lines = append(lines, strings.Join(words, " "))
		}
	}
	return strings.Join(lines, " ")
}

// extractVKBlockConfidence вычисляет средний confidence слов в блоке
func extractVKBlockConfidence(block vkBlock) float64 {
	var total float64
	var count int
	for _, line := range block.Lines {
		for _, word := range line.Words {
			total += word.Confidence
			count++
		}
	}
	if count == 0 {
		return 0.8
	}
	return total / float64(count)
}

// Структуры для VK Vision API (идентичны Yandex Vision v1)
type vkRequest struct {
	FolderID     string          `json:"folderId"`
	AnalyzeSpecs []vkAnalyzeSpec `json:"analyzeSpecs"`
}

type vkAnalyzeSpec struct {
	Content  []byte      `json:"content"`
	Features []vkFeature `json:"features"`
}

type vkFeature struct {
	Type                string                 `json:"type"`
	TextDetectionConfig *vkTextDetectionConfig `json:"textDetectionConfig,omitempty"`
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
