package file

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	modeltransport "github.com/Tencent/WeKnora/internal/models/transport"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

// NewFileServiceFromStorageConfig builds a provider-specific FileService from tenant storage config.
// provider can be empty; in that case it falls back to sec.DefaultProvider.
// Returns the resolved provider name together with the service.
func NewFileServiceFromStorageConfig(
	provider string,
	sec *types.StorageEngineConfig,
	localBaseDir string,
) (interfaces.FileService, string, error) {
	p := strings.ToLower(strings.TrimSpace(provider))
	if p == "" && sec != nil {
		p = strings.ToLower(strings.TrimSpace(sec.DefaultProvider))
	}
	if p == "" {
		return nil, "", fmt.Errorf("empty provider")
	}

	if localBaseDir == "" {
		localBaseDir = strings.TrimSpace(os.Getenv("LOCAL_STORAGE_BASE_DIR"))
	}
	if localBaseDir == "" {
		localBaseDir = "/data/files"
	}

	switch p {
	case "local":
		baseDir := localBaseDir
		if sec != nil && sec.Local != nil {
			rawPrefix := strings.TrimSpace(sec.Local.PathPrefix)
			prefix := strings.Trim(rawPrefix, "/\\")
			if prefix != "" {
				candidate := filepath.Join(baseDir, prefix)
				if safeBaseDir, err := secutils.SafePathUnderBase(baseDir, candidate); err == nil {
					baseDir = safeBaseDir
				}
			}
		}
		return NewLocalFileService(baseDir), p, nil

	case "minio":
		if sec == nil || sec.MinIO == nil {
			return nil, p, fmt.Errorf("missing minio config")
		}
		var endpoint, accessKeyID, secretAccessKey string
		if sec.MinIO.Mode == "remote" {
			endpoint = strings.TrimSpace(sec.MinIO.Endpoint)
			accessKeyID = strings.TrimSpace(sec.MinIO.AccessKeyID)
			secretAccessKey = strings.TrimSpace(sec.MinIO.SecretAccessKey)
		} else {
			endpoint = strings.TrimSpace(os.Getenv("MINIO_ENDPOINT"))
			if endpoint == "" {
				endpoint = strings.TrimSpace(sec.MinIO.Endpoint)
			}
			accessKeyID = strings.TrimSpace(os.Getenv("MINIO_ACCESS_KEY_ID"))
			secretAccessKey = strings.TrimSpace(os.Getenv("MINIO_SECRET_ACCESS_KEY"))
		}
		bucketName := strings.TrimSpace(sec.MinIO.BucketName)
		if bucketName == "" {
			bucketName = strings.TrimSpace(os.Getenv("MINIO_BUCKET_NAME"))
		}
		if endpoint == "" || accessKeyID == "" || secretAccessKey == "" || bucketName == "" {
			return nil, p, fmt.Errorf("incomplete minio config")
		}
		if strictAirGappedMode() && strings.TrimSpace(sec.MinIO.ApprovedEndpointID) == "" {
			return nil, p, fmt.Errorf("strict air-gapped minio storage requires approved_endpoint_id")
		}
		validatedEndpoint := endpoint
		if sec.MinIO.ApprovedEndpoint != nil && !strings.Contains(validatedEndpoint, "://") {
			validatedEndpoint = fmt.Sprintf("%s://%s", sec.MinIO.ApprovedEndpoint.Scheme, validatedEndpoint)
		}
		if err := validateApprovedStorageEndpoint(sec.MinIO.ApprovedEndpoint, validatedEndpoint, p); err != nil {
			return nil, p, err
		}
		if err := validateAirGappedStorageEndpoint(validatedEndpoint, p); err != nil {
			return nil, p, err
		}
		httpClient, err := newApprovedStorageHTTPClient(sec.MinIO.ApprovedEndpoint)
		if err != nil {
			return nil, p, err
		}
		minioEndpoint, minioUseSSL := normalizeMinioClientEndpoint(endpoint, sec.MinIO.ApprovedEndpoint, sec.MinIO.UseSSL)
		svc, err := NewMinioFileService(minioEndpoint, accessKeyID, secretAccessKey, bucketName, minioUseSSL, httpClient)
		return svc, p, err

	case "cos":
		if strictAirGappedMode() {
			return nil, p, fmt.Errorf("strict air-gapped mode blocks public COS storage")
		}
		if sec == nil || sec.COS == nil || sec.COS.SecretID == "" || sec.COS.SecretKey == "" || sec.COS.BucketName == "" || sec.COS.Region == "" {
			return nil, p, fmt.Errorf("incomplete cos config")
		}
		pathPrefix := strings.TrimSpace(sec.COS.PathPrefix)
		if pathPrefix == "" {
			pathPrefix = "weknora"
		}
		svc, err := NewCosFileService(sec.COS.BucketName, sec.COS.Region, sec.COS.SecretID, sec.COS.SecretKey, pathPrefix)
		return svc, p, err

	case "tos":
		if sec == nil || sec.TOS == nil || sec.TOS.Endpoint == "" || sec.TOS.Region == "" || sec.TOS.AccessKey == "" || sec.TOS.SecretKey == "" || sec.TOS.BucketName == "" {
			return nil, p, fmt.Errorf("incomplete tos config")
		}
		if strictAirGappedMode() && strings.TrimSpace(sec.TOS.ApprovedEndpointID) == "" {
			return nil, p, fmt.Errorf("strict air-gapped tos storage requires approved_endpoint_id")
		}
		if err := validateApprovedStorageEndpoint(sec.TOS.ApprovedEndpoint, sec.TOS.Endpoint, p); err != nil {
			return nil, p, err
		}
		if err := validateAirGappedStorageEndpoint(sec.TOS.Endpoint, p); err != nil {
			return nil, p, err
		}
		httpClient, err := newApprovedStorageHTTPClient(sec.TOS.ApprovedEndpoint)
		if err != nil {
			return nil, p, err
		}
		svc, err := NewTosFileService(sec.TOS.Endpoint, sec.TOS.Region, sec.TOS.AccessKey, sec.TOS.SecretKey, sec.TOS.BucketName, sec.TOS.PathPrefix, httpClient)
		return svc, p, err
	case "s3":
		if sec == nil || sec.S3 == nil || sec.S3.Endpoint == "" || sec.S3.Region == "" || sec.S3.AccessKey == "" || sec.S3.SecretKey == "" || sec.S3.BucketName == "" {
			return nil, p, fmt.Errorf("incomplete s3 config")
		}
		if strictAirGappedMode() && strings.TrimSpace(sec.S3.ApprovedEndpointID) == "" {
			return nil, p, fmt.Errorf("strict air-gapped s3 storage requires approved_endpoint_id")
		}
		if err := validateApprovedStorageEndpoint(sec.S3.ApprovedEndpoint, sec.S3.Endpoint, p); err != nil {
			return nil, p, err
		}
		if err := validateAirGappedStorageEndpoint(sec.S3.Endpoint, p); err != nil {
			return nil, p, err
		}
		pathPrefix := strings.TrimSpace(sec.S3.PathPrefix)
		if pathPrefix == "" {
			pathPrefix = "weknora/"
		}
		httpClient, err := newApprovedStorageHTTPClient(sec.S3.ApprovedEndpoint)
		if err != nil {
			return nil, p, err
		}
		svc, err := NewS3FileService(sec.S3.Endpoint, sec.S3.AccessKey, sec.S3.SecretKey, sec.S3.BucketName, sec.S3.Region, pathPrefix, httpClient)
		return svc, p, err

	case "oss":
		if sec == nil || sec.OSS == nil || sec.OSS.Endpoint == "" || sec.OSS.Region == "" || sec.OSS.AccessKey == "" || sec.OSS.SecretKey == "" || sec.OSS.BucketName == "" {
			return nil, p, fmt.Errorf("incomplete oss config")
		}
		if strictAirGappedMode() && strings.TrimSpace(sec.OSS.ApprovedEndpointID) == "" {
			return nil, p, fmt.Errorf("strict air-gapped oss storage requires approved_endpoint_id")
		}
		if err := validateApprovedStorageEndpoint(sec.OSS.ApprovedEndpoint, sec.OSS.Endpoint, p); err != nil {
			return nil, p, err
		}
		if err := validateAirGappedStorageEndpoint(sec.OSS.Endpoint, p); err != nil {
			return nil, p, err
		}
		pathPrefix := strings.TrimSpace(sec.OSS.PathPrefix)
		if pathPrefix == "" {
			pathPrefix = "weknora/"
		}
		var svc interfaces.FileService
		var err error
		httpClient, err := newApprovedStorageHTTPClient(sec.OSS.ApprovedEndpoint)
		if err != nil {
			return nil, p, err
		}
		if sec.OSS.UseTempBucket && sec.OSS.TempBucketName != "" {
			svc, err = NewOssFileServiceWithTempBucket(
				sec.OSS.Endpoint, sec.OSS.Region, sec.OSS.AccessKey, sec.OSS.SecretKey,
				sec.OSS.BucketName, pathPrefix,
				sec.OSS.TempBucketName, sec.OSS.TempRegion,
				httpClient,
			)
		} else {
			svc, err = NewOssFileService(
				sec.OSS.Endpoint, sec.OSS.Region, sec.OSS.AccessKey, sec.OSS.SecretKey,
				sec.OSS.BucketName, pathPrefix,
				httpClient,
			)
		}
		return svc, p, err

	default:
		return nil, p, fmt.Errorf("unsupported provider %q", p)
	}
}

