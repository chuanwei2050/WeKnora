package types

import (
	"net"
	"strings"
	"testing"
)

func TestPrivateEndpointCannotBeUsedWithoutApprovalInAirGap(t *testing.T) {
	if err := ValidateAirGapEndpoint("http://10.0.0.5:8080/v1", EndpointPrivateNetwork, false, true); err == nil {
		t.Fatal("expected unapproved endpoint to be rejected")
	}
	if err := ValidateAirGapEndpoint("http://10.0.0.5:8080/v1", EndpointPrivateNetwork, true, true); err != nil {
		t.Fatal(err)
	}
}

func TestDeriveEndpointLocation(t *testing.T) {
	if got := DeriveEndpointLocation("http://localhost:11434", nil); got != EndpointSameHost {
		t.Fatalf("got %s", got)
	}
	if got := DeriveEndpointLocation("http://model.internal:8080", []net.IP{net.ParseIP("10.1.2.3")}); got != EndpointPrivateNetwork {
		t.Fatalf("got %s", got)
	}
	if got := DeriveEndpointLocation("https://model.example", []net.IP{net.ParseIP("203.0.113.10")}); got != EndpointPublic {
		t.Fatalf("got %s", got)
	}
}

func TestApprovedEndpointMatchesEveryConnectionTarget(t *testing.T) {
	endpoint := ApprovedEndpoint{Scheme: "https", Host: "model.internal", Port: 443, Category: EndpointCategoryModel, AllowedUses: StringArray{"chat"}, TLSRequired: true}
	if err := endpoint.ValidateConnection("https://model.internal/v1", EndpointCategoryModel, "chat", []net.IP{net.ParseIP("10.0.0.5")}, true); err != nil {
		t.Fatal(err)
	}
	if err := endpoint.ValidateConnection("http://model.internal:443/v1", EndpointCategoryModel, "chat", []net.IP{net.ParseIP("10.0.0.5")}, true); err == nil {
		t.Fatal("expected TLS downgrade to be rejected")
	}
	if err := endpoint.ValidateConnection("https://model.internal/v1", EndpointCategoryModel, "embedding", []net.IP{net.ParseIP("10.0.0.5")}, true); err == nil {
		t.Fatal("expected cross-purpose endpoint use to be rejected")
	}
}

func TestCapabilityManifestValidatesRoleSpecificRequirements(t *testing.T) {
	base := ModelCapabilityManifest{Roles: []ModelRole{ModelRoleEmbedding}}
	if err := base.ValidateRole(ModelRoleEmbedding); err == nil {
		t.Fatal("expected embedding dimension requirement")
	}
	base.EmbeddingDimension = 768
	if err := base.ValidateRole(ModelRoleEmbedding); err != nil {
		t.Fatal(err)
	}

	parser := ModelCapabilityManifest{Roles: []ModelRole{ModelRoleParserOCR}}
	if err := parser.ValidateRole(ModelRoleParserOCR); err == nil {
		t.Fatal("expected document parsing requirement")
	}
	parser.DocumentParsing = true
	if err := parser.ValidateRole(ModelRoleParserOCR); err != nil {
		t.Fatal(err)
	}
}

func TestAirGappedDependencyGateFailsClosed(t *testing.T) {
	dependency := AirGapDependency{Name: "search", Endpoint: "http://search.internal:8080", Category: EndpointCategorySearch, Use: "query", Location: EndpointPrivateNetwork, Approved: true}
	if err := ValidateAirGappedDependencies([]AirGapDependency{dependency}); err != nil {
		t.Fatal(err)
	}
	dependency.Location = EndpointUnknown
	if err := ValidateAirGappedDependencies([]AirGapDependency{dependency}); err == nil {
		t.Fatal("expected unknown dependency location to fail closed")
	}
}

func TestAirGappedDependencyGateRejectsUnknownCategoryAndPublicTarget(t *testing.T) {
	dependency := AirGapDependency{Name: "unknown", Endpoint: "http://service.internal:8080", Category: "unknown", Use: "query", Location: EndpointPrivateNetwork, Approved: true}
	if err := ValidateAirGappedDependencies([]AirGapDependency{dependency}); err == nil {
		t.Fatal("expected unknown dependency category to fail")
	}
	dependency.Category = EndpointCategorySearch
	dependency.Location = EndpointPublic
	if err := ValidateAirGappedDependencies([]AirGapDependency{dependency}); err == nil {
		t.Fatal("expected public dependency to fail in air-gapped mode")
	}
}

