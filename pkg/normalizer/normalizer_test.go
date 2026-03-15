package normalizer

import (
	"testing"
)

func TestNormalize(t *testing.T) {
	n := New()
	
	tests := []struct {
		name     string
		rawText  string
		wantErr  bool
		checkFn  func(*Result) bool
	}{
		{
			name: "Стандартный паспорт",
			rawText: `ПАСПОРТ РОССИЙСКОЙ ФЕДЕРАЦИИ

ИВАНОВ ИВАН ИВАНОВИЧ
01.01.1990
г. Москва

СЕРИЯ 4515 НОМЕР 123456
ВЫДАН 15.05.2015
ОТДЕЛОМ УФМС РОССИИ ПО Г. МОСКВЕ
КОД ПОДРАЗДЕЛЕНИЯ 770-064`,
			checkFn: func(r *Result) bool {
				return r.Data.LastName == "Иванов" &&
					r.Data.FirstName == "Иван" &&
					r.Data.MiddleName == "Иванович" &&
					r.Data.Series == "4515" &&
					r.Data.Number == "123456"
			},
		},
		{
			name: "Только ФИО",
			rawText: `ПЕТРОВ ПЕТР ПЕТРОВИЧ`,
			checkFn: func(r *Result) bool {
				return r.Data.LastName == "Петров" &&
					r.Data.FirstName == "Петр" &&
					r.Data.MiddleName == "Петрович"
			},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := n.Normalize(tt.rawText)
			if (err != nil) != tt.wantErr {
				t.Errorf("Normalize() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.checkFn(result) {
				t.Errorf("Normalize() result check failed: %+v", result.Data)
			}
		})
	}
}

func TestExtractFIO(t *testing.T) {
	n := New()
	
	tests := []struct {
		name    string
		text    string
		wantFIO FIO
	}{
		{
			name: "Стандартное ФИО",
			text: "Иванов Иван Иванович",
			wantFIO: FIO{
				LastName:   "Иванов",
				FirstName:  "Иван",
				MiddleName: "Иванович",
			},
		},
		{
			name: "ФИО с заглавными буквами",
			text: "ПЕТРОВ ПЕТР ПЕТРОВИЧ",
			wantFIO: FIO{
				LastName:   "Петров",
				FirstName:  "Петр",
				MiddleName: "Петрович",
			},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := n.extractFIO(tt.text)
			if got.LastName != tt.wantFIO.LastName {
				t.Errorf("LastName = %v, want %v", got.LastName, tt.wantFIO.LastName)
			}
			if got.FirstName != tt.wantFIO.FirstName {
				t.Errorf("FirstName = %v, want %v", got.FirstName, tt.wantFIO.FirstName)
			}
			if got.MiddleName != tt.wantFIO.MiddleName {
				t.Errorf("MiddleName = %v, want %v", got.MiddleName, tt.wantFIO.MiddleName)
			}
		})
	}
}

func TestExtractSeriesAndNumber(t *testing.T) {
	n := New()
	
	tests := []struct {
		name            string
		text            string
		wantSeries      string
		wantNumber      string
		wantSeriesConf  float64
		wantNumberConf  float64
	}{
		{
			name:           "Серия и номер через пробел",
			text:           "45 15 123456",
			wantSeries:     "4515",
			wantNumber:     "123456",
			wantSeriesConf: 0.95,
			wantNumberConf: 0.95,
		},
		{
			name:           "Серия и номер слитно",
			text:           "4515123456",
			wantSeries:     "4515",
			wantNumber:     "123456",
			wantSeriesConf: 0.95,
			wantNumberConf: 0.95,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			series, number, seriesConf, numberConf := n.extractSeriesAndNumber(tt.text)
			if series != tt.wantSeries {
				t.Errorf("series = %v, want %v", series, tt.wantSeries)
			}
			if number != tt.wantNumber {
				t.Errorf("number = %v, want %v", number, tt.wantNumber)
			}
			if seriesConf != tt.wantSeriesConf {
				t.Errorf("seriesConf = %v, want %v", seriesConf, tt.wantSeriesConf)
			}
			if numberConf != tt.wantNumberConf {
				t.Errorf("numberConf = %v, want %v", numberConf, tt.wantNumberConf)
			}
		})
	}
}

func TestExtractDivisionCode(t *testing.T) {
	n := New()
	
	tests := []struct {
		name     string
		text     string
		wantCode string
		wantConf float64
	}{
		{
			name:     "Код с дефисом",
			text:     "код 770-064",
			wantCode: "770-064",
			wantConf: 0.9,
		},
		{
			name:     "Код без дефиса",
			text:     "770064",
			wantCode: "770-064",
			wantConf: 0.9,
		},
		{
			name:     "Нет кода",
			text:     "какой-то текст",
			wantCode: "",
			wantConf: 0,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, conf := n.extractDivisionCode(tt.text)
			if code != tt.wantCode {
				t.Errorf("code = %v, want %v", code, tt.wantCode)
			}
			if conf != tt.wantConf {
				t.Errorf("conf = %v, want %v", conf, tt.wantConf)
			}
		})
	}
}
