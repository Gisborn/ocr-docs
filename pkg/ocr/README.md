# pkg/ocr

Интерфейсы и реализации OCR-провайдеров.

## Структура

```
ocr/
├── provider.go     # Интерфейс Provider и общие типы
├── yandex.go       # Yandex Vision (primary)
├── vk.go           # VK Vision (fallback) - заглушка для задачи 2-2
└── mock.go         # Моки для тестирования
```

## Использование

```go
// Создание провайдера
yandex := ocr.NewYandexVision(apiKey, folderID)

// Распознавание
result, err := yandex.Recognize(ctx, imageBytes)
if err != nil {
    var providerErr *ocr.ProviderError
    if errors.As(err, &providerErr) && providerErr.IsRetryable() {
        // Fallback на VK Vision
    }
}

// Доступ к полям
lastName := result.Fields["last_name"].Value
confidence := result.Fields["last_name"].Confidence
```

## Тестирование

```go
// Мок с фиксированным результатом
mock := ocr.NewMock("test", &ocr.Result{
    Fields: map[string]ocr.Field{
        "last_name": {Value: "Иванов", Confidence: 0.99},
    },
})

// Мок с ошибкой
errorMock := ocr.NewMockError("error", errors.New("fail"))

// Настраиваемый мок
configMock := ocr.NewConfigurableMock("config")
configMock.AddResult(ocr.Result{...})
configMock.AddError(errors.New("fail"))
```
