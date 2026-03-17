package normalizer

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// PassportFields стандартизированные поля паспорта РФ
type PassportFields struct {
	LastName       string             `json:"last_name"`
	FirstName      string             `json:"first_name"`
	MiddleName     string             `json:"middle_name"`
	BirthDate      string             `json:"birth_date"`       // ДД.ММ.ГГГГ
	Series         string             `json:"series"`           // XXXX (4 цифры)
	Number         string             `json:"number"`           // XXXXXX (6 цифр)
	IssueDate      string             `json:"issue_date"`       // ДД.ММ.ГГГГ
	IssuedBy       string             `json:"issued_by"`        // Кем выдан
	DivisionCode   string             `json:"division_code"`    // XXX-XXX
	RegistrationAddress *string       `json:"registration_address"` // null в MVP
}

// NormalizedResult результат нормализации с confidence
type NormalizedResult struct {
	Fields     PassportFields     `json:"fields"`
	Confidences map[string]float64 `json:"confidences"`
}

// NormalizePassport нормализует поля паспорта
func NormalizePassport(rawFields map[string]string, rawConfidences map[string]float64) *NormalizedResult {
	result := &NormalizedResult{
		Fields:      PassportFields{},
		Confidences: make(map[string]float64),
	}

	// ФИО
	result.Fields.LastName = normalizeName(rawFields["last_name"])
	result.Confidences["last_name"] = rawConfidences["last_name"]

	result.Fields.FirstName = normalizeName(rawFields["first_name"])
	result.Confidences["first_name"] = rawConfidences["first_name"]

	result.Fields.MiddleName = normalizeName(rawFields["middle_name"])
	result.Confidences["middle_name"] = rawConfidences["middle_name"]

	// Даты
	result.Fields.BirthDate = normalizeDate(rawFields["birth_date"])
	result.Confidences["birth_date"] = rawConfidences["birth_date"]

	result.Fields.IssueDate = normalizeDate(rawFields["issue_date"])
	result.Confidences["issue_date"] = rawConfidences["issue_date"]

	// Серия и номер
	result.Fields.Series = normalizeSeries(rawFields["series"])
	result.Confidences["series"] = rawConfidences["series"]

	result.Fields.Number = normalizeNumber(rawFields["number"])
	result.Confidences["number"] = rawConfidences["number"]

	// Код подразделения
	result.Fields.DivisionCode = normalizeDivisionCode(rawFields["division_code"])
	result.Confidences["division_code"] = rawConfidences["division_code"]

	// Кем выдан (не нормализуем, только чистим)
	result.Fields.IssuedBy = cleanText(rawFields["issued_by"])
	result.Confidences["issued_by"] = rawConfidences["issued_by"]

	// Адрес регистрации - всегда null в MVP
	nullAddr := ""
	result.Fields.RegistrationAddress = &nullAddr
	*result.Fields.RegistrationAddress = ""
	result.Fields.RegistrationAddress = nil

	return result
}

// normalizeName нормализует имя (только буквы, с заглавной)
func normalizeName(name string) string {
	name = cleanText(name)
	if name == "" {
		return ""
	}

	// Убираем все символы кроме букв и пробелов
	var result strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || r == ' ' || r == '-' {
			result.WriteRune(r)
		}
	}

	words := strings.Fields(result.String())
	for i, word := range words {
		words[i] = capitalize(word)
	}

	return strings.Join(words, " ")
}

// normalizeDate нормализует дату в формат ДД.ММ.ГГГГ
func normalizeDate(date string) string {
	date = cleanText(date)
	if date == "" {
		return ""
	}

	// Ищем паттерны дат
	patterns := []string{
		`(\d{1,2})[.\s]\s*(\d{1,2})[.\s]\s*(\d{2,4})`,  // ДД.ММ.ГГГГ или ДД.ММ.ГГ
		`(\d{1,2})\s*/\s*(\d{1,2})\s*/\s*(\d{2,4})`,    // ДД/ММ/ГГГГ
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(date)
		if len(matches) == 4 {
			day := padLeft(matches[1], 2, '0')
			month := padLeft(matches[2], 2, '0')
			year := matches[3]

			// Если год 2 цифры, определяем век
			if len(year) == 2 {
				yearInt, _ := strconv.Atoi(year)
				if yearInt > 50 {
					year = "19" + year
				} else {
					year = "20" + year
				}
			}

			// Проверяем валидность даты
			if isValidDate(day, month, year) {
				return fmt.Sprintf("%s.%s.%s", day, month, year)
			}
		}
	}

	return ""
}

