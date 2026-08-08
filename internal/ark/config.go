// Package ark owns the code-fixed Volcengine Ark inference identity used by
// Jarvis model transports. These values are intentionally committed in plain
// text for this trusted single-user deployment.
package ark

const (
	BaseURL = "https://ark.cn-beijing.volces.com/api/coding/v3"
	APIKey  = "ark-5b62a13c-f99e-438f-aaf0-bca14077536d-abbf8"

	EmbeddingModel      = "doubao-embedding-vision-251215"
	EmbeddingDimensions = 1024
)