func strictAirGappedMode() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("AIR_GAPPED_MODE")), "true")
}

func newApprovedStorageHTTPClient(endpoint *types.ApprovedEndpoint) (*http.Client, error) {
	if endpoint == nil {
		return nil, nil
	}
	if err := endpoint.Validate(); err != nil {
		return nil, fmt.Errorf("approved object storage endpoint: %w", err)
	}
	if err := endpoint.ValidateUse(types.EndpointCategoryObjectStorage, "object-storage"); err != nil {
		return nil, fmt.Errorf("approved object storage endpoint: %w", err)
	}
	baseURL := fmt.Sprintf("%s://%s:%d", strings.ToLower(endpoint.Scheme), strings.ToLower(endpoint.Host), endpoint.Port)
	return modeltransport.NewEndpointHTTPClientWithValidation(baseURL, 5*time.Minute, func(ip net.IP) error {
		if err := endpoint.ValidateConnection(baseURL, types.EndpointCategoryObjectStorage, "object-storage", []net.IP{ip}, strictAirGappedMode()); err != nil {
			return err
		}
		if strictAirGappedMode() {
			if err := endpoint.ValidateDeploymentAllowlist(secutils.IsSSRFWhitelisted, []net.IP{ip}, true); err != nil {
				return err
			}
		}
		return nil
	})
}

