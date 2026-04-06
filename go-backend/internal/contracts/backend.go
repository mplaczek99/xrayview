package contracts

type MeasurementScale struct {
	RowSpacingMM    float64 `json:"rowSpacingMm"`
	ColumnSpacingMM float64 `json:"columnSpacingMm"`
	Source          string  `json:"source"`
}

type OpenStudyCommand struct {
	InputPath string `json:"inputPath"`
}

type StudyRecord struct {
	StudyID          string            `json:"studyId"`
	InputPath        string            `json:"inputPath"`
	InputName        string            `json:"inputName"`
	MeasurementScale *MeasurementScale `json:"measurementScale,omitempty"`
}

type OpenStudyCommandResult struct {
	Study StudyRecord `json:"study"`
}

type BackendErrorCode string

const (
	BackendErrorCodeInvalidInput   BackendErrorCode = "invalidInput"
	BackendErrorCodeNotFound       BackendErrorCode = "notFound"
	BackendErrorCodeCancelled      BackendErrorCode = "cancelled"
	BackendErrorCodeConflict       BackendErrorCode = "conflict"
	BackendErrorCodeCacheCorrupted BackendErrorCode = "cacheCorrupted"
	BackendErrorCodeInternal       BackendErrorCode = "internal"
)

type BackendError struct {
	Code        BackendErrorCode `json:"code"`
	Message     string           `json:"message"`
	Details     []string         `json:"details,omitempty"`
	Recoverable bool             `json:"recoverable"`
}

func NewBackendError(code BackendErrorCode, message string) BackendError {
	return BackendError{
		Code:        code,
		Message:     message,
		Recoverable: code != BackendErrorCodeInternal,
	}
}

func InvalidInput(message string) BackendError {
	return NewBackendError(BackendErrorCodeInvalidInput, message)
}

func NotFound(message string) BackendError {
	return NewBackendError(BackendErrorCodeNotFound, message)
}

func Cancelled(message string) BackendError {
	return NewBackendError(BackendErrorCodeCancelled, message)
}

func Conflict(message string) BackendError {
	return NewBackendError(BackendErrorCodeConflict, message)
}

func CacheCorrupted(message string) BackendError {
	return NewBackendError(BackendErrorCodeCacheCorrupted, message)
}

func Internal(message string) BackendError {
	return NewBackendError(BackendErrorCodeInternal, message)
}

func (err BackendError) Error() string {
	return err.Message
}

func (err BackendError) WithDetails(details ...string) BackendError {
	err.Details = append([]string(nil), details...)
	return err
}
