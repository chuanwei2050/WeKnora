param(
	[UInt64]$Tenant = 0,
	[int]$Limit = 0,
	[ValidateRange(1, 16)]
	[int]$Concurrency = 6
)

$ErrorActionPreference = 'Stop'
if ($Tenant -gt 0) {
    go run ./cmd/backfill-table-schemas --tenant $Tenant --limit $Limit --concurrency $Concurrency
} else {
    go run ./cmd/backfill-table-schemas --all-tenants --limit $Limit --concurrency $Concurrency
}
