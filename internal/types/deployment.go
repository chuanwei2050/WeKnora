package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type ModelProtocol string

const (
	ModelProtocolOllama           ModelProtocol = "ollama"
	ModelProtocolOpenAICompatible ModelProtocol = "openai-compatible"
	ModelProtocolNative           ModelProtocol = "native"
)

type EndpointLocation string

const (
	EndpointPublic         EndpointLocation = "public"
	EndpointPrivateNetwork EndpointLocation = "private-network"
	EndpointSameHost       EndpointLocation = "same-host"
	EndpointUnknown        EndpointLocation = "unknown"
)

type ArtifactPolicy string

const (
	ArtifactPreloadedOnly ArtifactPolicy = "preloaded-only"
	ArtifactAllowDownload ArtifactPolicy = "allow-download"
)

type AirGapDependency struct {
	Name     string                   `json:"name"`
	Endpoint string                   `json:"endpoint"`
	Category ApprovedEndpointCategory `json:"category"`
	Use      string                   `json:"use"`
	Location EndpointLocation         `json:"location"`
	Approved bool                     `json:"approved"`
}

// ValidateAirGappedDependencies is the shared startup/preflight gate for
// non-model outbound dependencies. It fails closed for public, unknown or
// unapproved targets and therefore can be reused by search, connectors,
// object storage and telemetry adapters.
func ValidateAirGappedDependencies(dependencies []AirGapDependency) error {
	return ValidateAirGappedDependenciesWithAllowlist(dependencies, nil)
}

// ValidateAirGappedDependenciesWithAllowlist validates both the tenant-facing
// endpoint declaration and the deployment-level SSRF allowlist. The
// allowlist callback is intentionally injected so the types package does not
// depend on the process environment or a network/security package.
func ValidateAirGappedDependenciesWithAllowlist(dependencies []AirGapDependency, allowlisted func(string) bool) error {
	for _, dependency := range dependencies {
		if strings.TrimSpace(dependency.Name) == "" || strings.TrimSpace(dependency.Endpoint) == "" {
			return fmt.Errorf("air-gapped dependency name and endpoint are required")
		}
		if err := ValidateAirGapEndpoint(dependency.Endpoint, dependency.Location, dependency.Approved, true); err != nil {
			return fmt.Errorf("air-gapped dependency %q: %w", dependency.Name, err)
		}
		switch dependency.Category {
		case EndpointCategoryModel, EndpointCategorySearch, EndpointCategoryDataConnector, EndpointCategoryObjectStorage, EndpointCategoryTelemetry:
		default:
			return fmt.Errorf("air-gapped dependency %q has unknown category %q", dependency.Name, dependency.Category)
		}
		if strings.TrimSpace(dependency.Use) == "" {
			return fmt.Errorf("air-gapped dependency %q must declare category and use", dependency.Name)
		}
		if !isApprovedEndpointUse(dependency.Category, strings.ToLower(strings.TrimSpace(dependency.Use))) {
			return fmt.Errorf("air-gapped dependency %q has unknown use %q for category %q", dependency.Name, dependency.Use, dependency.Category)
		}
		if allowlisted != nil {
			scheme, host, _, err := NormalizeEndpoint(dependency.Endpoint)
			if err != nil {
				return fmt.Errorf("air-gapped dependency %q: %w", dependency.Name, err)
			}
			_ = scheme
			if !allowlisted(host) {
				return fmt.Errorf("air-gapped dependency %q is not in the deployment SSRF allowlist", dependency.Name)
			}
		}
	}
	return nil
}

type ModelRole string

const (
	ModelRoleChat            ModelRole = "chat"
	ModelRoleEmbedding       ModelRole = "embedding"
	ModelRoleRerank          ModelRole = "rerank"
	ModelRoleVLM             ModelRole = "vlm"
	ModelRoleASR             ModelRole = "asr"
	ModelRoleTTS             ModelRole = "tts"
	ModelRoleVerifier        ModelRole = "verifier"
	ModelRoleEvaluationJudge ModelRole = "evaluation_judge"
	ModelRoleParserOCR       ModelRole = "parser_ocr"
)