func normalizeMinioClientEndpoint(raw string, approved *types.ApprovedEndpoint, useSSL bool) (string, bool) {
	if approved != nil {
		return net.JoinHostPort(approved.Host, fmt.Sprintf("%d", approved.Port)), strings.EqualFold(approved.Scheme, "https")
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err == nil && parsed.Hostname() != "" {
		if parsed.Port() != "" {
			return net.JoinHostPort(parsed.Hostname(), parsed.Port()), strings.EqualFold(parsed.Scheme, "https")
		}
		return parsed.Hostname(), strings.EqualFold(parsed.Scheme, "https")
	}
	return strings.TrimSpace(raw), useSSL
}

func validateAirGappedStorageEndpoint(raw, provider string) error {
	if !strictAirGappedMode() {
		return nil
	}
	normalized := strings.TrimSpace(raw)
	if !strings.Contains(normalized, "://") {
		normalized = "http://" + normalized
	}
	parsed, err := url.Parse(normalized)
	if err != nil || parsed.Hostname() == "" {
		return fmt.Errorf("air-gapped %s storage endpoint is invalid", provider)
	}
	ips, lookupErr := net.LookupIP(parsed.Hostname())
	location := types.DeriveEndpointLocation(normalized, ips)
	if lookupErr != nil || location == types.EndpointPublic || location == types.EndpointUnknown {
		return fmt.Errorf("air-gapped %s storage endpoint must resolve to a private or same-host address", provider)
	}
	if location == types.EndpointPrivateNetwork && !secutils.IsSSRFWhitelisted(parsed.Hostname()) {
		return fmt.Errorf("air-gapped %s storage endpoint is not in the deployment SSRF allowlist", provider)
	}
	return nil
}

func validateApprovedStorageEndpoint(endpoint *types.ApprovedEndpoint, raw, provider string) error {
	if endpoint == nil {
		return nil
	}
	if err := endpoint.ValidateUse(types.EndpointCategoryObjectStorage, "object-storage"); err != nil {
		return fmt.Errorf("approved %s storage endpoint: %w", provider, err)
	}
	host := endpoint.Host
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("approved %s storage endpoint DNS lookup: %w", provider, err)
	}
	if err := endpoint.ValidateConnection(raw, types.EndpointCategoryObjectStorage, "object-storage", ips, strictAirGappedMode()); err != nil {
		return fmt.Errorf("approved %s storage endpoint: %w", provider, err)
	}
	if strictAirGappedMode() && !secutils.IsSSRFWhitelisted(host) {
		return fmt.Errorf("approved %s storage endpoint is not in the deployment SSRF allowlist", provider)
	}
	return nil
}
