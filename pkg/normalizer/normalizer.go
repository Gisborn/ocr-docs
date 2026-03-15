package normalizer

import (
	"regexp"
	"strings"
	"time"
	"unicode"
)

// Normalizer нормализует данные паспорта РФ
type Normalizer struct {
	// Можно добавить настройки (форматы дат и т.д.)
}

// New создает новый нормализатор
func New() *Normalizer {
	return &Normalizer{}
}

// PassportData структурированные данные паспорта
type PassportData struct {
	LastName           string  `json:"last_name"`
	FirstName          string  `json:"first_name"`
	MiddleName         string  `json:"middle_name"`
	BirthDate          string  `json:"birth_date"`          // ДД.ММ.ГГГГ
	Series             string  `json:"series"`              // XXXX
	Number             string  `json:"number"`              // XXXXXX
	IssueDate          string  `json:"issue_date"`          // ДД.ММ.ГГГГ
	IssuedBy           string  `json:"issued_by"`
	DivisionCode       string  `json:"division_code"`       // XXX-XXX
	RegistrationAddress *string `json:"registration_address"` // всегда null в MVP
}

// Result результат нормализации с confidence
type Result struct {
	Data       PassportData       `json:"data"`
	Confidences FieldConfidences  `json:"confidences"`
}

// FieldConfidences confidence по каждому полю
type FieldConfidences struct {
	LastName           float64 `json:"last_name"`
	FirstName          float64 `json:"first_name"`
	MiddleName         float64 `json:"middle_name"`
	BirthDate          float64 `json:"birth_date"`
	Series             float64 `json:"series"`
	Number             float64 `json:"number"`
	IssueDate          float64 `json:"issue_date"`
	IssuedBy           float64 `json:"issued_by"`
	DivisionCode       float64 `json:"division_code"`
	RegistrationAddress float64 `json:"registration_address"`
}

// Normalize парсит сырой текст и возвращает структурированные данные
func (n *Normalizer) Normalize(rawText string) (*Result, error) {
	// Убираем лишние пробелы и нормализуем
	text := n.cleanText(rawText)
	
	result := &Result{
		Data: PassportData{
			RegistrationAddress: nil, // Всегда null в MVP
		},
		Confidences: FieldConfidences{
			RegistrationAddress: 0,
		},
	}
	
	// Извлекаем ФИО
	fio := n.extractFIO(text)
	result.Data.LastName = fio.LastName
	result.Data.FirstName = fio.FirstName
	result.Data.MiddleName = fio.MiddleName
	result.Confidences.LastName = n.calculateConfidence(fio.LastName, "last_name")
	result.Confidences.FirstName = n.calculateConfidence(fio.FirstName, "first_name")
	result.Confidences.MiddleName = n.calculateConfidence(fio.MiddleName, "middle_name")
	
	// Извлекаем даты
	birthDate, birthConf := n.extractDate(text, "birth")
	result.Data.BirthDate = birthDate
	result.Confidences.BirthDate = birthConf
	
	issueDate, issueConf := n.extractDate(text, "issue")
	result.Data.IssueDate = issueDate
	result.Confidences.IssueDate = issueConf
	
	// Извлекаем серию и номер
	series, number, seriesConf, numberConf := n.extractSeriesAndNumber(text)
	result.Data.Series = series
	result.Data.Number = number
	result.Confidences.Series = seriesConf
	result.Confidences.Number = numberConf
	
	// Извлекаем код подразделения
	divisionCode, divisionConf := n.extractDivisionCode(text)
	result.Data.DivisionCode = divisionCode
	result.Confidences.DivisionCode = divisionConf
	
	// Извлекаем кем выдан
	issuedBy, issuedConf := n.extractIssuedBy(text)
	result.Data.IssuedBy = issuedBy
	result.Confidences.IssuedBy = issuedConf
	
	return result, nil
}

// cleanText нормализует текст
func (n *Normalizer) cleanText(text string) string {
	// Убираем лишние пробелы
	text = regexp.MustCompile(`\s+`).ReplaceAllString(text, " ")
	// Убираем пробелы вокруг переносов строк
	text = regexp.MustCompile(` ?\n ?`).ReplaceAllString(text, "\n")
	// Приводим к нижнему регистру для поиска
	return strings.TrimSpace(text)
}

// FIO структура для ФИО
type FIO struct {
	LastName   string
	FirstName  string
	MiddleName string
}

// extractFIO извлекает ФИО из текста
func (n *Normalizer) extractFIO(text string) FIO {
	// Ищем паттерн: ФАМИЛИЯ ИМЯ ОТЧЕСТВО
	// Обычно идут подряд в начале документа или после "ПОЛНОЕ ИМЯ"
	
	// Паттерн для русских ФИО (3 слова подряд с заглавной буквы)
	fioPattern := regexp.MustCompile(`([А-ЯЁ][а-яё]+)\s+([А-ЯЁ][а-яё]+)\s+([А-ЯЁ][а-яё]+)`)
	
	matches := fioPattern.FindStringSubmatch(text)
	if len(matches) >= 4 {
		return FIO{
			LastName:   matches[1],
			FirstName:  matches[2],
			MiddleName: matches[3],
		}
	}
	
	// Fallback: ищем отдельно
	return FIO{}
}

