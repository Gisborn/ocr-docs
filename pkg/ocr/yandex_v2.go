package ocr

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const (
	yandexV2Endpoint        = "https://ocr.api.cloud.yandex.net/ocr/v1/recognizeText"
	yandexV2DefaultTimeout  = 30 * time.Second
	yandexV2DefaultConfidence = 0.90 // confidence для structured fields (модель очень точна)
)

// YandexVisionV2 клиент для Yandex Vision OCR API v2 с поддержкой моделей
type YandexVisionV2 struct {
	apiKey     string
	model      DocumentModel
	httpClient *http.Client
	endpoint   string // для тестирования
}

// NewYandexVisionV2 создает новый клиент Yandex Vision OCR v2
// model — модель распознавания (passport, driver-license-front, page и т.д.)
func NewYandexVisionV2(apiKey string, model DocumentModel) *YandexVisionV2 {
	if !model.IsValid() {
		model = ModelPassportRF
	}
	return &YandexVisionV2{
		apiKey:   apiKey,
		model:    model,
		endpoint: yandexV2Endpoint,
		httpClient: &http.Client{
			Timeout: yandexV2DefaultTimeout,
		},
	}
}

func (y *YandexVisionV2) Name() string {
	return "yandex-vision-v2/" + string(y.model)
}

// Recognize выполняет OCR с двухуровневым fallback:
//  1. Structured model (например, passport) → entities
//  2. Generic model (page) → raw text + ExtractPassportFields
func (y *YandexVisionV2) Recognize(ctx context.Context, image []byte) (*Result, error) {
	// Шаг 1: Пробуем structured модель (если она structured)
	if y.model.IsStructured() {
		result, err := y.recognizeWithModel(ctx, image, y.model)
		if err == nil && y.hasRequiredFields(result) {
			return result, nil
		}
		// Если structured model не дала достаточно полей — fallback на generic
		fieldCount := 0
		if result != nil {
			fieldCount = len(result.Fields)
		}
		fmt.Printf("[YandexVisionV2] Structured model %q returned incomplete fields (%d), falling back to generic\n",
			y.model, fieldCount)
	}

	// Шаг 2: Fallback на generic page model + текстовый парсинг
	result, err := y.recognizeWithModel(ctx, image, ModelPage)
	if err != nil {
		return nil, err
	}

	// Для generic model применяем текстовый парсинг
	if result.RawText != "" {
		parsed := ExtractPassportFields(result.RawText, nil)
		if len(parsed) > 0 {
			result.Fields = parsed
		}
	}

	return result, nil
}