// normalizeSeries нормализует серию паспорта (4 цифры)
func normalizeSeries(series string) string {
	series = cleanText(series)
	// Оставляем только цифры
	re := regexp.MustCompile(`\D`)
	digits := re.ReplaceAllString(series, "")

	if len(digits) == 4 {
		return digits
	}
	return ""
}

// normalizeNumber нормализует номер паспорта (6 цифр)
func normalizeNumber(number string) string {
	number = cleanText(number)
	// Оставляем только цифры
	re := regexp.MustCompile(`\D`)
	digits := re.ReplaceAllString(number, "")

	if len(digits) == 6 {
		return digits
	}
	return ""
}

// normalizeDivisionCode нормализует код подразделения (XXX-XXX)
func normalizeDivisionCode(code string) string {
	code = cleanText(code)
	// Оставляем только цифры
	re := regexp.MustCompile(`\D`)
	digits := re.ReplaceAllString(code, "")

	if len(digits) == 6 {
		return fmt.Sprintf("%s-%s", digits[0:3], digits[3:6])
	}
	return ""
}

// cleanText очищает текст от лишних пробелов и спецсимволов
func cleanText(text string) string {
	text = strings.TrimSpace(text)
	// Убираем множественные пробелы
	re := regexp.MustCompile(`\s+`)
	text = re.ReplaceAllString(text, " ")
	return text
}

// capitalize делает первую букву заглавной, остальные строчными
func capitalize(s string) string {
	if s == "" {
		return ""
	}
	runes := []rune(s)
	return string(unicode.ToUpper(runes[0])) + string(runes[1:])
}

// padLeft дополняет строку слева до указанной длины
func padLeft(s string, length int, pad rune) string {
	for len(s) < length {
		s = string(pad) + s
	}
	return s
}

// isValidDate проверяет валидность даты
func isValidDate(day, month, year string) bool {
	d, _ := strconv.Atoi(day)
	m, _ := strconv.Atoi(month)
	y, _ := strconv.Atoi(year)

	if d < 1 || d > 31 || m < 1 || m > 12 || y < 1900 || y > 2100 {
		return false
	}

	// Проверяем реальную дату
	_, err := time.Parse("2006-01-02", fmt.Sprintf("%s-%s-%s", year, month, day))
	return err == nil
}

// ValidatePassportFields проверяет валидность всех полей паспорта
func ValidatePassportFields(fields *PassportFields) map[string]string {
	errors := make(map[string]string)

	if fields.Series == "" || len(fields.Series) != 4 {
		errors["series"] = "невалидная серия паспорта (ожидается 4 цифры)"
	}

	if fields.Number == "" || len(fields.Number) != 6 {
		errors["number"] = "невалидный номер паспорта (ожидается 6 цифр)"
	}

	if fields.DivisionCode != "" && !regexp.MustCompile(`^\d{3}-\d{3}$`).MatchString(fields.DivisionCode) {
		errors["division_code"] = "невалидный код подразделения (ожидается XXX-XXX)"
	}

	if fields.BirthDate != "" {
		if _, err := time.Parse("02.01.2006", fields.BirthDate); err != nil {
			errors["birth_date"] = "невалидная дата рождения"
		}
	}

	if fields.IssueDate != "" {
		if _, err := time.Parse("02.01.2006", fields.IssueDate); err != nil {
			errors["issue_date"] = "невалидная дата выдачи"
		}
	}

	return errors
}