type ModelRoleArray []ModelRole

func (a ModelRoleArray) Value() (driver.Value, error) { return json.Marshal(a) }

func (a *ModelRoleArray) Scan(value interface{}) error {
	if value == nil {
		*a = nil
		return nil
	}
	var data []byte
	switch typed := value.(type) {
	case []byte:
		data = typed
	case string:
		data = []byte(typed)
	default:
		return fmt.Errorf("unsupported model role array value %T", value)
	}
	return json.Unmarshal(data, a)
}

type ModelCapabilityManifest struct {
	Roles              []ModelRole      `json:"roles"`
	Protocol           ModelProtocol    `json:"protocol"`
	Location           EndpointLocation `json:"location"`
	ArtifactPolicy     ArtifactPolicy   `json:"artifact_policy"`
	Streaming          bool             `json:"streaming"`
	StructuredOutput   bool             `json:"structured_output"`
	ToolCalling        bool             `json:"tool_calling"`
	VisionInput        bool             `json:"vision_input"`
	AudioInput         bool             `json:"audio_input"`
	AudioOutput        bool             `json:"audio_output"`
	DocumentParsing    bool             `json:"document_parsing"`
	MaxContextTokens   int              `json:"max_context_tokens"`
	EmbeddingDimension int              `json:"embedding_dimension"`
	MaxConcurrency     int              `json:"max_concurrency"`
	TimeoutSeconds     int              `json:"timeout_seconds"`
}

type CapabilityProbeStatus string

const (
	CapabilityProbePassed          CapabilityProbeStatus = "passed"
	CapabilityProbeUnsupported     CapabilityProbeStatus = "unsupported"
	CapabilityProbeMissingResource CapabilityProbeStatus = "missing_resource"
	CapabilityProbeFailed          CapabilityProbeStatus = "failed"
)

type ModelCapabilityProbeResult struct {
	Role           ModelRole              `json:"role"`
	Status         CapabilityProbeStatus  `json:"status"`
	LatencyMs      int64                  `json:"latency_ms,omitempty"`
	Error          string                 `json:"error,omitempty"`
	ModelKey       string                 `json:"model_key,omitempty"`
	ObservedModel  string                 `json:"observed_model,omitempty"`
	ObservedValues map[string]interface{} `json:"observed_values,omitempty"`
	CheckedAt      time.Time              `json:"checked_at"`
}

type ModelPreflightResult struct {
	ModelID   string                       `json:"model_id"`
	ModelName string                       `json:"model_name"`
	Location  EndpointLocation             `json:"location"`
	Protocol  ModelProtocol                `json:"protocol"`
	Probes    []ModelCapabilityProbeResult `json:"probes"`
	Checks    []ModelPreflightCheckResult  `json:"checks,omitempty"`
	CheckedAt time.Time                    `json:"checked_at"`
}

type PreflightCheckStatus string

const (
	PreflightCheckPassed          PreflightCheckStatus = "passed"
	PreflightCheckSkipped         PreflightCheckStatus = "skipped"
	PreflightCheckFailed          PreflightCheckStatus = "failed"
	PreflightCheckMissingResource PreflightCheckStatus = "missing_resource"
)

type ModelPreflightCheckResult struct {
	Name      string                 `json:"name"`
	Status    PreflightCheckStatus   `json:"status"`
	Details   map[string]interface{} `json:"details,omitempty"`
	Error     string                 `json:"error,omitempty"`
	CheckedAt time.Time              `json:"checked_at"`
}

