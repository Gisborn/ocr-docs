package ocr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestYandexVisionV2_Recognize_PassportModel(t *testing.T) {
	// Мок-сервер, имитирующий Yandex Vision OCR v2
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Api-Key test-key" {
			t.Errorf("expected Authorization header with Api-Key")
		}

		var req yandexV2Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		// Проверяем, что модель правильная
		if req.Model != "passport" {
			t.Errorf("expected model=passport, got %s", req.Model)
		}

		resp := yandexV2Response{
			Result: &yandexV2Result{
				TextAnnotation: &yandexV2TextAnnotation{
					Width:  "983",
					Height: "1360",
					Entities: []yandexV2Entity{
						{Name: "citizenship", Text: "rus"},
						{Name: "expiration_date", Text: "-"},
						{Name: "gender", Text: "муж"},
						{Name: "issue_date", Text: "17.12.2004"},
						{Name: "subdivision", Text: "292-000"},
						{Name: "issued_by", Text: "овд октябрьского округа г. архангельска"},
						{Name: "surname", Text: "имярек"},
						{Name: "name", Text: "евгений"},
						{Name: "middle_name", Text: "александрович"},
						{Name: "birth_date", Text: "12.09.1982"},
						{Name: "birth_place", Text: "гор. архангельск"},
						{Name: "number", Text: "1104000000"},
					},
					FullText: "",
					Rotate:   "ANGLE_0",
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewYandexVisionV2("test-key", ModelPassportRF)
	provider.endpoint = server.URL

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := provider.Recognize(ctx, []byte("fake-image-data"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Проверяем количество полей
	if len(result.Fields) != 12 {
		t.Errorf("expected 12 fields, got %d", len(result.Fields))
	}

	// Проверяем конкретные поля
	assertField(t, result.Fields, "last_name", "Имярек")
	assertField(t, result.Fields, "first_name", "Евгений")
	assertField(t, result.Fields, "middle_name", "Александрович")
	assertField(t, result.Fields, "birth_date", "12.09.1982")
	assertField(t, result.Fields, "birth_place", "гор. архангельск")
	assertField(t, result.Fields, "series", "1104")
	assertField(t, result.Fields, "number", "000000")
	assertField(t, result.Fields, "issue_date", "17.12.2004")
	assertField(t, result.Fields, "division_code", "292-000")
	assertField(t, result.Fields, "issued_by", "овд октябрьского округа г. архангельска")
	assertField(t, result.Fields, "gender", "муж")
	assertField(t, result.Fields, "citizenship", "rus")
}

func TestYandexVisionV2_Recognize_FallbackToGeneric(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		var req yandexV2Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.Model == "passport" {
			// Structured model возвращает мало полей — должно вызвать fallback
			resp := yandexV2Response{
				Result: &yandexV2Result{
					TextAnnotation: &yandexV2TextAnnotation{
						Width:  "772",
						Height: "1022",
						Entities: []yandexV2Entity{
							{Name: "citizenship", Text: "rus"},
							// Только 1 поле — недостаточно
						},
						Blocks: []yandexV2Block{
							{
								Lines: []yandexV2Line{
									{Text: "ЖАДАН"},
									{Text: "СТАНИСЛАВ"},
									{Text: "ВЛАДИМИРОВИЧ"},
									{Text: "19.08.1991"},
									{Text: "24.07.2014"},
									{Text: "6014 620859"},
									{Text: "610-043"},
								},
							},
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(resp)
			return
		}

		if req.Model == "page" {
			// Generic model
			resp := yandexV2Response{
				Result: &yandexV2Result{
					TextAnnotation: &yandexV2TextAnnotation{
						Width:  "772",
						Height: "1022",
						Blocks: []yandexV2Block{
							{
								Lines: []yandexV2Line{
									{Text: "РОССИЙСКАЯ ФЕДЕРАЦИЯ"},
									{Text: "ЖАДАН"},
									{Text: "СТАНИСЛАВ"},
									{Text: "ВЛАДИМИРОВИЧ"},
									{Text: "19.08.1991"},
									{Text: "24.07.2014"},
									{Text: "6014 620859"},
									{Text: "610-043"},
								},
							},
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(resp)
			return
		}

		t.Errorf("unexpected model: %s", req.Model)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	provider := NewYandexVisionV2("test-key", ModelPassportRF)
	provider.endpoint = server.URL

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := provider.Recognize(ctx, []byte("fake-image-data"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Должно быть 2 вызова: passport + page
	if callCount != 2 {
		t.Errorf("expected 2 API calls (passport + page fallback), got %d", callCount)
	}

	// Fallback должен извлечь поля из generic OCR
	if result.Fields["last_name"].Value != "Жадан" {
		t.Errorf("expected last_name=Жадан from fallback, got %s", result.Fields["last_name"].Value)
	}
	if result.Fields["first_name"].Value != "Станислав" {
		t.Errorf("expected first_name=Станислав from fallback, got %s", result.Fields["first_name"].Value)
	}
}

func TestYandexVisionV2_Recognize_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":{"message":"internal error"}}`)
	}))
	defer server.Close()

	provider := NewYandexVisionV2("test-key", ModelPassportRF)
	provider.endpoint = server.URL

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := provider.Recognize(ctx, []byte("fake-image-data"))
	if err == nil {
		t.Fatal("expected error for 500 response")
	}

	providerErr, ok := err.(*ProviderError)
	if !ok {
		t.Fatalf("expected ProviderError, got %T", err)
	}

	if providerErr.Type != ErrorTypeAPI {
		t.Errorf("expected error type API, got %s", providerErr.Type)
	}

	if result != nil {
		t.Error("expected nil result on error")
	}
}

func TestYandexVisionV2_Recognize_RateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprintf(w, `{"error":{"message":"rate limit exceeded"}}`)
	}))
	defer server.Close()

	provider := NewYandexVisionV2("test-key", ModelPassportRF)
	provider.endpoint = server.URL

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := provider.Recognize(ctx, []byte("fake-image-data"))
	if err == nil {
		t.Fatal("expected error for 429 response")
	}

	providerErr, ok := err.(*ProviderError)
	if !ok {
		t.Fatalf("expected ProviderError, got %T", err)
	}

	if providerErr.Type != ErrorTypeRateLimit {
		t.Errorf("expected error type rate_limit, got %s", providerErr.Type)
	}

	if !providerErr.IsRetryable() {
		t.Error("expected rate limit error to be retryable")
	}
}

func TestYandexVisionV2_Recognize_AuthError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintf(w, `{"error":{"message":"unauthorized"}}`)
	}))
	defer server.Close()

	provider := NewYandexVisionV2("test-key", ModelPassportRF)
	provider.endpoint = server.URL

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := provider.Recognize(ctx, []byte("fake-image-data"))
	if err == nil {
		t.Fatal("expected error for 401 response")
	}

	providerErr, ok := err.(*ProviderError)
	if !ok {
		t.Fatalf("expected ProviderError, got %T", err)
	}

	if providerErr.Type != ErrorTypeAuth {
		t.Errorf("expected error type auth, got %s", providerErr.Type)
	}

	if providerErr.IsRetryable() {
		t.Error("expected auth error to be non-retryable")
	}
}

func TestYandexVisionV2_Name(t *testing.T) {
	provider := NewYandexVisionV2("test-key", ModelPassportRF)
	expected := "yandex-vision-v2/passport"
	if provider.Name() != expected {
		t.Errorf("expected name %s, got %s", expected, provider.Name())
	}
}

func TestDocumentModel_IsStructured(t *testing.T) {
	tests := []struct {
		model    DocumentModel
		expected bool
	}{
		{ModelPassportRF, true},
		{ModelDriverLicenseFront, true},
		{ModelDriverLicenseBack, true},
		{ModelVehicleRegFront, true},
		{ModelVehicleRegBack, true},
		{ModelLicensePlates, true},
		{ModelPage, false},
		{ModelPageColumnSort, false},
		{ModelHandwritten, false},
		{ModelTable, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.model), func(t *testing.T) {
			if got := tt.model.IsStructured(); got != tt.expected {
				t.Errorf("IsStructured() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDocumentModel_IsValid(t *testing.T) {
	validModels := []DocumentModel{
		ModelPage, ModelPageColumnSort, ModelHandwritten, ModelTable,
		ModelMarkdown, ModelMathMarkdown,
		ModelPassportRF, ModelDriverLicenseFront, ModelDriverLicenseBack,
		ModelVehicleRegFront, ModelVehicleRegBack, ModelLicensePlates,
	}

	for _, m := range validModels {
		t.Run(string(m), func(t *testing.T) {
			if !m.IsValid() {
				t.Errorf("expected model %s to be valid", m)
			}
		})
	}

	invalid := DocumentModel("invalid-model")
	if invalid.IsValid() {
		t.Error("expected invalid model to be invalid")
	}
}

func assertField(t *testing.T, fields map[string]Field, key, expected string) {
	t.Helper()
	f, ok := fields[key]
	if !ok {
		t.Errorf("expected field %s to be present", key)
		return
	}
	if f.Value != expected {
		t.Errorf("field %s: expected %q, got %q", key, expected, f.Value)
	}
}
