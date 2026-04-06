package jobs

type Kind string

const (
	RenderStudyKind  Kind = "renderStudy"
	ProcessStudyKind Kind = "processStudy"
	AnalyzeStudyKind Kind = "analyzeStudy"
)

type Service struct {
	supportedKinds []Kind
}

func New() *Service {
	return &Service{
		supportedKinds: []Kind{
			RenderStudyKind,
			ProcessStudyKind,
			AnalyzeStudyKind,
		},
	}
}

func (service *Service) SupportedKinds() []string {
	kinds := make([]string, 0, len(service.supportedKinds))
	for _, kind := range service.supportedKinds {
		kinds = append(kinds, string(kind))
	}

	return kinds
}