func (m ModelCapabilityManifest) ValidateProbe(role ModelRole, probe ModelCapabilityProbeResult) error {
	if probe.Role != role {
		return fmt.Errorf("probe role %q does not match requested role %q", probe.Role, role)
	}
	if err := m.ValidateRole(role); err != nil {
		return err
	}
	if probe.Status != CapabilityProbePassed {
		return fmt.Errorf("capability probe for %s did not pass: %s", role, probe.Status)
	}
	return nil
}

func (m ModelCapabilityManifest) Supports(role ModelRole) bool {
	for _, candidate := range m.Roles {
		if candidate == role {
			return true
		}
	}
	return false
}

func (m ModelCapabilityManifest) ValidateRole(role ModelRole) error {
	if !m.Supports(role) {
		return fmt.Errorf("model does not declare role %q", role)
	}
	switch role {
	case ModelRoleEmbedding:
		if m.EmbeddingDimension <= 0 {
			return fmt.Errorf("embedding model requires a positive embedding dimension")
		}
	case ModelRoleChat:
		if !m.Streaming {
			return fmt.Errorf("chat model requires streaming capability")
		}
	case ModelRoleVerifier, ModelRoleEvaluationJudge:
		if !m.StructuredOutput {
			return fmt.Errorf("%s requires structured output", role)
		}
	case ModelRoleTTS:
		if !m.AudioOutput {
			return fmt.Errorf("tts model requires audio output")
		}
	case ModelRoleASR:
		if !m.AudioInput {
			return fmt.Errorf("asr model requires audio input")
		}
	case ModelRoleVLM:
		if !m.VisionInput {
			return fmt.Errorf("vlm model requires vision input")
		}
	case ModelRoleParserOCR:
		if !m.DocumentParsing {
			return fmt.Errorf("parser/ocr model requires document parsing capability")
		}
	}
	return nil
}

type ApprovedEndpointCategory string

const (
	EndpointCategoryModel         ApprovedEndpointCategory = "model"
	EndpointCategorySearch        ApprovedEndpointCategory = "search"
	EndpointCategoryDataConnector ApprovedEndpointCategory = "data-connector"
	EndpointCategoryObjectStorage ApprovedEndpointCategory = "object-storage"
	EndpointCategoryTelemetry     ApprovedEndpointCategory = "telemetry"
)