// recognizeWithModel отправляет запрос с указанной моделью
func (y *YandexVisionV2) recognizeWithModel(ctx context.Context, image []byte, model DocumentModel) (*Result, error) {
	mimeType := detectMimeType(image)

	reqBody := yandexV2Request{
		Content:       base64.StdEncoding.EncodeToString(image),
		MIMEType:      mimeType,
		LanguageCodes: []string{"ru"},
		Model:         string(model),
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

	req, err := http.NewRequestWithContext(ctx, "POST", y.endpoint, bytes.NewReader(jsonBody))
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

	// Логирование для отладки
	previewLen := min(len(body), 300)
	fmt.Printf("[YandexVisionV2] model=%s status=%d body=%s...\n", model, resp.StatusCode, string(body)[:previewLen])

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

	var yandexResp yandexV2Response
	if err := json.Unmarshal(body, &yandexResp); err != nil {
		return nil, &ProviderError{
			Provider: y.Name(),
			Type:     ErrorTypeUnknown,
			Message:  "failed to parse response",
			Cause:    err,
		}
	}

	return y.convertResponse(&yandexResp, model)
}

// convertResponse конвертирует ответ v2 API в Result
func (y *YandexVisionV2) convertResponse(resp *yandexV2Response, model DocumentModel) (*Result, error) {
	result := &Result{
		Fields:       make(map[string]Field),
		DocumentType: "passport_rf",
	}

	if resp.Result == nil || resp.Result.TextAnnotation == nil {
		return result, nil
	}

	ta := resp.Result.TextAnnotation

	// Собираем raw text из blocks (для generic fallback)
	var lines []string
	for _, block := range ta.Blocks {
		for _, line := range block.Lines {
			if line.Text != "" {
				lines = append(lines, line.Text)
			}
		}
	}
	result.RawText = strings.Join(lines, "\n")

	// Если structured model и есть entities — извлекаем поля
	if model.IsStructured() && len(ta.Entities) > 0 {
		result.Fields = y.extractStructuredFields(ta.Entities)
	}

	return result, nil
}

// extractStructuredFields маппит entities passport модели на наши поля
func (y *YandexVisionV2) extractStructuredFields(entities []yandexV2Entity) map[string]Field {
	fields := make(map[string]Field)

	for _, entity := range entities {
		name := strings.ToLower(strings.TrimSpace(entity.Name))
		text := strings.TrimSpace(entity.Text)
		if text == "" || text == "-" {
			continue
		}

		conf := yandexV2DefaultConfidence

		switch name {
		case "surname":
			fields["last_name"] = Field{Value: normalizeName(text), Confidence: conf}
		case "name":
			fields["first_name"] = Field{Value: normalizeName(text), Confidence: conf}
		case "middle_name":
			fields["middle_name"] = Field{Value: normalizeName(text), Confidence: conf}
		case "birth_date":
			fields["birth_date"] = Field{Value: normalizeDate(text), Confidence: conf}
		case "issue_date":
			fields["issue_date"] = Field{Value: normalizeDate(text), Confidence: conf}
		case "number":
			// number содержит 10 цифр: серия (4) + номер (6)
			if len(text) == 10 {
				fields["series"] = Field{Value: text[:4], Confidence: conf}
				fields["number"] = Field{Value: text[4:], Confidence: conf}
			} else {
				fields["number"] = Field{Value: text, Confidence: conf}
			}
		case "subdivision":
			code := regexp.MustCompile(`\s*[-–—]\s*`).ReplaceAllString(text, "-")
			fields["division_code"] = Field{Value: code, Confidence: conf}
		case "issued_by":
			fields["issued_by"] = Field{Value: text, Confidence: conf}
		case "birth_place":
			fields["birth_place"] = Field{Value: text, Confidence: conf}
		case "gender":
			fields["gender"] = Field{Value: text, Confidence: conf}
		case "citizenship":
			fields["citizenship"] = Field{Value: text, Confidence: conf}
		}
	}

	return fields
}

// hasRequiredFields проверяет, что результат содержит минимум необходимых полей
func (y *YandexVisionV2) hasRequiredFields(result *Result) bool {
	if result == nil || len(result.Fields) == 0 {
		return false
	}

	// Обязательные поля для паспорта РФ
	required := []string{"last_name", "first_name", "birth_date", "series", "number", "issue_date"}
	optional := []string{"middle_name", "division_code", "issued_by"}

	requiredCount := 0
	for _, key := range required {
		if result.Fields[key].Value != "" {
			requiredCount++
		}
	}

	optionalCount := 0
	for _, key := range optional {
		if result.Fields[key].Value != "" {
			optionalCount++
		}
	}

	// Минимум: все 6 обязательных + хотя бы 1 опциональное
	return requiredCount >= 6 && optionalCount >= 1
}

// detectMimeType определяет MIME-тип по magic bytes
func detectMimeType(data []byte) string {
	if len(data) < 4 {
		return "image/jpeg"
	}
	// PNG: 89 50 4E 47
	if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		return "image/png"
	}
	// JPEG: FF D8 FF
	if data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "image/jpeg"
	}
	// PDF: 25 50 44 46
	if data[0] == 0x25 && data[1] == 0x50 && data[2] == 0x44 && data[3] == 0x46 {
		return "application/pdf"
	}
	return "image/jpeg"
}

// Структуры для Yandex Vision OCR API v2

type yandexV2Request struct {
	Content       string   `json:"content"`
	MIMEType      string   `json:"mimeType"`
	LanguageCodes []string `json:"languageCodes"`
	Model         string   `json:"model"`
}

type yandexV2Response struct {
	Result *yandexV2Result `json:"result"`
}

type yandexV2Result struct {
	TextAnnotation *yandexV2TextAnnotation `json:"textAnnotation"`
	Page           string                  `json:"page"`
}

type yandexV2TextAnnotation struct {
	Width    string            `json:"width"`
	Height   string            `json:"height"`
	Blocks   []yandexV2Block   `json:"blocks"`
	Entities []yandexV2Entity  `json:"entities"`
	FullText string            `json:"fullText"`
	Rotate   string            `json:"rotate"`
}

type yandexV2Entity struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

type yandexV2Block struct {
	BoundingBox *yandexV2BoundingBox `json:"boundingBox"`
	Lines       []yandexV2Line       `json:"lines"`
	Languages   []yandexV2Language   `json:"languages"`
	LayoutType  string               `json:"layoutType"`
}

type yandexV2BoundingBox struct {
	Vertices []yandexV2Vertex `json:"vertices"`
}

type yandexV2Vertex struct {
	X string `json:"x"`
	Y string `json:"y"`
}

type yandexV2Line struct {
	BoundingBox  *yandexV2BoundingBox `json:"boundingBox"`
	Text         string               `json:"text"`
	Words        []yandexV2Word       `json:"words"`
	Orientation  string               `json:"orientation"`
}

type yandexV2Word struct {
	BoundingBox  *yandexV2BoundingBox `json:"boundingBox"`
	Text         string               `json:"text"`
	EntityIndex  string               `json:"entityIndex"`
}

type yandexV2Language struct {
	LanguageCode string `json:"languageCode"`
}
