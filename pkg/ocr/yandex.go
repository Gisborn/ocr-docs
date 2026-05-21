package ocr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

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
	fields := extractPassportFields(result.RawText, allBlocks)
	result.Fields = fields

	return result
}

// textBlock представляет блок текста с confidence
type textBlock struct {
	Text       string
	Confidence float64
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

// isLabelLike проверяет, что строка похожа на лейбл поля паспорта
func isLabelLike(s string) bool {
	upper := strings.ToUpper(s)
	labels := []string{"ФАМИЛИЯ", "ИМЯ", "ОТЧЕСТВО", "ДАТА РОЖДЕНИЯ", "МЕСТО РОЖДЕНИЯ", "ПОЛ", "ПАСПОРТ", "ВЫДАН"}
	for _, l := range labels {
		if strings.Contains(upper, l) {
			return true
		}
	}
	return false
}

// extractPassportFields извлекает поля паспорта из текста
func extractPassportFields(text string, blocks []textBlock) map[string]Field {
	fields := make(map[string]Field)

	pages := strings.Split(text, "\n---PAGE---\n")

	// Stop-слова для ФИО (exact match)
	stopWords := []string{
		"ПАСПОРТ", "РОССИЙСКОЙ", "ФЕДЕРАЦИИ", "ОТДЕЛ", "УФМС", "МВД",
		"РОССИИ", "ГОРОДА", "ОБЛАСТИ", "ОКРУГА", "РАЙОНА", "КРАЯ",
		"МЕЖРАЙОНН", "ВНУТРЕННИХ", "ДЕЛ", "ВЫДАН", "ДАТА", "РОЖДЕНИЯ",
		"СЕРИЯ", "НОМЕР", "КОД", "ПОДРАЗДЕЛЕНИЯ", "МЕСТО", "ПОЛ",
		"МУЖ", "ЖЕН", "РОССИЯ", "ЛИЧНЫЙ", "ФАМИЛИЯ", "ИМЯ", "ОТЧЕСТВО",
	}
	isStopWord := func(s string) bool {
		upper := strings.ToUpper(s)
		for _, sw := range stopWords {
			if upper == sw {
				return true
			}
		}
		return false
	}

	// isSpacedLetters проверяет, что в строке каждая буква отделена пробелом
	isSpacedLetters := func(s string) bool {
		trimmed := strings.TrimSpace(s)
		if len(trimmed) < 5 {
			return false
		}
		letters := 0
		spaces := 0
		for _, r := range trimmed {
			if unicode.IsLetter(r) {
				letters++
			} else if r == ' ' {
				spaces++
			}
		}
		return spaces > 0 && float64(spaces)/float64(letters) > 0.5
	}

	// isName проверяет, что строка похожа на имя/фамилию/отчество
	isName := func(s string) bool {
		s = strings.TrimSpace(s)
		runeCount := utf8.RuneCountInString(s)
		if runeCount < 2 || runeCount > 25 {
			return false
		}
		if isStopWord(s) || isSpacedLetters(s) {
			return false
		}
		// Только русские буквы, начинается с заглавной
		// Принимаем как обычный регистр (Иванов), так и ВЕСЬ ЗАГЛАВНЫЙ (ИВАНОВ)
		if regexp.MustCompile(`^[А-ЯЁ][А-ЯЁа-яё]+$`).MatchString(s) {
			return true
		}
		return false
	}

	// Собираем все строки
	var allLines []string
	for _, pageText := range pages {
		for _, line := range strings.Split(pageText, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				allLines = append(allLines, line)
			}
		}
	}

	// Хелперы для поиска имени в окрестности
	findNameAbove := func(idx int, maxDist int) string {
		for j := idx - 1; j >= 0 && idx-j <= maxDist; j-- {
			if isName(allLines[j]) {
				return allLines[j]
			}
		}
		return ""
	}
	findNameBelow := func(idx int, maxDist int) string {
		for j := idx + 1; j < len(allLines) && j-idx <= maxDist; j++ {
			if isName(allLines[j]) {
				return allLines[j]
			}
		}
		return ""
	}

	// Распространённые русские имена для эвристики определения порядка
	commonFirstNames := map[string]bool{
		"АЛЕКСЕЙ": true, "АЛЕКСАНДР": true, "СЕРГЕЙ": true, "ДМИТРИЙ": true,
		"АНДРЕЙ": true, "МАКСИМ": true, "ИВАН": true, "МИХАИЛ": true,
		"ПАВЕЛ": true, "НИКИТА": true, "ВЛАДИМИР": true, "ЕВГЕНИЙ": true,
		"РОМАН": true, "АРТЁМ": true, "АРТЕМ": true, "ИЛЬЯ": true,
		"КОНСТАНТИН": true, "ВИКТОР": true, "ОЛЕГ": true, "ДЕНИС": true,
		"КИРИЛЛ": true, "АНТОН": true, "ВАДИМ": true, "ГРИГОРИЙ": true,
		"ЛЕОНИД": true, "БОРИС": true, "СТАНИСЛАВ": true, "ГЕОРГИЙ": true,
		"СЕМЁН": true, "СЕМЕН": true, "ПЁТР": true, "ПЕТР": true,
		"АРКАДИЙ": true, "ГЕРМАН": true, "ЗАХАР": true, "МАТВЕЙ": true,
		"ПЛАТОН": true, "ТИМОФЕЙ": true, "ФЁДОР": true,
		"ФЕДОР": true, "ЭДУАРД": true, "ЯРОСЛАВ": true, "САВЕЛИЙ": true,
		"ТИХОН": true, "ДАНИИЛ": true, "ДАНИЛ": true, "ЕГОР": true,
		"МАРК": true, "ЛЕВ": true, "НИКОЛАЙ": true,
		"ТИМУР": true, "ЯН": true, "САША": true, "ВЛАД": true,
		"ДИМА": true, "МАКС": true, "КОЛЯ": true, "ВИТЯ": true,
		"СТЕПАН": true, "ГЛЕБ": true, "ТАРАС": true, "ЮРИЙ": true,
		"ИГОРЬ": true, "РУСЛАН": true, "ВЯЧЕСЛАВ": true, "МАРАТ": true,
		"АЛЬБЕРТ": true, "РУСТАМ": true, "ТЕМУР": true, "ЗАУР": true,
		"ТАМЕРЛАН": true, "САИД": true, "МАГОМЕД": true, "АХМЕД": true,
		"ИСЛАМ": true, "АЛИ": true, "РАМИЛЬ": true, "АЗАТ": true,
		"ИЛЬДАР": true, "АЙРАТ": true, "МАРСЕЛЬ": true, "АМИР": true,
		"ТИГРАН": true, "ГАГИК": true, "АРТУР": true, "САМВЕЛ": true,
	}
	isCommonFirstName := func(s string) bool {
		return commonFirstNames[strings.ToUpper(s)]
	}

	// Определяет порядок имён: возвращает (фамилия, имя)
	detectNameOrder := func(a, b string) (lastName, firstName string) {
		aIsName := isCommonFirstName(a)
		bIsName := isCommonFirstName(b)
		if aIsName && !bIsName {
			return b, a
		}
		if bIsName && !aIsName {
			return a, b
		}
		// Если оба или ни одного — оставляем как есть
		return a, b
	}

	// 1. Ищем ФИО по меткам (Фамилия/Имя/Отчество)
	// В паспорте РФ значение идёт ПЕРЕД лейблом (сверху), лейбл снизу
	for i, line := range allLines {
		upper := strings.ToUpper(line)
		if strings.Contains(upper, "ФАМИЛИЯ") {
			val := findNameAbove(i, 5)
			if val == "" {
				val = findNameBelow(i, 3)
			}
			if val != "" {
				fields["last_name"] = Field{Value: normalizeName(val), Confidence: getConfidenceForLine(val, blocks)}
			}
		}
		if strings.Contains(upper, "ИМЯ") && !strings.Contains(upper, "ОТЧЕСТВО") {
			val := findNameAbove(i, 5)
			if val == "" {
				val = findNameBelow(i, 3)
			}
			if val != "" {
				fields["first_name"] = Field{Value: normalizeName(val), Confidence: getConfidenceForLine(val, blocks)}
			}
		}
		if strings.Contains(upper, "ОТЧЕСТВО") {
			val := findNameAbove(i, 3)
			if val == "" {
				val = findNameBelow(i, 3)
			}
			if val != "" {
				fields["middle_name"] = Field{Value: normalizeName(val), Confidence: getConfidenceForLine(val, blocks)}
			}
		}
	}

	// Дополнение: если по меткам нашли только часть ФИО — дополняем из оставшихся имён
	if fields["last_name"].Value != "" || fields["first_name"].Value != "" || fields["middle_name"].Value != "" {
		used := make(map[string]bool)
		if fields["last_name"].Value != "" {
			used[strings.ToUpper(fields["last_name"].Value)] = true
		}
		if fields["first_name"].Value != "" {
			used[strings.ToUpper(fields["first_name"].Value)] = true
		}
		if fields["middle_name"].Value != "" {
			used[strings.ToUpper(fields["middle_name"].Value)] = true
		}
		var unusedNames []string
		for _, line := range allLines {
			if isName(line) && !used[strings.ToUpper(line)] {
				unusedNames = append(unusedNames, line)
			}
		}
		if fields["first_name"].Value == "" && len(unusedNames) > 0 {
			fields["first_name"] = Field{Value: normalizeName(unusedNames[0]), Confidence: getConfidenceForLine(unusedNames[0], blocks)}
			used[strings.ToUpper(unusedNames[0])] = true
			unusedNames = unusedNames[1:]
		}
		if fields["middle_name"].Value == "" && len(unusedNames) > 0 {
			fields["middle_name"] = Field{Value: normalizeName(unusedNames[0]), Confidence: getConfidenceForLine(unusedNames[0], blocks)}
		}
	}

	// Fallback: если не нашли по меткам — ищем имена в скользящем окне
	if fields["last_name"].Value == "" {
		// Сначала ищем 3 имени в окне 8 строк
		for i := 0; i < len(allLines)-2; i++ {
			var names []string
			for j := i; j <= min(len(allLines)-1, i+8) && len(names) < 3; j++ {
				if isName(allLines[j]) {
					names = append(names, allLines[j])
				}
			}
			if len(names) >= 3 {
				fields["last_name"] = Field{Value: normalizeName(names[0]), Confidence: getConfidenceForLine(names[0], blocks)}
				fields["first_name"] = Field{Value: normalizeName(names[1]), Confidence: getConfidenceForLine(names[1], blocks)}
				fields["middle_name"] = Field{Value: normalizeName(names[2]), Confidence: getConfidenceForLine(names[2], blocks)}
				break
			}
		}
	}
	// Если всё ещё нет last_name, ищем 2 имени в окне 6 строк (фамилия + имя)
	if fields["last_name"].Value == "" {
		for i := 0; i < len(allLines)-1; i++ {
			var names []string
			for j := i; j <= min(len(allLines)-1, i+6) && len(names) < 2; j++ {
				if isName(allLines[j]) {
					names = append(names, allLines[j])
				}
			}
			if len(names) >= 2 {
				last, first := detectNameOrder(names[0], names[1])
				fields["last_name"] = Field{Value: normalizeName(last), Confidence: getConfidenceForLine(last, blocks)}
				fields["first_name"] = Field{Value: normalizeName(first), Confidence: getConfidenceForLine(first, blocks)}
				break
			}
		}
	}

	// 2. Ищем серию и номер
	seriesRegex := regexp.MustCompile(`(\d{2})\s*(\d{2})\s+(\d{6})`)
	dateRegex := regexp.MustCompile(`(\d{1,2})\s*[.]\s*(\d{1,2})\s*[.]\s*(\d{4})`)
	divisionRegex := regexp.MustCompile(`\d{3}\s*[-–—]\s*\d{3}`)

	for _, line := range allLines {
		// Паттерн: XX XX XXXXXX (серия + номер с пробелами)
		if matches := seriesRegex.FindStringSubmatch(line); matches != nil {
			series := matches[1] + matches[2]
			if series != "0101" && series != "3112" && series != "3005" && series != "1601" {
				fields["series"] = Field{Value: series, Confidence: getConfidenceForLine(line, blocks)}
				fields["number"] = Field{Value: matches[3], Confidence: getConfidenceForLine(line, blocks)}
				break
			}
		}
	}
	// Отдельно серия (4 цифры) и номер (6 цифр), если не нашли вместе
	for _, line := range allLines {
		if fields["series"].Value == "" {
			if matches := regexp.MustCompile(`\b(\d{4})\b`).FindStringSubmatch(line); matches != nil {
				s := matches[1]
				if s != "0101" && s != "3112" {
					fields["series"] = Field{Value: s, Confidence: getConfidenceForLine(line, blocks)}
				}
			}
		}
		if fields["number"].Value == "" {
			if matches := regexp.MustCompile(`\b(\d{6})\b`).FindStringSubmatch(line); matches != nil {
				n := matches[1]
				fields["number"] = Field{Value: n, Confidence: getConfidenceForLine(line, blocks)}
			}
		}
	}

	// 3. Ищем даты с контекстом
	for i, line := range allLines {
		upper := strings.ToUpper(line)
		// Ищем все даты в строке
		dateMatches := dateRegex.FindAllStringSubmatch(line, -1)
		for _, dm := range dateMatches {
			date := dm[1] + "." + dm[2] + "." + dm[3]
			year, _ := strconv.Atoi(dm[3])
			if year < 1500 || year > 2100 {
				continue // пропускаем невалидные годы
			}
			// Собираем контекст (соседние строки)
			ctx := ""
			for j := max(0, i-2); j <= min(len(allLines)-1, i+2); j++ {
				if j != i {
					ctx += " " + strings.ToUpper(allLines[j])
				}
			}

			// Дата рождения: рядом с "рождения", "место рождения", "дата рождения"
			if fields["birth_date"].Value == "" {
				if strings.Contains(upper, "РОЖДЕНИЯ") || strings.Contains(upper, "РОЖДЕНИЕ") ||
					strings.Contains(ctx, "РОЖДЕНИЯ") || strings.Contains(ctx, "МЕСТО РОЖДЕНИЯ") {
					fields["birth_date"] = Field{Value: normalizeDate(date), Confidence: getConfidenceForLine(line, blocks)}
					continue
				}
			}
			// Дата выдачи: рядом с "выдачи", "выдан", "паспорт"
			if fields["issue_date"].Value == "" {
				if strings.Contains(upper, "ВЫДАЧИ") || strings.Contains(upper, "ВЫДАН") ||
					strings.Contains(ctx, "ВЫДАН") || strings.Contains(ctx, "ПАСПОРТ") ||
					strings.Contains(ctx, "ВЫДАЧИ") {
					fields["issue_date"] = Field{Value: normalizeDate(date), Confidence: getConfidenceForLine(line, blocks)}
					continue
				}
			}
		}
	}

	// Fallback для дат: если не определили по контексту
	var allDates []string
	for _, line := range allLines {
		if matches := dateRegex.FindStringSubmatch(line); len(matches) == 4 {
			year, _ := strconv.Atoi(matches[3])
			if year >= 1500 && year <= 2100 {
				allDates = append(allDates, matches[0])
			}
		}
	}
	if fields["issue_date"].Value == "" {
		// Ищем первую дату, отличную от birth_date
		for _, d := range allDates {
			if normalizeDate(d) != fields["birth_date"].Value {
				fields["issue_date"] = Field{Value: normalizeDate(d), Confidence: getConfidenceForLine(d, blocks)}
				break
			}
		}
	}
	if fields["birth_date"].Value == "" {
		// Ищем первую дату, отличную от issue_date
		for _, d := range allDates {
			if normalizeDate(d) != fields["issue_date"].Value {
				fields["birth_date"] = Field{Value: normalizeDate(d), Confidence: getConfidenceForLine(d, blocks)}
				break
			}
		}
	}

	// Если даты перепутались — меняем (дата рождения должна быть раньше)
	if fields["birth_date"].Value != "" && fields["issue_date"].Value != "" {
		birthT, _ := time.Parse("02.01.2006", fields["birth_date"].Value)
		issueT, _ := time.Parse("02.01.2006", fields["issue_date"].Value)
		if birthT.After(issueT) {
			fields["birth_date"], fields["issue_date"] = fields["issue_date"], fields["birth_date"]
		}
	}

	// 4. Ищем код подразделения
	for i, line := range allLines {
		upper := strings.ToUpper(line)
		if matches := divisionRegex.FindStringSubmatch(line); matches != nil {
			code := matches[0]
			// Нормализуем дефис
			code = regexp.MustCompile(`\s*[-–—]\s*`).ReplaceAllString(code, "-")
			// Проверяем соседние строки на "Код подразделения"
			isDivisionCode := false
			if strings.Contains(upper, "КОД") || strings.Contains(upper, "ПОДРАЗДЕЛЕНИЯ") {
				isDivisionCode = true
			}
			for j := 1; j <= 3 && i-j >= 0; j++ {
				if strings.Contains(strings.ToUpper(allLines[i-j]), "КОД") || strings.Contains(strings.ToUpper(allLines[i-j]), "ПОДРАЗДЕЛЕНИЯ") {
					isDivisionCode = true
					break
				}
			}
			if isDivisionCode || fields["issue_date"].Value != "" {
				fields["division_code"] = Field{Value: code, Confidence: getConfidenceForLine(line, blocks)}
				break
			}
		}
	}

	// 5. Ищем "кем выдан"
	// Стратегия: собираем строки с ключевыми словами УФМС/УМВД/МВД/ОТДЕЛ
	var issuedByParts []string
	for _, line := range allLines {
		upper := strings.ToUpper(line)
		// Пропускаем не-issued_by строки
		if seriesRegex.MatchString(line) || divisionRegex.MatchString(line) || dateRegex.MatchString(line) {
			continue
		}
		if strings.Contains(upper, "ФАМИЛИЯ") || strings.Contains(upper, "ИМЯ") || strings.Contains(upper, "ОТЧЕСТВО") ||
			strings.Contains(upper, "ДАТА РОЖДЕНИЯ") || strings.Contains(upper, "МЕСТО РОЖДЕНИЯ") ||
			strings.Contains(upper, "ПОЛ") || strings.Contains(upper, "РОЖДЕНИЯ") {
			continue
		}
		if strings.Contains(upper, "ПАСПОРТ РОССИИ") || strings.Contains(upper, "РОССИЙСКАЯ ФЕДЕРАЦИЯ") ||
			strings.Contains(upper, "ЛИЧНЫЙ") {
			continue
		}

		// Если строка содержит authority keywords — добавляем
		if strings.Contains(upper, "УФМС") || strings.Contains(upper, "УМВД") || strings.Contains(upper, "МВД") ||
			strings.Contains(upper, "ОТДЕЛ") || strings.Contains(upper, "ФМС") || strings.Contains(upper, "ОВД") ||
			strings.Contains(upper, "РОВД") || strings.Contains(upper, "ПАСПОРТ ВЫДАН") || strings.Contains(upper, "ВЫДАН") {
			clean := line
			// Удаляем "Паспорт выдан" префикс
			if idx := strings.Index(strings.ToUpper(clean), "ПАСПОРТ ВЫДАН"); idx != -1 {
				clean = clean[idx+len("Паспорт выдан"):]
			} else if idx := strings.Index(strings.ToUpper(clean), "ВЫДАН"); idx != -1 && idx < 5 {
				clean = clean[idx+len("выдан"):]
			}
			// Удаляем даты, серии, номера, division code из строки
			clean = seriesRegex.ReplaceAllString(clean, "")
			clean = divisionRegex.ReplaceAllString(clean, "")
			clean = dateRegex.ReplaceAllString(clean, "")
			clean = strings.TrimSpace(clean)
			if clean != "" {
				issuedByParts = append(issuedByParts, clean)
			}
		}
	}
	if len(issuedByParts) > 0 {
		issuedBy := strings.Join(issuedByParts, " ")
		// Убираем дублирование
		issuedBy = regexp.MustCompile(`\s+`).ReplaceAllString(issuedBy, " ")
		fields["issued_by"] = Field{Value: issuedBy, Confidence: 0.8}
	}

	return fields
}

