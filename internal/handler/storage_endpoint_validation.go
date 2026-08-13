package handler

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

func validateStorageEndpointApprovals(ctx context.Context, tenantID uint64, cfg *types.StorageEngineConfig, repo interfaces.ApprovedEndpointRepository) error {
	if cfg == nil {
		return nil
	}
	validate := func(provider, raw, endpointID string, useSSL bool) (*types.ApprovedEndpoint, error) {
		raw = strings.TrimSpace(raw)
		endpointID = strings.TrimSpace(endpointID)
		if raw == "" && endpointID == "" {
			return nil, nil
		}
		if endpointID == "" {
			if strictAirGappedHandlerMode() {
				return nil, fmt.Errorf("strict air-gapped mode requires approved_endpoint_id for %s storage", provider)
			}
			return nil, nil
		}
		if repo == nil {
			return nil, fmt.Errorf("approved endpoint registry is unavailable")
		}
		endpoint, err := repo.GetByID(ctx, tenantID, endpointID)
		if err != nil {
			return nil, fmt.Errorf("load approved %s storage endpoint: %w", provider, err)
		}
		if endpoint == nil {
			return nil, fmt.Errorf("approved %s storage endpoint not found: %s", provider, endpointID)
		}
		if err := endpoint.Validate(); err != nil {
			return nil, err
		}
		if err := endpoint.ValidateUse(types.EndpointCategoryObjectStorage, "object-storage"); err != nil {
			return nil, err
		}
		if raw == "" {
			raw = fmt.Sprintf("%s://%s:%d", strings.ToLower(endpoint.Scheme), strings.ToLower(endpoint.Host), endpoint.Port)
		} else if !strings.Contains(raw, "://") {
			scheme := "http"
			if useSSL {
				scheme = "https"
			}
			raw = scheme + "://" + raw
		}
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", endpoint.Host)
		if err != nil {
			return nil, fmt.Errorf("resolve approved %s storage endpoint: %w", provider, err)
		}
		if err := endpoint.ValidateConnection(raw, types.EndpointCategoryObjectStorage, "object-storage", ips, strictAirGappedHandlerMode()); err != nil {
			return nil, err
		}
		if strictAirGappedHandlerMode() {
			if err := endpoint.ValidateDeploymentAllowlist(secutils.IsSSRFWhitelisted, ips, true); err != nil {
				return nil, err
			}
		}
		return endpoint, nil
	}

	if cfg.MinIO != nil {
		endpoint, err := validate("minio", cfg.MinIO.Endpoint, cfg.MinIO.ApprovedEndpointID, cfg.MinIO.UseSSL)
		if err != nil {
			return err
		}
		cfg.MinIO.ApprovedEndpoint = endpoint
		if endpoint != nil {
			cfg.MinIO.Endpoint = fmt.Sprintf("%s://%s:%d", strings.ToLower(endpoint.Scheme), strings.ToLower(endpoint.Host), endpoint.Port)
		}
	}
	if cfg.TOS != nil {
		endpoint, err := validate("tos", cfg.TOS.Endpoint, cfg.TOS.ApprovedEndpointID, false)
		if err != nil {
			return err
		}
		cfg.TOS.ApprovedEndpoint = endpoint
		if endpoint != nil {
			cfg.TOS.Endpoint = fmt.Sprintf("%s://%s:%d", strings.ToLower(endpoint.Scheme), strings.ToLower(endpoint.Host), endpoint.Port)
		}
	}
	if cfg.S3 != nil {
		endpoint, err := validate("s3", cfg.S3.Endpoint, cfg.S3.ApprovedEndpointID, false)
		if err != nil {
			return err
		}
		cfg.S3.ApprovedEndpoint = endpoint
		if endpoint != nil {
			cfg.S3.Endpoint = fmt.Sprintf("%s://%s:%d", strings.ToLower(endpoint.Scheme), strings.ToLower(endpoint.Host), endpoint.Port)
		}
	}
	if cfg.OSS != nil {
		endpoint, err := validate("oss", cfg.OSS.Endpoint, cfg.OSS.ApprovedEndpointID, false)
		if err != nil {
			return err
		}
		cfg.OSS.ApprovedEndpoint = endpoint
		if endpoint != nil {
			cfg.OSS.Endpoint = fmt.Sprintf("%s://%s:%d", strings.ToLower(endpoint.Scheme), strings.ToLower(endpoint.Host), endpoint.Port)
		}
	}
	return nil
}

func strictAirGappedHandlerMode() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("AIR_GAPPED_MODE")), "true")
}
