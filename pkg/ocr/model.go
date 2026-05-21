package ocr

// DocumentModel тип модели распознавания для Yandex Vision OCR v2
type DocumentModel string

const (
	// Модели распознавания текста (generic)
	ModelPage           DocumentModel = "page"
	ModelPageColumnSort DocumentModel = "page-column-sort"
	ModelHandwritten    DocumentModel = "handwritten"
	ModelTable          DocumentModel = "table"
	ModelMarkdown       DocumentModel = "markdown"
	ModelMathMarkdown   DocumentModel = "math-markdown"

	// Модели распознавания шаблонных документов (structured)
	ModelPassportRF            DocumentModel = "passport"
	ModelDriverLicenseFront    DocumentModel = "driver-license-front"
	ModelDriverLicenseBack     DocumentModel = "driver-license-back"
	ModelVehicleRegFront       DocumentModel = "vehicle-registration-front"
	ModelVehicleRegBack        DocumentModel = "vehicle-registration-back"
	ModelLicensePlates         DocumentModel = "license-plates"
)

// IsStructured возвращает true, если модель возвращает структурированные entities
func (m DocumentModel) IsStructured() bool {
	switch m {
	case ModelPassportRF, ModelDriverLicenseFront, ModelDriverLicenseBack,
		ModelVehicleRegFront, ModelVehicleRegBack, ModelLicensePlates:
		return true
	default:
		return false
	}
}

// IsValid проверяет, что модель допустима
func (m DocumentModel) IsValid() bool {
	switch m {
	case ModelPage, ModelPageColumnSort, ModelHandwritten, ModelTable,
		ModelMarkdown, ModelMathMarkdown,
		ModelPassportRF, ModelDriverLicenseFront, ModelDriverLicenseBack,
		ModelVehicleRegFront, ModelVehicleRegBack, ModelLicensePlates:
		return true
	default:
		return false
	}
}
