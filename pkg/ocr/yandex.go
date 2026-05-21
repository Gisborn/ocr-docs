package ocr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	yandexVisionEndpoint = "https://vision.api.cloud.yandex.net/vision/v1/batchAnalyze"
	yandexDefaultTimeout = 30 * time.Second
)

// YandexVision клиент для Yandex Vision API (legacy v1)
type YandexVision struct {
	apiKey     string
	httpClient *http.Client
	folderID   string
}

// NewYandexVision создает новый клиент Yandex Vision (legacy v1)
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

	// Временное логирование для отладки
	fmt.Printf("[YandexVision] status=%d body=%s\n", resp.StatusCode, string(body)[:min(len(body), 500)])

	if resp.StatusCode >= 400 {
		fmt.Printf("[YandexVision] ERROR: status=%d body=%s\n", resp.StatusCode, string(body))
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
		Fields:       make(map[string]Field),
		DocumentType: "passport_rf",
	}

	if len(resp.Results) == 0 {
		return result
	}

	// Собираем текст постранично, сортируем блоки по Y-координате
	var pageTexts []string
	var allBlocks []textBlock

	for _, r := range resp.Results {
		if r.Error != nil {
			continue
		}
		for _, resultItem := range r.Results {
			if resultItem.TextDetection != nil {
				for _, page := range resultItem.TextDetection.Pages {
					pageBlocks := make([]blockWithPos, 0, len(page.Blocks))
					for _, b := range page.Blocks {
						blockText := extractBlockText(b)
						if blockText != "" {
							blockConf := extractBlockConfidence(b)
							pageBlocks = append(pageBlocks, blockWithPos{
								textBlock: textBlock{Text: blockText, Confidence: blockConf},
								y:         blockTopY(b),
							})
						}
					}
					// Сортируем блоки сверху вниз
					sortBlocksByY(pageBlocks)
					var pageText strings.Builder
					for _, b := range pageBlocks {
						pageText.WriteString(b.Text + "\n")
						allBlocks = append(allBlocks, b.textBlock)
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

// blockWithPos блок с Y-координатой для сортировки
type blockWithPos struct {
	textBlock
	y int
}

// blockTopY возвращает верхнюю Y-координату блока
func blockTopY(b block) int {
	if b.BoundingBox != nil && len(b.BoundingBox.Vertices) > 0 {
		if y, err := strconv.Atoi(b.BoundingBox.Vertices[0].Y); err == nil {
			return y
		}
	}
	return 0
}

// sortBlocksByY сортирует блоки сверху вниз
func sortBlocksByY(blocks []blockWithPos) {
	for i := 0; i < len(blocks); i++ {
		for j := i + 1; j < len(blocks); j++ {
			if blocks[j].y < blocks[i].y {
				blocks[i], blocks[j] = blocks[j], blocks[i]
			}
		}
	}
}

// extractBlockText извлекает текст из блока
func extractBlockText(block block) string {
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

// extractBlockConfidence вычисляет средний confidence слов в блоке
func extractBlockConfidence(block block) float64 {
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

// Структуры для Yandex Vision API v1

type yandexRequest struct {
	FolderID     string        `json:"folderId"`
	AnalyzeSpecs []analyzeSpec `json:"analyzeSpecs"`
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
	Error   *yandexError      `json:"error,omitempty"`
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
	Pages []page `json:"pages"`
}

type page struct {
	Blocks []block `json:"blocks"`
}

type block struct {
	Confidence  float64      `json:"confidence"`
	Lines       []line       `json:"lines"`
	BoundingBox *boundingBox `json:"boundingBox,omitempty"`
}

type boundingBox struct {
	Vertices []vertex `json:"vertices"`
}

type vertex struct {
	X string `json:"x"`
	Y string `json:"y"`
}

type line struct {
	Words []word `json:"words"`
}

type word struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
}