type ApprovedEndpoint struct {
	ID                string                   `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID          uint64                   `json:"tenant_id" gorm:"index"`
	Scheme            string                   `json:"scheme"`
	Host              string                   `json:"host"`
	Port              int                      `json:"port"`
	Protocol          string                   `json:"protocol"`
	TLSRequired       bool                     `json:"tls_required"`
	Category          ApprovedEndpointCategory `json:"category"`
	AllowedUses       StringArray              `json:"allowed_uses" gorm:"type:json"`
	AllowedModelRoles ModelRoleArray           `json:"allowed_model_roles" gorm:"type:json"`
	CreatedBy         string                   `json:"created_by"`
	CreatedAt         time.Time                `json:"created_at"`
	UpdatedAt         time.Time                `json:"updated_at"`
}

type ApprovedEndpointAudit struct {
	ID         string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID   uint64    `json:"tenant_id" gorm:"index"`
	EndpointID string    `json:"endpoint_id" gorm:"index"`
	ActorID    string    `json:"actor_id"`
	Action     string    `json:"action"`
	BeforeJSON string    `json:"before_json,omitempty" gorm:"type:text"`
	AfterJSON  string    `json:"after_json,omitempty" gorm:"type:text"`
	CreatedAt  time.Time `json:"created_at"`
}

func (e ApprovedEndpoint) Validate() error {
	if e.TenantID == 0 || strings.TrimSpace(e.Scheme) == "" || strings.TrimSpace(e.Host) == "" || e.Port < 1 || e.Port > 65535 {
		return fmt.Errorf("approved endpoint tenant, scheme, host and valid port are required")
	}
	if !strings.EqualFold(strings.TrimSpace(e.Scheme), "http") && !strings.EqualFold(strings.TrimSpace(e.Scheme), "https") {
		return fmt.Errorf("approved endpoint scheme must be http or https")
	}
	scheme, host, port, err := NormalizeEndpoint(fmt.Sprintf("%s://%s:%d", e.Scheme, e.Host, e.Port))
	if err != nil || scheme != strings.ToLower(strings.TrimSpace(e.Scheme)) || host != strings.ToLower(strings.TrimSpace(e.Host)) || port != e.Port {
		return fmt.Errorf("approved endpoint destination is invalid")
	}
	switch e.Category {
	case EndpointCategoryModel, EndpointCategorySearch, EndpointCategoryDataConnector, EndpointCategoryObjectStorage, EndpointCategoryTelemetry:
	default:
		return fmt.Errorf("unknown approved endpoint category %q", e.Category)
	}
	if len(e.AllowedUses) == 0 {
		return fmt.Errorf("approved endpoint must declare allowed uses")
	}
	seenUses := make(map[string]struct{}, len(e.AllowedUses))
	for _, rawUse := range e.AllowedUses {
		use := strings.ToLower(strings.TrimSpace(rawUse))
		if !isApprovedEndpointUse(e.Category, use) {
			return fmt.Errorf("unknown use %q for approved endpoint category %q", rawUse, e.Category)
		}
		if _, exists := seenUses[use]; exists {
			return fmt.Errorf("duplicate use %q for approved endpoint category %q", rawUse, e.Category)
		}
		seenUses[use] = struct{}{}
	}
	return nil
}

func isApprovedEndpointUse(category ApprovedEndpointCategory, use string) bool {
	allowed := map[ApprovedEndpointCategory]map[string]struct{}{
		EndpointCategoryModel: {
			"model": {}, "chat": {}, "embedding": {}, "rerank": {}, "vlm": {},
			"asr": {}, "tts": {}, "verifier": {}, "reflection": {}, "judge": {},
			"parser": {}, "ocr": {},
		},
		EndpointCategorySearch:        {"query": {}},
		EndpointCategoryDataConnector: {"sync": {}, "vector-store": {}, "document-reader": {}},
		EndpointCategoryObjectStorage: {"object-storage": {}},
		EndpointCategoryTelemetry:     {"telemetry": {}},
	}
	_, ok := allowed[category][use]
	return ok
}

func (e ApprovedEndpoint) ValidateUse(category ApprovedEndpointCategory, use string) error {
	if e.Category != category {
		return fmt.Errorf("endpoint category %q cannot be used as %q", e.Category, category)
	}
	for _, allowed := range e.AllowedUses {
		if strings.EqualFold(strings.TrimSpace(allowed), strings.TrimSpace(use)) {
			return nil
		}
	}
	return fmt.Errorf("endpoint is not approved for use %q", use)
}

// ValidateConnection verifies the normalized destination and purpose on every
// connection. It intentionally accepts resolved IPs from the current dial
// attempt so DNS rebinding cannot reuse a stale approval decision.
func (e ApprovedEndpoint) ValidateConnection(raw string, category ApprovedEndpointCategory, use string, resolvedIPs []net.IP, airGapped bool) error {
	if err := e.ValidateUse(category, use); err != nil {
		return err
	}
	scheme, host, port, err := NormalizeEndpoint(raw)
	if err != nil {
		return err
	}
	if scheme != strings.ToLower(strings.TrimSpace(e.Scheme)) || host != strings.ToLower(strings.TrimSpace(e.Host)) || port != e.Port {
		return fmt.Errorf("endpoint does not match the approved destination")
	}
	if e.TLSRequired && scheme != "https" {
		return fmt.Errorf("approved endpoint requires TLS")
	}
	return ValidateAirGapEndpoint(raw, DeriveEndpointLocation(raw, resolvedIPs), true, airGapped)
}

// ValidateDeploymentAllowlist checks the deployment-level SSRF allowlist for
// an approved endpoint. It is only required for strict offline operation;
// public online model providers keep using the existing SSRF policy.
func (e ApprovedEndpoint) ValidateDeploymentAllowlist(allowlisted func(string) bool, resolvedIPs []net.IP, airGapped bool) error {
	if !airGapped {
		return nil
	}
	if allowlisted == nil {
		return fmt.Errorf("air-gapped approved endpoints require a deployment SSRF allowlist")
	}
	if allowlisted(e.Host) {
		return nil
	}
	for _, ip := range resolvedIPs {
		if ip != nil && allowlisted(ip.String()) {
			return nil
		}
	}
	return fmt.Errorf("approved endpoint %q is not in the deployment SSRF allowlist", e.Host)
}

func NormalizeEndpoint(raw string) (scheme, host string, port int, err error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", 0, err
	}
	if parsed.Scheme == "" || parsed.Hostname() == "" {
		return "", "", 0, fmt.Errorf("endpoint must include scheme and host")
	}
	scheme = strings.ToLower(parsed.Scheme)
	host = strings.ToLower(parsed.Hostname())
	if parsed.Port() != "" {
		port, err = strconv.Atoi(parsed.Port())
		if err != nil || port < 1 || port > 65535 {
			return "", "", 0, fmt.Errorf("invalid endpoint port")
		}
	} else if scheme == "https" {
		port = 443
	} else {
		port = 80
	}
	return scheme, host, port, nil
}

func DeriveEndpointLocation(raw string, resolvedIPs []net.IP) EndpointLocation {
	_, host, _, err := NormalizeEndpoint(raw)
	if err != nil {
		return EndpointUnknown
	}
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return EndpointSameHost
	}
	if len(resolvedIPs) == 0 {
		if ip := net.ParseIP(host); ip != nil {
			resolvedIPs = []net.IP{ip}
		}
	}
	if len(resolvedIPs) == 0 {
		return EndpointUnknown
	}
	for _, ip := range resolvedIPs {
		if !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
			return EndpointPublic
		}
	}
	return EndpointPrivateNetwork
}

func ValidateAirGapEndpoint(raw string, location EndpointLocation, approved bool, airGapped bool) error {
	if _, _, _, err := NormalizeEndpoint(raw); err != nil {
		return err
	}
	if !airGapped {
		return nil
	}
	if location == EndpointPublic || location == EndpointUnknown {
		return fmt.Errorf("air-gapped mode rejects non-private endpoint")
	}
	if !approved {
		return fmt.Errorf("air-gapped mode requires an approved endpoint")
	}
	return nil
}

// NormalizeLegacyModelDeployment upgrades the historical source=local API
// shape to explicit Ollama/same-host metadata. It is deliberately pure so old
// API payloads can be migrated and tested before persistence.
func NormalizeLegacyModelDeployment(model *Model, airGapped bool) error {
	if model == nil {
		return fmt.Errorf("model is required")
	}
	if model.Source == ModelSourceLocal {
		if model.Parameters.Protocol == "" {
			model.Parameters.Protocol = ModelProtocolOllama
		}
		if model.Parameters.Location == "" || model.Parameters.Location == EndpointUnknown {
			model.Parameters.Location = EndpointSameHost
		}
		if airGapped || model.Parameters.ArtifactPolicy == "" {
			model.Parameters.ArtifactPolicy = ArtifactPreloadedOnly
		}
	} else if model.Parameters.Protocol == "" {
		model.Parameters.Protocol = ModelProtocolOpenAICompatible
	}
	if model.Parameters.BaseURL != "" && (model.Parameters.Location == "" || model.Parameters.Location == EndpointUnknown) {
		model.Parameters.Location = DeriveEndpointLocation(model.Parameters.BaseURL, nil)
	}
	if airGapped && (model.Parameters.Location == EndpointPublic || model.Parameters.Location == EndpointUnknown) {
		return fmt.Errorf("air-gapped mode rejects public or unknown model location")
	}
	return nil
}