// normalizeName приводит имя к нормальному виду (первая заглавная, остальные строчные)
func normalizeName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	for i := 1; i < len(runes); i++ {
		runes[i] = unicode.ToLower(runes[i])
	}
	return string(runes)
}

// normalizeDate нормализует дату к формату ДД.ММ.ГГГГ
func normalizeDate(s string) string {
	s = strings.TrimSpace(s)
	// Заменяем / и пробелы на точки
	s = strings.ReplaceAll(s, "/", ".")
	s = strings.ReplaceAll(s, " ", ".")
	// Убираем лишние точки
	s = regexp.MustCompile(`\.{2,}`).ReplaceAllString(s, ".")
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return s
	}
	day := parts[0]
	month := parts[1]
	year := parts[2]
	if len(day) == 1 {
		day = "0" + day
	}
	if len(month) == 1 {
		month = "0" + month
	}
	return day + "." + month + "." + year
}

// getConfidenceForLine возвращает confidence для строки
func getConfidenceForLine(line string, blocks []textBlock) float64 {
	line = strings.TrimSpace(line)
	for _, block := range blocks {
		if strings.Contains(block.Text, line) {
			return block.Confidence
		}
	}
	// Fallback: case-insensitive поиск
	lowerLine := strings.ToLower(line)
	for _, block := range blocks {
		if strings.Contains(strings.ToLower(block.Text), lowerLine) {
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