// extractDate извлекает дату (рождения или выдачи)
func (n *Normalizer) extractDate(text string, dateType string) (string, float64) {
	// Ищем даты в форматах: ДД.ММ.ГГГГ, ДД/ММ/ГГГГ, ДД ММ ГГГГ
	datePatterns := []*regexp.Regexp{
		regexp.MustCompile(`(\d{2})\.(\d{2})\.(\d{4})`),
		regexp.MustCompile(`(\d{2})/(\d{2})/(\d{4})`),
	}
	
	dates := make([]string, 0)
	for _, pattern := range datePatterns {
		matches := pattern.FindAllStringSubmatch(text, -1)
		for _, match := range matches {
			if len(match) >= 4 {
				day, month, year := match[1], match[2], match[3]
				if n.isValidDate(day, month, year) {
					dates = append(dates, day+"."+month+"."+year)
				}
			}
		}
	}
	
	if len(dates) == 0 {
		return "", 0
	}
	
	// Для даты рождения берем самую раннюю (паспорт выдается позже рождения)
	// Для даты выдачи берем более позднюю
	// TODO: улучшить логику с учетом контекста (поиск ключевых слов)
	
	if dateType == "birth" {
		return dates[0], 0.9 // Берем первую найденную как дату рождения
	}
	
	if len(dates) > 1 {
		return dates[1], 0.9 // Вторая дата - дата выдачи
	}
	
	return dates[0], 0.7 // Только одна дата - низкий confidence
}

// isValidDate проверяет валидность даты
func (n *Normalizer) isValidDate(day, month, year string) bool {
	_, err := time.Parse("02.01.2006", day+"."+month+"."+year)
	return err == nil
}

// extractSeriesAndNumber извлекает серию и номер паспорта
func (n *Normalizer) extractSeriesAndNumber(text string) (series, number string, seriesConf, numberConf float64) {
	// Серия: 4 цифры (обычно в формате XX XX или XXXX)
	// Номер: 6 цифр
	
	// Ищем серию и номер рядом
	pattern := regexp.MustCompile(`(\d{2})\s?(\d{2})\s?(\d{6})`)
	matches := pattern.FindStringSubmatch(text)
	
	if len(matches) >= 4 {
		series = matches[1] + matches[2]
		number = matches[3]
		seriesConf = 0.95
		numberConf = 0.95
		return
	}
	
	// Отдельный поиск серии (4 цифры)
	seriesPattern := regexp.MustCompile(`(?:серия|series)?[:\s]*(\d{4})`)
	seriesMatches := seriesPattern.FindStringSubmatch(text)
	if len(seriesMatches) >= 2 {
		series = seriesMatches[1]
		seriesConf = 0.8
	}
	
	// Отдельный поиск номера (6 цифр)
	numberPattern := regexp.MustCompile(`(?:номер|number|№)[:\s]*(\d{6})`)
	numberMatches := numberPattern.FindStringSubmatch(text)
	if len(numberMatches) >= 2 {
		number = numberMatches[1]
		numberConf = 0.8
	}
	
	return
}

// extractDivisionCode извлекает код подразделения (XXX-XXX)
func (n *Normalizer) extractDivisionCode(text string) (string, float64) {
	// Формат: XXX-XXX или XXXXXXX (7 цифр)
	pattern := regexp.MustCompile(`(\d{3})-?(\d{3})`)
	matches := pattern.FindStringSubmatch(text)
	
	if len(matches) >= 3 {
		code := matches[1] + "-" + matches[2]
		return code, 0.9
	}
	
	return "", 0
}

// extractIssuedBy извлекает "кем выдан"
func (n *Normalizer) extractIssuedBy(text string) (string, float64) {
	// Ищем после ключевых слов "выдан", "кем выдан", "authority"
	// TODO: улучшить парсинг - это сложная часть
	
	keywords := []string{"выдан", "кем выдан", "орган выдачи", "issuing authority"}
	
	for _, keyword := range keywords {
		idx := strings.Index(strings.ToLower(text), keyword)
		if idx != -1 {
			// Берем текст после ключевого слова до конца строки или следующего поля
			start := idx + len(keyword)
			rest := text[start:]
			
			// Ищем конец (следующее поле или перенос строки)
			end := strings.IndexAny(rest, "\n,;")
			if end == -1 {
				end = len(rest)
			}
			
			issuedBy := strings.TrimSpace(rest[:end])
			if len(issuedBy) > 5 {
				return issuedBy, 0.8
			}
		}
	}
	
	return "", 0
}

// calculateConfidence оценивает уверенность в распознанном поле
func (n *Normalizer) calculateConfidence(value, fieldType string) float64 {
	if value == "" {
		return 0
	}
	
	confidence := 0.9
	
	// Проверяем на наличие подозрительных символов
	suspiciousChars := 0
	for _, r := range value {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsSpace(r) && r != '.' && r != '-' {
			suspiciousChars++
		}
	}
	
	if suspiciousChars > 0 {
		confidence -= float64(suspiciousChars) * 0.1
	}
	
	// Проверяем длину
	minLen := map[string]int{
		"last_name":   2,
		"first_name":  2,
		"middle_name": 2,
	}
	
	if min, ok := minLen[fieldType]; ok && len([]rune(value)) < min {
		confidence -= 0.3
	}
	
	if confidence < 0 {
		confidence = 0
	}
	
	return confidence
}
