package ocr

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"scan.passport.local/api/pkg/ocr"
)

func TestMain(m *testing.M) {
	if os.Getenv("YANDEX_VISION_API_KEY") == "" {
		fmt.Println("SKIP: YANDEX_VISION_API_KEY not set, skipping OCR integration tests")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// TestYandexVisionV2_Integration тестирует Yandex Vision v2 passport model на реальных изображениях.
func TestYandexVisionV2_Integration(t *testing.T) {
	apiKey := os.Getenv("YANDEX_VISION_API_KEY")

	provider := ocr.NewYandexVisionV2(apiKey, ocr.ModelPassportRF)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	images := []struct {
		path      string
		minFields int
	}{
		{"../../fixtures/passports/good/Pasport_RF.jpg", 10},
		{"../../fixtures/passports/good/original.jpg", 10},
		{"../../fixtures/passports/good/b9534be85643dad95e8b74f0b4a7fa87.jpg", 9},
		{"../../fixtures/passports/good/vqxkqp1sp23nwwshszdk.jpg", 9},
	}

	for _, img := range images {
		t.Run(img.path, func(t *testing.T) {
			data, err := os.ReadFile(img.path)
			if err != nil {
				t.Fatalf("failed to read image: %v", err)
			}

			result, err := provider.Recognize(ctx, data)
			if err != nil {
				t.Fatalf("recognize failed: %v", err)
			}

			if len(result.Fields) < img.minFields {
				t.Errorf("expected at least %d fields, got %d", img.minFields, len(result.Fields))
			}

			// Проверяем обязательные поля
			required := []string{"last_name", "first_name", "birth_date", "series", "number", "issue_date"}
			for _, key := range required {
				if result.Fields[key].Value == "" {
					t.Errorf("required field %s is empty", key)
				}
			}

			// Проверяем формат серии и номера
			series := result.Fields["series"].Value
			if len(series) != 4 {
				t.Errorf("expected series length 4, got %d (%s)", len(series), series)
			}

			number := result.Fields["number"].Value
			if len(number) != 6 {
				t.Errorf("expected number length 6, got %d (%s)", len(number), number)
			}

			t.Logf("Fields: %d | LastName: %s | FirstName: %s | Series: %s | Number: %s",
				len(result.Fields),
				result.Fields["last_name"].Value,
				result.Fields["first_name"].Value,
				series,
				number,
			)
		})
	}
}
