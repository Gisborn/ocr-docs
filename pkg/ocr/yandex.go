package ocr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
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
		Fields:       make(map[string]Field),
		DocumentType: "passport_rf",
	}

	if len(resp.Results) == 0 {
		return result
	}

	// Собираем весь текст
	var allText strings.Builder
	var blocks []textBlock

	for _, r := range resp.Results {
		if r.Error != nil {
			continue
		}
		for _, resultItem := range r.Results {
			if resultItem.TextDetection != nil {
				for _, page := range resultItem.TextDetection.Pages {
					for _, block := range page.Blocks {
						blockText := extractBlockText(block)
						if blockText != "" {
							blocks = append(blocks, textBlock{
								Text:       blockText,
								Confidence: block.Confidence,
							})
							allText.WriteString(blockText + "\n")
						}
					}
				}
			}
		}
	}

	result.RawText = allText.String()

	// Извлекаем поля паспорта из текста
	fields := extractPassportFields(result.RawText, blocks)
	result.Fields = fields

	return result
}

// textBlock представляет блок текста с confidence
type textBlock struct {
	Text       string
	Confidence float64
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

// extractPassportFields извлекает поля паспорта из текста
func extractPassportFields(text string, blocks []textBlock) map[string]Field {
	fields := make(map[string]Field)

	lines := strings.Split(text, "\n")

	// ФИО - ищем паттерны
	// Фамилия обычно перед именем, часто на отдельной строке
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Ищем серию и номер паспорта (4 цифры пробел 6 цифр)
		if matches := regexp.MustCompile(`\b(\d{2})\s*(\d{2})\s+(\d{6})\b`).FindStringSubmatch(line); matches != nil {
			fields["series"] = Field{
				Value:      matches[1] + matches[2],
				Confidence: getConfidenceForLine(line, blocks),
			}
			fields["number"] = Field{
				Value:      matches[3],
				Confidence: getConfidenceForLine(line, blocks),
			}
		}

		// Ищем дату рождения (ДД.ММ.ГГГГ)
		if matches := regexp.MustCompile(`(\d{2})[.\s]*(\d{2})[.\s]*(\d{4})`).FindStringSubmatch(line); matches != nil {
			date := fmt.Sprintf("%s.%s.%s", matches[1], matches[2], matches[3])
			// Проверяем что это не дата выдачи (обычно позже в паспорте)
			if i < len(lines)/2 {
				fields["birth_date"] = Field{
					Value:      date,
					Confidence: getConfidenceForLine(line, blocks),
				}
			} else {
				fields["issue_date"] = Field{
					Value:      date,
					Confidence: getConfidenceForLine(line, blocks),
				}
			}
		}

		// Ищем код подразделения (XXX-XXX)
		if matches := regexp.MustCompile(`(\d{3})\s*[-]?\s*(\d{3})`).FindStringSubmatch(line); matches != nil {
			code := fmt.Sprintf("%s-%s", matches[1], matches[2])
			// Проверяем что это похоже на код подразделения (обычно после даты выдачи)
			if i > len(lines)/2 {
				fields["division_code"] = Field{
					Value:      code,
					Confidence: getConfidenceForLine(line, blocks),
				}
			}
		}
	}

	// Ищем ФИО - обычно в начале документа
	// Упрощенная эвристика: берем первые 3 строки с заглавными буквами
	nameLines := []string{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Проверяем что строка состоит из букв и заглавная
		if regexp.MustCompile(`^[А-ЯЁ][А-ЯЁ\s]+$`).MatchString(line) && len(line) > 2 {
			nameLines = append(nameLines, line)
		}
		if len(nameLines) >= 3 {
			break
		}
	}

	if len(nameLines) >= 1 {
		fields["last_name"] = Field{
			Value:      nameLines[0],
			Confidence: 0.85,
		}
	}
	if len(nameLines) >= 2 {
		fields["first_name"] = Field{
			Value:      nameLines[1],
			Confidence: 0.85,
		}
	}
	if len(nameLines) >= 3 {
		fields["middle_name"] = Field{
			Value:      nameLines[2],
			Confidence: 0.85,
		}
	}

	return fields
}

// getConfidenceForLine возвращает confidence для строки
func getConfidenceForLine(line string, blocks []textBlock) float64 {
	for _, block := range blocks {
		if strings.Contains(block.Text, strings.TrimSpace(line)) {
			return block.Confidence
		}
	}
	return 0.8 // дефолтное значение
}

// Структуры для Yandex Vision API

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
	Confidence float64 `json:"confidence"`
	Lines      []line  `json:"lines"`
}

type line struct {
	Words []word `json:"words"`
}

type word struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
}
