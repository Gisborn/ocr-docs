package ocr

import (
	"context"
	"fmt"
	"time"
)

const (
	vkVisionEndpoint     = "https://vision.api.cloud.vk.com/vision/v1/batchAnalyze"
	vkDefaultTimeout     = 30 * time.Second
)

// VKVision клиент для VK Vision API
type VKVision struct {
	apiKey     string
	httpClient *httpClient
}

// httpClient интерфейс для тестирования
type httpClient interface {
	Do(req interface{}) (interface{}, error)
}

// NewVKVision создает новый клиент VK Vision
func NewVKVision(apiKey string) *VKVision {
	return &VKVision{
		apiKey: apiKey,
	}
}

func (v *VKVision) Name() string {
	return "vk-vision"
}

// Recognize отправляет изображение в VK Vision API
// TODO: реализовать аналогично YandexVision (задача 2-2)
func (v *VKVision) Recognize(ctx context.Context, image []byte) (*Result, error) {
	// Заглушка для задачи 2-2
	return nil, &ProviderError{
		Provider: v.Name(),
		Type:     ErrorTypeUnknown,
		Message:  "not implemented yet (task 2-2)",
	}
}

// Структуры для VK Vision API (TODO: заполнить в задаче 2-2)
type vkRequest struct {
	// TODO: определить структуру запроса VK Vision
}

type vkResponse struct {
	// TODO: определить структуру ответа VK Vision
}