func TestAirGappedDependencyGateRejectsCrossPurposeUse(t *testing.T) {
	dependency := AirGapDependency{
		Name:     "private-search",
		Endpoint: "http://search.internal:8080",
		Category: EndpointCategorySearch,
		Use:      "telemetry",
		Location: EndpointPrivateNetwork,
		Approved: true,
	}
	if err := ValidateAirGappedDependencies([]AirGapDependency{dependency}); err == nil {
		t.Fatal("expected cross-purpose dependency use to fail")
	}
}

func TestAirGappedDependencyGateAcceptsApprovedUsesWithAllowlist(t *testing.T) {
	dependencies := []AirGapDependency{
		{Name: "search", Endpoint: "http://search.internal:8080", Category: EndpointCategorySearch, Use: "query", Location: EndpointPrivateNetwork, Approved: true},
		{Name: "connector", Endpoint: "http://connector.internal:8081", Category: EndpointCategoryDataConnector, Use: "sync", Location: EndpointPrivateNetwork, Approved: true},
		{Name: "storage", Endpoint: "https://storage.internal:443", Category: EndpointCategoryObjectStorage, Use: "object-storage", Location: EndpointPrivateNetwork, Approved: true},
		{Name: "telemetry", Endpoint: "https://telemetry.internal:443", Category: EndpointCategoryTelemetry, Use: "telemetry", Location: EndpointPrivateNetwork, Approved: true},
	}
	allowlisted := func(value string) bool {
		return strings.HasSuffix(value, ".internal")
	}
	if err := ValidateAirGappedDependenciesWithAllowlist(dependencies, allowlisted); err != nil {
		t.Fatalf("expected approved uses to pass: %v", err)
	}
}

func TestApprovedEndpointRequiresDeploymentAllowlistInAirGap(t *testing.T) {
	endpoint := ApprovedEndpoint{Host: "model.internal"}
	allowlisted := func(value string) bool { return value == "10.0.0.5" }
	if err := endpoint.ValidateDeploymentAllowlist(allowlisted, []net.IP{net.ParseIP("10.0.0.5")}, true); err != nil {
		t.Fatal(err)
	}
	if err := endpoint.ValidateDeploymentAllowlist(allowlisted, []net.IP{net.ParseIP("10.0.0.6")}, true); err == nil {
		t.Fatal("expected endpoint outside deployment allowlist to be rejected")
	}
	if err := endpoint.ValidateDeploymentAllowlist(nil, nil, true); err == nil {
		t.Fatal("expected missing deployment allowlist to fail closed")
	}
}

func TestApprovedEndpointRejectsUnknownOrDuplicateUses(t *testing.T) {
	base := ApprovedEndpoint{
		TenantID:    1,
		Scheme:      "https",
		Host:        "model.internal",
		Port:        443,
		Category:    EndpointCategoryModel,
		AllowedUses: StringArray{"chat"},
	}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}

	unknown := base
	unknown.AllowedUses = StringArray{"query"}
	if err := unknown.Validate(); err == nil {
		t.Fatal("expected a search-only use to be rejected for a model endpoint")
	}

	duplicate := base
	duplicate.AllowedUses = StringArray{"chat", "CHAT"}
	if err := duplicate.Validate(); err == nil {
		t.Fatal("expected duplicate endpoint uses to be rejected")
	}
}

func TestApprovedEndpointRejectsNonHTTPSScheme(t *testing.T) {
	endpoint := ApprovedEndpoint{
		TenantID:    1,
		Scheme:      "ftp",
		Host:        "model.internal",
		Port:        21,
		Category:    EndpointCategoryModel,
		AllowedUses: StringArray{"chat"},
	}
	if err := endpoint.Validate(); err == nil {
		t.Fatal("expected non-HTTP endpoint scheme to be rejected")
	}
}
