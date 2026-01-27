// Azure Mock API Server
//
// A lightweight mock server that implements Azure REST API endpoints
// for integration testing. Supports:
//   - Azure CDN (profiles and endpoints)
//   - Azure DNS (zones and CNAME records)
//   - Azure Storage Accounts (read-only for data source)
//
// Usage:
//
//	docker run -p 8080:8080 azure-mock
//
// Configure Terraform azurerm provider to use this endpoint.
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// =============================================================================
// In-Memory Store
// =============================================================================

type Store struct {
	mu               sync.RWMutex
	cdnProfiles      map[string]CDNProfile
	cdnEndpoints     map[string]CDNEndpoint
	cdnCustomDomains map[string]CDNCustomDomain
	dnsZones         map[string]DNSZone
	dnsCNAMERecords  map[string]DNSCNAMERecord
	storageAccounts  map[string]StorageAccount
	blobContainers   map[string]BlobContainer // key: accountName/containerName
	blobs            map[string]Blob          // key: accountName/containerName/blobName
	blobBlocks       map[string][]byte        // key: blobKey/blockId - staged blocks for block blob uploads
	// App Service resources
	appServicePlans       map[string]AppServicePlan
	linuxWebApps          map[string]LinuxWebApp
	webAppSlots           map[string]WebAppSlot
	logAnalyticsWorkspaces map[string]LogAnalyticsWorkspace
	appInsights           map[string]ApplicationInsights
	autoscaleSettings     map[string]AutoscaleSetting
	actionGroups          map[string]ActionGroup
	metricAlerts          map[string]MetricAlert
	diagnosticSettings    map[string]DiagnosticSetting
	trafficRouting        map[string][]TrafficRoutingRule
}

// TrafficRoutingRule represents a traffic routing rule for a slot
type TrafficRoutingRule struct {
	ActionHostName    string `json:"actionHostName"`
	ReroutePercentage int    `json:"reroutePercentage"`
	Name              string `json:"name"`
}

func NewStore() *Store {
	return &Store{
		cdnProfiles:            make(map[string]CDNProfile),
		cdnEndpoints:           make(map[string]CDNEndpoint),
		cdnCustomDomains:       make(map[string]CDNCustomDomain),
		dnsZones:               make(map[string]DNSZone),
		dnsCNAMERecords:        make(map[string]DNSCNAMERecord),
		storageAccounts:        make(map[string]StorageAccount),
		blobContainers:         make(map[string]BlobContainer),
		blobs:                  make(map[string]Blob),
		blobBlocks:             make(map[string][]byte),
		appServicePlans:        make(map[string]AppServicePlan),
		linuxWebApps:           make(map[string]LinuxWebApp),
		webAppSlots:            make(map[string]WebAppSlot),
		logAnalyticsWorkspaces: make(map[string]LogAnalyticsWorkspace),
		appInsights:            make(map[string]ApplicationInsights),
		autoscaleSettings:      make(map[string]AutoscaleSetting),
		actionGroups:           make(map[string]ActionGroup),
		metricAlerts:           make(map[string]MetricAlert),
		diagnosticSettings:     make(map[string]DiagnosticSetting),
		trafficRouting:         make(map[string][]TrafficRoutingRule),
	}
}

// =============================================================================
// Azure Resource Models
// =============================================================================

// CDN Profile
type CDNProfile struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Sku        CDNSku            `json:"sku"`
	Properties CDNProfileProps   `json:"properties"`
}

type CDNSku struct {
	Name string `json:"name"`
}

type CDNProfileProps struct {
	ResourceState     string `json:"resourceState"`
	ProvisioningState string `json:"provisioningState"`
}

// CDN Endpoint
type CDNEndpoint struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Type       string              `json:"type"`
	Location   string              `json:"location"`
	Tags       map[string]string   `json:"tags,omitempty"`
	Properties CDNEndpointProps    `json:"properties"`
}

// CDN Custom Domain
type CDNCustomDomain struct {
	ID         string                  `json:"id"`
	Name       string                  `json:"name"`
	Type       string                  `json:"type"`
	Properties CDNCustomDomainProps    `json:"properties"`
}

type CDNCustomDomainProps struct {
	HostName          string `json:"hostName"`
	ResourceState     string `json:"resourceState"`
	ProvisioningState string `json:"provisioningState"`
	ValidationData    string `json:"validationData,omitempty"`
}

type CDNEndpointProps struct {
	HostName              string              `json:"hostName"`
	OriginHostHeader      string              `json:"originHostHeader,omitempty"`
	Origins               []CDNOrigin         `json:"origins"`
	OriginPath            string              `json:"originPath,omitempty"`
	IsHttpAllowed         bool                `json:"isHttpAllowed"`
	IsHttpsAllowed        bool                `json:"isHttpsAllowed"`
	IsCompressionEnabled  bool                `json:"isCompressionEnabled"`
	ResourceState         string              `json:"resourceState"`
	ProvisioningState     string              `json:"provisioningState"`
	DeliveryPolicy        *CDNDeliveryPolicy  `json:"deliveryPolicy,omitempty"`
}

type CDNOrigin struct {
	Name       string          `json:"name"`
	Properties CDNOriginProps  `json:"properties"`
}

type CDNOriginProps struct {
	HostName  string `json:"hostName"`
	HttpPort  int    `json:"httpPort,omitempty"`
	HttpsPort int    `json:"httpsPort,omitempty"`
}

type CDNDeliveryPolicy struct {
	Rules []CDNDeliveryRule `json:"rules,omitempty"`
}

type CDNDeliveryRule struct {
	Name    string        `json:"name"`
	Order   int           `json:"order"`
	Actions []interface{} `json:"actions,omitempty"`
}

// DNS Zone
type DNSZone struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Type       string          `json:"type"`
	Location   string          `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Properties DNSZoneProps    `json:"properties"`
}

type DNSZoneProps struct {
	MaxNumberOfRecordSets int      `json:"maxNumberOfRecordSets"`
	NumberOfRecordSets    int      `json:"numberOfRecordSets"`
	NameServers           []string `json:"nameServers"`
}

// DNS CNAME Record
type DNSCNAMERecord struct {
	ID         string               `json:"id"`
	Name       string               `json:"name"`
	Type       string               `json:"type"`
	Etag       string               `json:"etag,omitempty"`
	Properties DNSCNAMERecordProps  `json:"properties"`
}

type DNSCNAMERecordProps struct {
	TTL         int              `json:"TTL"`
	Fqdn        string           `json:"fqdn,omitempty"`
	CNAMERecord *DNSCNAMEValue   `json:"CNAMERecord,omitempty"`
}

type DNSCNAMEValue struct {
	Cname string `json:"cname"`
}

// Storage Account
type StorageAccount struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Type       string                 `json:"type"`
	Location   string                 `json:"location"`
	Tags       map[string]string      `json:"tags,omitempty"`
	Kind       string                 `json:"kind"`
	Sku        StorageSku             `json:"sku"`
	Properties StorageAccountProps    `json:"properties"`
}

type StorageSku struct {
	Name string `json:"name"`
	Tier string `json:"tier"`
}

type StorageAccountProps struct {
	PrimaryEndpoints  StorageEndpoints `json:"primaryEndpoints"`
	ProvisioningState string           `json:"provisioningState"`
}

type StorageEndpoints struct {
	Blob string `json:"blob"`
	Web  string `json:"web"`
}

// Blob Storage Container
type BlobContainer struct {
	Name       string    `json:"name"`
	Properties BlobContainerProps `json:"properties"`
}

type BlobContainerProps struct {
	LastModified string `json:"lastModified"`
	Etag         string `json:"etag"`
}

// Blob
type Blob struct {
	Name       string            `json:"name"`
	Content    []byte            `json:"-"`
	Properties BlobProps         `json:"properties"`
	Metadata   map[string]string `json:"-"` // x-ms-meta-* headers
}

type BlobProps struct {
	LastModified  string `json:"lastModified"`
	Etag          string `json:"etag"`
	ContentLength int    `json:"contentLength"`
	ContentType   string `json:"contentType"`
}

// =============================================================================
// App Service Models
// =============================================================================

// App Service Plan (serverfarms)
type AppServicePlan struct {
	ID         string               `json:"id"`
	Name       string               `json:"name"`
	Type       string               `json:"type"`
	Location   string               `json:"location"`
	Tags       map[string]string    `json:"tags,omitempty"`
	Kind       string               `json:"kind,omitempty"`
	Sku        AppServiceSku        `json:"sku"`
	Properties AppServicePlanProps  `json:"properties"`
}

type AppServiceSku struct {
	Name     string `json:"name"`
	Tier     string `json:"tier"`
	Size     string `json:"size"`
	Family   string `json:"family"`
	Capacity int    `json:"capacity"`
}

type AppServicePlanProps struct {
	ProvisioningState       string `json:"provisioningState"`
	Status                  string `json:"status"`
	MaximumNumberOfWorkers  int    `json:"maximumNumberOfWorkers"`
	NumberOfSites           int    `json:"numberOfSites"`
	PerSiteScaling          bool   `json:"perSiteScaling"`
	ZoneRedundant           bool   `json:"zoneRedundant"`
	Reserved                bool   `json:"reserved"` // true for Linux
}

// Linux Web App (sites)
type LinuxWebApp struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Type       string              `json:"type"`
	Location   string              `json:"location"`
	Tags       map[string]string   `json:"tags,omitempty"`
	Kind       string              `json:"kind,omitempty"`
	Identity   *AppIdentity        `json:"identity,omitempty"`
	Properties LinuxWebAppProps    `json:"properties"`
}

type AppIdentity struct {
	Type        string            `json:"type"`
	PrincipalID string            `json:"principalId,omitempty"`
	TenantID    string            `json:"tenantId,omitempty"`
	UserIDs     map[string]string `json:"userAssignedIdentities,omitempty"`
}

type LinuxWebAppProps struct {
	ProvisioningState           string           `json:"provisioningState"`
	State                       string           `json:"state"`
	DefaultHostName             string           `json:"defaultHostName"`
	ServerFarmID                string           `json:"serverFarmId"`
	HTTPSOnly                   bool             `json:"httpsOnly"`
	ClientAffinityEnabled       bool             `json:"clientAffinityEnabled"`
	OutboundIPAddresses         string           `json:"outboundIpAddresses"`
	PossibleOutboundIPAddresses string           `json:"possibleOutboundIpAddresses"`
	CustomDomainVerificationID  string           `json:"customDomainVerificationId"`
	SiteConfig                  *WebAppSiteConfig `json:"siteConfig,omitempty"`
}

type WebAppSiteConfig struct {
	AlwaysOn              bool                     `json:"alwaysOn"`
	HTTP20Enabled         bool                     `json:"http20Enabled"`
	WebSocketsEnabled     bool                     `json:"webSocketsEnabled"`
	FtpsState             string                   `json:"ftpsState"`
	MinTLSVersion         string                   `json:"minTlsVersion"`
	LinuxFxVersion        string                   `json:"linuxFxVersion"`
	AppCommandLine        string                   `json:"appCommandLine,omitempty"`
	HealthCheckPath       string                   `json:"healthCheckPath,omitempty"`
	VnetRouteAllEnabled   bool                     `json:"vnetRouteAllEnabled"`
	AutoHealEnabled       bool                     `json:"autoHealEnabled"`
	Experiments           *WebAppExperiments       `json:"experiments,omitempty"`
}

// WebAppExperiments contains traffic routing configuration
type WebAppExperiments struct {
	RampUpRules []RampUpRule `json:"rampUpRules,omitempty"`
}

// RampUpRule defines traffic routing to a deployment slot
type RampUpRule struct {
	ActionHostName    string  `json:"actionHostName"`
	ReroutePercentage float64 `json:"reroutePercentage"`
	Name              string  `json:"name"`
}

// Web App Slot
type WebAppSlot struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Type       string              `json:"type"`
	Location   string              `json:"location"`
	Tags       map[string]string   `json:"tags,omitempty"`
	Kind       string              `json:"kind,omitempty"`
	Properties LinuxWebAppProps    `json:"properties"`
}

// Log Analytics Workspace
type LogAnalyticsWorkspace struct {
	ID         string                      `json:"id"`
	Name       string                      `json:"name"`
	Type       string                      `json:"type"`
	Location   string                      `json:"location"`
	Tags       map[string]string           `json:"tags,omitempty"`
	Properties LogAnalyticsWorkspaceProps  `json:"properties"`
}

type LogAnalyticsWorkspaceProps struct {
	ProvisioningState string `json:"provisioningState"`
	CustomerID        string `json:"customerId"`
	Sku               struct {
		Name string `json:"name"`
	} `json:"sku"`
	RetentionInDays int `json:"retentionInDays"`
}

// Application Insights
type ApplicationInsights struct {
	ID         string                    `json:"id"`
	Name       string                    `json:"name"`
	Type       string                    `json:"type"`
	Location   string                    `json:"location"`
	Tags       map[string]string         `json:"tags,omitempty"`
	Kind       string                    `json:"kind"`
	Properties ApplicationInsightsProps  `json:"properties"`
}

type ApplicationInsightsProps struct {
	ProvisioningState   string `json:"provisioningState"`
	ApplicationID       string `json:"AppId"`
	InstrumentationKey  string `json:"InstrumentationKey"`
	ConnectionString    string `json:"ConnectionString"`
	WorkspaceResourceID string `json:"WorkspaceResourceId,omitempty"`
}

// Monitor Autoscale Settings
type AutoscaleSetting struct {
	ID         string                  `json:"id"`
	Name       string                  `json:"name"`
	Type       string                  `json:"type"`
	Location   string                  `json:"location"`
	Tags       map[string]string       `json:"tags,omitempty"`
	Properties AutoscaleSettingProps   `json:"properties"`
}

type AutoscaleSettingProps struct {
	ProvisioningState      string `json:"provisioningState,omitempty"`
	Enabled                bool   `json:"enabled"`
	TargetResourceURI      string `json:"targetResourceUri"`
	TargetResourceLocation string `json:"targetResourceLocation,omitempty"`
	Profiles               []interface{} `json:"profiles"`
	Notifications          []interface{} `json:"notifications,omitempty"`
}

// Monitor Action Group
type ActionGroup struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Type       string              `json:"type"`
	Location   string              `json:"location"`
	Tags       map[string]string   `json:"tags,omitempty"`
	Properties ActionGroupProps    `json:"properties"`
}

type ActionGroupProps struct {
	GroupShortName    string        `json:"groupShortName"`
	Enabled           bool          `json:"enabled"`
	EmailReceivers    []interface{} `json:"emailReceivers,omitempty"`
	WebhookReceivers  []interface{} `json:"webhookReceivers,omitempty"`
}

// Monitor Metric Alert
type MetricAlert struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Type       string              `json:"type"`
	Location   string              `json:"location"`
	Tags       map[string]string   `json:"tags,omitempty"`
	Properties MetricAlertProps    `json:"properties"`
}

type MetricAlertProps struct {
	Description          string        `json:"description,omitempty"`
	Severity             int           `json:"severity"`
	Enabled              bool          `json:"enabled"`
	Scopes               []string      `json:"scopes"`
	EvaluationFrequency  string        `json:"evaluationFrequency"`
	WindowSize           string        `json:"windowSize"`
	Criteria             interface{}   `json:"criteria"`
	Actions              []interface{} `json:"actions,omitempty"`
}

// Diagnostic Settings (nested resource)
type DiagnosticSetting struct {
	ID         string                   `json:"id"`
	Name       string                   `json:"name"`
	Type       string                   `json:"type"`
	Properties DiagnosticSettingProps   `json:"properties"`
}

type DiagnosticSettingProps struct {
	WorkspaceID string        `json:"workspaceId,omitempty"`
	Logs        []interface{} `json:"logs,omitempty"`
	Metrics     []interface{} `json:"metrics,omitempty"`
}

// Azure Error Response
type AzureError struct {
	Error AzureErrorDetail `json:"error"`
}

type AzureErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// =============================================================================
// Server
// =============================================================================

type Server struct {
	store *Store
}

func NewServer() *Server {
	return &Server{
		store: NewStore(),
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	method := r.Method
	host := r.Host

	log.Printf("%s %s (Host: %s)", method, path, host)

	// Health check
	if path == "/health" || path == "/" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	}

	// Check if this is a Blob Storage request (based on Host header)
	if strings.Contains(host, ".blob.core.windows.net") {
		s.handleBlobStorage(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// OpenID Connect discovery endpoints (required by MSAL/Azure CLI)
	if strings.Contains(path, "/.well-known/openid-configuration") {
		s.handleOpenIDConfiguration(w, r)
		return
	}

	// MSAL instance discovery endpoint
	if strings.Contains(path, "/common/discovery/instance") || strings.Contains(path, "/discovery/instance") {
		s.handleInstanceDiscovery(w, r)
		return
	}

	// OAuth token endpoint (Azure AD authentication)
	if strings.Contains(path, "/oauth2/token") || strings.Contains(path, "/oauth2/v2.0/token") {
		s.handleOAuth(w, r)
		return
	}

	// Subscription endpoint
	if matchSubscription(path) {
		s.handleSubscription(w, r)
		return
	}

	// List all providers endpoint (for provider cache)
	if matchListProviders(path) {
		s.handleListProviders(w, r)
		return
	}

	// Provider registration endpoint
	if matchProviderRegistration(path) {
		s.handleProviderRegistration(w, r)
		return
	}

	// Route to appropriate handler
	// Note: More specific routes must come first (operationresults before enableCustomHttps before customDomain, customDomain before endpoint)
	switch {
	case matchCDNOperationResults(path):
		s.handleCDNOperationResults(w, r)
	case matchCDNCustomDomainEnableHttps(path):
		s.handleCDNCustomDomainHttps(w, r, true)
	case matchCDNCustomDomainDisableHttps(path):
		s.handleCDNCustomDomainHttps(w, r, false)
	case matchCDNCustomDomain(path):
		s.handleCDNCustomDomain(w, r)
	case matchCDNProfile(path):
		s.handleCDNProfile(w, r)
	case matchCDNEndpoint(path):
		s.handleCDNEndpoint(w, r)
	case matchDNSZone(path):
		s.handleDNSZone(w, r)
	case matchDNSCNAMERecord(path):
		s.handleDNSCNAMERecord(w, r)
	case matchStorageAccountKeys(path):
		s.handleStorageAccountKeys(w, r)
	case matchStorageAccount(path):
		s.handleStorageAccount(w, r)
	// App Service handlers (more specific routes first)
	case matchWebAppCheckName(path):
		s.handleWebAppCheckName(w, r)
	case matchWebAppAuthSettings(path):
		s.handleWebAppAuthSettings(w, r)
	case matchWebAppAuthSettingsV2(path):
		s.handleWebAppAuthSettingsV2(w, r)
	case matchWebAppConfigLogs(path):
		s.handleWebAppConfigLogs(w, r)
	case matchWebAppAppSettings(path):
		s.handleWebAppAppSettings(w, r)
	case matchWebAppConnStrings(path):
		s.handleWebAppConnStrings(w, r)
	case matchWebAppStickySettings(path):
		s.handleWebAppStickySettings(w, r)
	case matchWebAppStorageAccounts(path):
		s.handleWebAppStorageAccounts(w, r)
	case matchWebAppBackups(path):
		s.handleWebAppBackups(w, r)
	case matchWebAppMetadata(path):
		s.handleWebAppMetadata(w, r)
	case matchWebAppPubCreds(path):
		s.handleWebAppPubCreds(w, r)
	case matchWebAppConfig(path):
		// Must be before ConfigFallback - /config/web is more specific than /config/[^/]+
		s.handleWebAppConfig(w, r)
	case matchWebAppConfigFallback(path):
		s.handleWebAppConfigFallback(w, r)
	case matchWebAppBasicAuthPolicy(path):
		s.handleWebAppBasicAuthPolicy(w, r)
	case matchWebAppSlotConfig(path):
		s.handleWebAppSlotConfig(w, r)
	case matchWebAppSlotConfigFallback(path):
		s.handleWebAppSlotConfigFallback(w, r)
	case matchWebAppSlotBasicAuthPolicy(path):
		s.handleWebAppSlotBasicAuthPolicy(w, r)
	case matchWebAppSlot(path):
		s.handleWebAppSlot(w, r)
	case matchWebAppTrafficRouting(path):
		s.handleWebAppTrafficRouting(w, r)
	case matchLinuxWebApp(path):
		s.handleLinuxWebApp(w, r)
	case matchAppServicePlan(path):
		s.handleAppServicePlan(w, r)
	// Monitoring handlers
	case matchLogAnalytics(path):
		s.handleLogAnalytics(w, r)
	case matchAppInsights(path):
		s.handleAppInsights(w, r)
	case matchAutoscaleSetting(path):
		s.handleAutoscaleSetting(w, r)
	case matchActionGroup(path):
		s.handleActionGroup(w, r)
	case matchMetricAlert(path):
		s.handleMetricAlert(w, r)
	case matchDiagnosticSetting(path):
		s.handleDiagnosticSetting(w, r)
	default:
		s.notFound(w, path)
	}
}

// =============================================================================
// Path Matchers
// =============================================================================

var (
	subscriptionRegex         = regexp.MustCompile(`^/subscriptions/[^/]+$`)
	listProvidersRegex        = regexp.MustCompile(`^/subscriptions/[^/]+/providers$`)
	providerRegistrationRegex = regexp.MustCompile(`/subscriptions/[^/]+/providers/Microsoft\.[^/]+$`)
	cdnProfileRegex           = regexp.MustCompile(`/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Cdn/profiles/[^/]+$`)
	cdnEndpointRegex          = regexp.MustCompile(`/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Cdn/profiles/[^/]+/endpoints/[^/]+$`)
	cdnCustomDomainRegex           = regexp.MustCompile(`(?i)/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Cdn/profiles/[^/]+/endpoints/[^/]+/customDomains/[^/]+$`)
	cdnCustomDomainEnableHttpsRegex  = regexp.MustCompile(`(?i)/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Cdn/profiles/[^/]+/endpoints/[^/]+/customDomains/[^/]+/enableCustomHttps$`)
	cdnCustomDomainDisableHttpsRegex = regexp.MustCompile(`(?i)/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Cdn/profiles/[^/]+/endpoints/[^/]+/customDomains/[^/]+/disableCustomHttps$`)
	cdnOperationResultsRegex         = regexp.MustCompile(`(?i)/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Cdn/profiles/[^/]+/endpoints/[^/]+/customDomains/[^/]+/operationresults/`)
	dnsZoneRegex              = regexp.MustCompile(`(?i)/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Network/dnszones/[^/]+$`)
	dnsCNAMERecordRegex       = regexp.MustCompile(`(?i)/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Network/dnszones/[^/]+/CNAME/[^/]+$`)
	storageAccountRegex       = regexp.MustCompile(`/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Storage/storageAccounts/[^/]+$`)
	storageAccountKeysRegex   = regexp.MustCompile(`/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Storage/storageAccounts/[^/]+/listKeys$`)
	// App Service resources
	appServicePlanRegex         = regexp.MustCompile(`(?i)/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Web/serverfarms/[^/]+$`)
	linuxWebAppRegex            = regexp.MustCompile(`(?i)/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Web/sites/[^/]+$`)
	webAppSlotRegex               = regexp.MustCompile(`(?i)/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Web/sites/[^/]+/slots/[^/]+$`)
	webAppSlotConfigRegex         = regexp.MustCompile(`(?i)/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Web/sites/[^/]+/slots/[^/]+/config/web$`)
	webAppSlotConfigFallbackRegex = regexp.MustCompile(`(?i)/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Web/sites/[^/]+/slots/[^/]+/config/[^/]+(/list)?$`)
	webAppSlotBasicAuthPolicyRegex = regexp.MustCompile(`(?i)/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Web/sites/[^/]+/slots/[^/]+/basicPublishingCredentialsPolicies/(ftp|scm)$`)
	webAppConfigRegex           = regexp.MustCompile(`(?i)/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Web/sites/[^/]+/config/web$`)
	webAppCheckNameRegex        = regexp.MustCompile(`(?i)/subscriptions/[^/]+/providers/Microsoft\.Web/checknameavailability$`)
	webAppAuthSettingsRegex     = regexp.MustCompile(`(?i)/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Web/sites/[^/]+/config/authsettings/list$`)
	webAppAuthSettingsV2Regex   = regexp.MustCompile(`(?i)/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Web/sites/[^/]+/config/authsettingsV2/list$`)
	webAppConfigLogsRegex       = regexp.MustCompile(`(?i)/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Web/sites/[^/]+/config/logs$`)
	webAppAppSettingsRegex      = regexp.MustCompile(`(?i)/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Web/sites/[^/]+/config/appSettings/list$`)
	webAppConnStringsRegex      = regexp.MustCompile(`(?i)/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Web/sites/[^/]+/config/connectionstrings/list$`)
	webAppStickySettingsRegex   = regexp.MustCompile(`(?i)/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Web/sites/[^/]+/config/slotConfigNames$`)
	webAppStorageAccountsRegex  = regexp.MustCompile(`(?i)/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Web/sites/[^/]+/config/azurestorageaccounts/list$`)
	webAppBackupsRegex          = regexp.MustCompile(`(?i)/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Web/sites/[^/]+/config/backup/list$`)
	webAppMetadataRegex         = regexp.MustCompile(`(?i)/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Web/sites/[^/]+/config/metadata/list$`)
	webAppPubCredsRegex         = regexp.MustCompile(`(?i)/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Web/sites/[^/]+/config/publishingcredentials/list$`)
	webAppConfigFallbackRegex   = regexp.MustCompile(`(?i)/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Web/sites/[^/]+/config/[^/]+(/list)?$`)
	webAppBasicAuthPolicyRegex  = regexp.MustCompile(`(?i)/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Web/sites/[^/]+/basicPublishingCredentialsPolicies/(ftp|scm)$`)
	webAppTrafficRoutingRegex   = regexp.MustCompile(`(?i)/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Web/sites/[^/]+/trafficRouting$`)
	// Monitoring resources
	logAnalyticsRegex         = regexp.MustCompile(`(?i)/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.OperationalInsights/workspaces/[^/]+$`)
	appInsightsRegex          = regexp.MustCompile(`(?i)/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Insights/components/[^/]+$`)
	autoscaleSettingRegex     = regexp.MustCompile(`(?i)/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Insights/autoscalesettings/[^/]+$`)
	actionGroupRegex          = regexp.MustCompile(`(?i)/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Insights/actionGroups/[^/]+$`)
	metricAlertRegex          = regexp.MustCompile(`(?i)/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Insights/metricAlerts/[^/]+$`)
	diagnosticSettingRegex    = regexp.MustCompile(`(?i)/providers/Microsoft\.Insights/diagnosticSettings/[^/]+$`)
)

func matchSubscription(path string) bool         { return subscriptionRegex.MatchString(path) }
func matchListProviders(path string) bool        { return listProvidersRegex.MatchString(path) }
func matchProviderRegistration(path string) bool { return providerRegistrationRegex.MatchString(path) }
func matchCDNProfile(path string) bool           { return cdnProfileRegex.MatchString(path) }
func matchCDNEndpoint(path string) bool          { return cdnEndpointRegex.MatchString(path) }
func matchCDNCustomDomain(path string) bool            { return cdnCustomDomainRegex.MatchString(path) }
func matchCDNCustomDomainEnableHttps(path string) bool  { return cdnCustomDomainEnableHttpsRegex.MatchString(path) }
func matchCDNCustomDomainDisableHttps(path string) bool { return cdnCustomDomainDisableHttpsRegex.MatchString(path) }
func matchCDNOperationResults(path string) bool         { return cdnOperationResultsRegex.MatchString(path) }
func matchDNSZone(path string) bool              { return dnsZoneRegex.MatchString(path) }
func matchDNSCNAMERecord(path string) bool       { return dnsCNAMERecordRegex.MatchString(path) }
func matchStorageAccount(path string) bool       { return storageAccountRegex.MatchString(path) }
func matchStorageAccountKeys(path string) bool   { return storageAccountKeysRegex.MatchString(path) }
// App Service matchers
func matchAppServicePlan(path string) bool       { return appServicePlanRegex.MatchString(path) }
func matchLinuxWebApp(path string) bool          { return linuxWebAppRegex.MatchString(path) }
func matchWebAppSlot(path string) bool               { return webAppSlotRegex.MatchString(path) }
func matchWebAppSlotConfig(path string) bool         { return webAppSlotConfigRegex.MatchString(path) }
func matchWebAppSlotConfigFallback(path string) bool { return webAppSlotConfigFallbackRegex.MatchString(path) }
func matchWebAppSlotBasicAuthPolicy(path string) bool { return webAppSlotBasicAuthPolicyRegex.MatchString(path) }
func matchWebAppConfig(path string) bool            { return webAppConfigRegex.MatchString(path) }
func matchWebAppCheckName(path string) bool      { return webAppCheckNameRegex.MatchString(path) }
func matchWebAppAuthSettings(path string) bool   { return webAppAuthSettingsRegex.MatchString(path) }
func matchWebAppAuthSettingsV2(path string) bool { return webAppAuthSettingsV2Regex.MatchString(path) }
func matchWebAppConfigLogs(path string) bool     { return webAppConfigLogsRegex.MatchString(path) }
func matchWebAppAppSettings(path string) bool    { return webAppAppSettingsRegex.MatchString(path) }
func matchWebAppConnStrings(path string) bool    { return webAppConnStringsRegex.MatchString(path) }
func matchWebAppStickySettings(path string) bool { return webAppStickySettingsRegex.MatchString(path) }
func matchWebAppStorageAccounts(path string) bool { return webAppStorageAccountsRegex.MatchString(path) }
func matchWebAppBackups(path string) bool        { return webAppBackupsRegex.MatchString(path) }
func matchWebAppMetadata(path string) bool       { return webAppMetadataRegex.MatchString(path) }
func matchWebAppPubCreds(path string) bool       { return webAppPubCredsRegex.MatchString(path) }
func matchWebAppConfigFallback(path string) bool { return webAppConfigFallbackRegex.MatchString(path) }
func matchWebAppBasicAuthPolicy(path string) bool { return webAppBasicAuthPolicyRegex.MatchString(path) }
func matchWebAppTrafficRouting(path string) bool  { return webAppTrafficRoutingRegex.MatchString(path) }
// Monitoring matchers
func matchLogAnalytics(path string) bool         { return logAnalyticsRegex.MatchString(path) }
func matchAppInsights(path string) bool          { return appInsightsRegex.MatchString(path) }
func matchAutoscaleSetting(path string) bool     { return autoscaleSettingRegex.MatchString(path) }
func matchActionGroup(path string) bool          { return actionGroupRegex.MatchString(path) }
func matchMetricAlert(path string) bool          { return metricAlertRegex.MatchString(path) }
func matchDiagnosticSetting(path string) bool    { return diagnosticSettingRegex.MatchString(path) }

// =============================================================================
// CDN Profile Handler
// =============================================================================

func (s *Server) handleCDNProfile(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	parts := strings.Split(path, "/")

	// Extract components from path
	subscriptionID := parts[2]
	resourceGroup := parts[4]
	profileName := parts[8]

	resourceID := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Cdn/profiles/%s",
		subscriptionID, resourceGroup, profileName)

	switch r.Method {
	case http.MethodPut:
		var req struct {
			Location string            `json:"location"`
			Tags     map[string]string `json:"tags"`
			Sku      CDNSku            `json:"sku"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.badRequest(w, "Invalid request body")
			return
		}

		if req.Sku.Name == "" {
			s.badRequest(w, "sku.name is required")
			return
		}

		profile := CDNProfile{
			ID:       resourceID,
			Name:     profileName,
			Type:     "Microsoft.Cdn/profiles",
			Location: req.Location,
			Tags:     req.Tags,
			Sku:      req.Sku,
			Properties: CDNProfileProps{
				ResourceState:     "Active",
				ProvisioningState: "Succeeded",
			},
		}

		s.store.mu.Lock()
		s.store.cdnProfiles[resourceID] = profile
		s.store.mu.Unlock()

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(profile)

	case http.MethodGet:
		s.store.mu.RLock()
		profile, exists := s.store.cdnProfiles[resourceID]
		s.store.mu.RUnlock()

		if !exists {
			s.resourceNotFound(w, "CDN Profile", profileName)
			return
		}

		json.NewEncoder(w).Encode(profile)

	case http.MethodDelete:
		s.store.mu.Lock()
		delete(s.store.cdnProfiles, resourceID)
		// Also delete associated endpoints
		for k := range s.store.cdnEndpoints {
			if strings.HasPrefix(k, resourceID+"/endpoints/") {
				delete(s.store.cdnEndpoints, k)
			}
		}
		s.store.mu.Unlock()

		w.WriteHeader(http.StatusOK)

	default:
		s.methodNotAllowed(w)
	}
}

// =============================================================================
// CDN Endpoint Handler
// =============================================================================

func (s *Server) handleCDNEndpoint(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	parts := strings.Split(path, "/")

	subscriptionID := parts[2]
	resourceGroup := parts[4]
	profileName := parts[8]
	endpointName := parts[10]

	resourceID := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Cdn/profiles/%s/endpoints/%s",
		subscriptionID, resourceGroup, profileName, endpointName)

	switch r.Method {
	case http.MethodPut:
		var req struct {
			Location   string              `json:"location"`
			Tags       map[string]string   `json:"tags"`
			Properties CDNEndpointProps    `json:"properties"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.badRequest(w, "Invalid request body")
			return
		}

		if len(req.Properties.Origins) == 0 {
			s.badRequest(w, "At least one origin is required")
			return
		}

		endpoint := CDNEndpoint{
			ID:       resourceID,
			Name:     endpointName,
			Type:     "Microsoft.Cdn/profiles/endpoints",
			Location: req.Location,
			Tags:     req.Tags,
			Properties: CDNEndpointProps{
				HostName:             fmt.Sprintf("%s.azureedge.net", endpointName),
				OriginHostHeader:     req.Properties.OriginHostHeader,
				Origins:              req.Properties.Origins,
				OriginPath:           req.Properties.OriginPath,
				IsHttpAllowed:        req.Properties.IsHttpAllowed,
				IsHttpsAllowed:       true,
				IsCompressionEnabled: req.Properties.IsCompressionEnabled,
				ResourceState:        "Running",
				ProvisioningState:    "Succeeded",
				DeliveryPolicy:       req.Properties.DeliveryPolicy,
			},
		}

		s.store.mu.Lock()
		s.store.cdnEndpoints[resourceID] = endpoint
		s.store.mu.Unlock()

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(endpoint)

	case http.MethodGet:
		s.store.mu.RLock()
		endpoint, exists := s.store.cdnEndpoints[resourceID]
		s.store.mu.RUnlock()

		if !exists {
			s.resourceNotFound(w, "CDN Endpoint", endpointName)
			return
		}

		json.NewEncoder(w).Encode(endpoint)

	case http.MethodDelete:
		s.store.mu.Lock()
		delete(s.store.cdnEndpoints, resourceID)
		s.store.mu.Unlock()

		w.WriteHeader(http.StatusOK)

	default:
		s.methodNotAllowed(w)
	}
}

// =============================================================================
// CDN Custom Domain Handler
// =============================================================================

func (s *Server) handleCDNCustomDomain(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	parts := strings.Split(path, "/")

	subscriptionID := parts[2]
	resourceGroup := parts[4]
	profileName := parts[8]
	endpointName := parts[10]
	customDomainName := parts[12]

	resourceID := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Cdn/profiles/%s/endpoints/%s/customDomains/%s",
		subscriptionID, resourceGroup, profileName, endpointName, customDomainName)

	switch r.Method {
	case http.MethodPut:
		var req struct {
			Properties struct {
				HostName string `json:"hostName"`
			} `json:"properties"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.badRequest(w, "Invalid request body")
			return
		}

		if req.Properties.HostName == "" {
			s.badRequest(w, "properties.hostName is required")
			return
		}

		customDomain := CDNCustomDomain{
			ID:   resourceID,
			Name: customDomainName,
			Type: "Microsoft.Cdn/profiles/endpoints/customDomains",
			Properties: CDNCustomDomainProps{
				HostName:          req.Properties.HostName,
				ResourceState:     "Active",
				ProvisioningState: "Succeeded",
			},
		}

		s.store.mu.Lock()
		s.store.cdnCustomDomains[resourceID] = customDomain
		s.store.mu.Unlock()

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(customDomain)

	case http.MethodGet:
		s.store.mu.RLock()
		customDomain, exists := s.store.cdnCustomDomains[resourceID]
		s.store.mu.RUnlock()

		if !exists {
			s.resourceNotFound(w, "CDN Custom Domain", customDomainName)
			return
		}

		json.NewEncoder(w).Encode(customDomain)

	case http.MethodDelete:
		s.store.mu.Lock()
		delete(s.store.cdnCustomDomains, resourceID)
		s.store.mu.Unlock()

		w.WriteHeader(http.StatusOK)

	default:
		s.methodNotAllowed(w)
	}
}

// =============================================================================
// CDN Custom Domain HTTPS Handler
// =============================================================================

func (s *Server) handleCDNOperationResults(w http.ResponseWriter, r *http.Request) {
	// Operation results endpoint - returns the status of an async operation
	// Always return Succeeded to indicate the operation is complete

	if r.Method != http.MethodGet {
		s.methodNotAllowed(w)
		return
	}

	w.Header().Set("x-ms-request-id", fmt.Sprintf("%d", time.Now().UnixNano()))
	w.WriteHeader(http.StatusOK)

	response := map[string]interface{}{
		"status": "Succeeded",
		"properties": map[string]interface{}{
			"customHttpsProvisioningState":    "Enabled",
			"customHttpsProvisioningSubstate": "CertificateDeployed",
		},
	}
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleCDNCustomDomainHttps(w http.ResponseWriter, r *http.Request, enable bool) {
	// enableCustomHttps and disableCustomHttps endpoints
	// These are POST requests to enable/disable HTTPS on a custom domain

	if r.Method != http.MethodPost {
		s.methodNotAllowed(w)
		return
	}

	// Extract resource info from path for the polling URL
	path := r.URL.Path
	// Remove /enableCustomHttps or /disableCustomHttps from path to get custom domain path
	customDomainPath := strings.TrimSuffix(path, "/enableCustomHttps")
	customDomainPath = strings.TrimSuffix(customDomainPath, "/disableCustomHttps")

	// Azure async operations require a Location or Azure-AsyncOperation header for polling
	// The Location header should point to the operation status endpoint
	operationID := fmt.Sprintf("op-%d", time.Now().UnixNano())
	asyncOperationURL := fmt.Sprintf("https://%s%s/operationresults/%s", r.Host, customDomainPath, operationID)

	w.Header().Set("Azure-AsyncOperation", asyncOperationURL)
	w.Header().Set("Location", asyncOperationURL)
	w.Header().Set("x-ms-request-id", fmt.Sprintf("%d", time.Now().UnixNano()))
	w.WriteHeader(http.StatusAccepted)

	// Return a custom domain response with the updated HTTPS state
	response := map[string]interface{}{
		"properties": map[string]interface{}{
			"customHttpsProvisioningState":    "Enabled",
			"customHttpsProvisioningSubstate": "CertificateDeployed",
		},
	}
	if !enable {
		response["properties"].(map[string]interface{})["customHttpsProvisioningState"] = "Disabled"
		response["properties"].(map[string]interface{})["customHttpsProvisioningSubstate"] = ""
	}
	json.NewEncoder(w).Encode(response)
}

// =============================================================================
// DNS Zone Handler
// =============================================================================

func (s *Server) handleDNSZone(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	parts := strings.Split(path, "/")

	subscriptionID := parts[2]
	resourceGroup := parts[4]
	zoneName := parts[8]

	resourceID := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/dnszones/%s",
		subscriptionID, resourceGroup, zoneName)

	switch r.Method {
	case http.MethodPut:
		var req struct {
			Location string            `json:"location"`
			Tags     map[string]string `json:"tags"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		zone := DNSZone{
			ID:       resourceID,
			Name:     zoneName,
			Type:     "Microsoft.Network/dnszones",
			Location: "global",
			Tags:     req.Tags,
			Properties: DNSZoneProps{
				MaxNumberOfRecordSets: 10000,
				NumberOfRecordSets:    2,
				NameServers: []string{
					"ns1-01.azure-dns.com.",
					"ns2-01.azure-dns.net.",
					"ns3-01.azure-dns.org.",
					"ns4-01.azure-dns.info.",
				},
			},
		}

		s.store.mu.Lock()
		s.store.dnsZones[resourceID] = zone
		s.store.mu.Unlock()

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(zone)

	case http.MethodGet:
		s.store.mu.RLock()
		zone, exists := s.store.dnsZones[resourceID]
		s.store.mu.RUnlock()

		if !exists {
			// Return a fake zone for any GET request (like storage account handler)
			// This allows data sources to work without pre-creating the zone
			zone = DNSZone{
				ID:       resourceID,
				Name:     zoneName,
				Type:     "Microsoft.Network/dnszones",
				Location: "global",
				Properties: DNSZoneProps{
					MaxNumberOfRecordSets: 10000,
					NumberOfRecordSets:    2,
					NameServers: []string{
						"ns1-01.azure-dns.com.",
						"ns2-01.azure-dns.net.",
						"ns3-01.azure-dns.org.",
						"ns4-01.azure-dns.info.",
					},
				},
			}
		}

		json.NewEncoder(w).Encode(zone)

	case http.MethodDelete:
		s.store.mu.Lock()
		delete(s.store.dnsZones, resourceID)
		s.store.mu.Unlock()

		w.WriteHeader(http.StatusOK)

	default:
		s.methodNotAllowed(w)
	}
}

// =============================================================================
// DNS CNAME Record Handler
// =============================================================================

func (s *Server) handleDNSCNAMERecord(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	parts := strings.Split(path, "/")

	subscriptionID := parts[2]
	resourceGroup := parts[4]
	zoneName := parts[8]
	recordName := parts[10]

	resourceID := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/dnszones/%s/CNAME/%s",
		subscriptionID, resourceGroup, zoneName, recordName)

	switch r.Method {
	case http.MethodPut:
		var req struct {
			Properties DNSCNAMERecordProps `json:"properties"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.badRequest(w, "Invalid request body")
			return
		}

		if req.Properties.CNAMERecord == nil || req.Properties.CNAMERecord.Cname == "" {
			s.badRequest(w, "CNAMERecord.cname is required")
			return
		}

		record := DNSCNAMERecord{
			ID:   resourceID,
			Name: recordName,
			Type: "Microsoft.Network/dnszones/CNAME",
			Etag: fmt.Sprintf("etag-%d", time.Now().Unix()),
			Properties: DNSCNAMERecordProps{
				TTL:         req.Properties.TTL,
				Fqdn:        fmt.Sprintf("%s.%s.", recordName, zoneName),
				CNAMERecord: req.Properties.CNAMERecord,
			},
		}

		s.store.mu.Lock()
		s.store.dnsCNAMERecords[resourceID] = record
		s.store.mu.Unlock()

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(record)

	case http.MethodGet:
		s.store.mu.RLock()
		record, exists := s.store.dnsCNAMERecords[resourceID]
		s.store.mu.RUnlock()

		if !exists {
			s.resourceNotFound(w, "DNS CNAME Record", recordName)
			return
		}

		json.NewEncoder(w).Encode(record)

	case http.MethodDelete:
		s.store.mu.Lock()
		delete(s.store.dnsCNAMERecords, resourceID)
		s.store.mu.Unlock()

		w.WriteHeader(http.StatusOK)

	default:
		s.methodNotAllowed(w)
	}
}

// =============================================================================
// Storage Account Handler (Read-only for data source)
// =============================================================================

func (s *Server) handleStorageAccount(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	parts := strings.Split(path, "/")

	subscriptionID := parts[2]
	resourceGroup := parts[4]
	accountName := parts[8]

	resourceID := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Storage/storageAccounts/%s",
		subscriptionID, resourceGroup, accountName)

	switch r.Method {
	case http.MethodGet:
		// For data sources, we return a pre-configured storage account
		// The account "exists" as long as it's queried
		account := StorageAccount{
			ID:       resourceID,
			Name:     accountName,
			Type:     "Microsoft.Storage/storageAccounts",
			Location: "eastus",
			Kind:     "StorageV2",
			Sku: StorageSku{
				Name: "Standard_LRS",
				Tier: "Standard",
			},
			Properties: StorageAccountProps{
				PrimaryEndpoints: StorageEndpoints{
					Blob: fmt.Sprintf("https://%s.blob.core.windows.net/", accountName),
					Web:  fmt.Sprintf("https://%s.z13.web.core.windows.net/", accountName),
				},
				ProvisioningState: "Succeeded",
			},
		}

		json.NewEncoder(w).Encode(account)

	case http.MethodPut:
		// Allow creating storage accounts for completeness
		var req struct {
			Location string            `json:"location"`
			Tags     map[string]string `json:"tags"`
			Kind     string            `json:"kind"`
			Sku      StorageSku        `json:"sku"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		account := StorageAccount{
			ID:       resourceID,
			Name:     accountName,
			Type:     "Microsoft.Storage/storageAccounts",
			Location: req.Location,
			Kind:     req.Kind,
			Sku:      req.Sku,
			Properties: StorageAccountProps{
				PrimaryEndpoints: StorageEndpoints{
					Blob: fmt.Sprintf("https://%s.blob.core.windows.net/", accountName),
					Web:  fmt.Sprintf("https://%s.z13.web.core.windows.net/", accountName),
				},
				ProvisioningState: "Succeeded",
			},
		}

		s.store.mu.Lock()
		s.store.storageAccounts[resourceID] = account
		s.store.mu.Unlock()

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(account)

	default:
		s.methodNotAllowed(w)
	}
}

// =============================================================================
// Storage Account Keys Handler
// =============================================================================

func (s *Server) handleStorageAccountKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.methodNotAllowed(w)
		return
	}

	// Return mock storage account keys
	response := map[string]interface{}{
		"keys": []map[string]interface{}{
			{
				"keyName":     "key1",
				"value":       "mock-storage-key-1-base64encodedvalue==",
				"permissions": "FULL",
			},
			{
				"keyName":     "key2",
				"value":       "mock-storage-key-2-base64encodedvalue==",
				"permissions": "FULL",
			},
		},
	}
	json.NewEncoder(w).Encode(response)
}

// =============================================================================
// Blob Storage Handler (for azurerm backend state storage)
// =============================================================================

func (s *Server) handleBlobStorage(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	path := r.URL.Path
	query := r.URL.Query()

	// Extract account name from host (e.g., "devstoreaccount1.blob.core.windows.net" -> "devstoreaccount1")
	accountName := strings.Split(host, ".")[0]

	// Remove leading slash and parse path
	path = strings.TrimPrefix(path, "/")
	parts := strings.SplitN(path, "/", 2)

	containerName := ""
	blobName := ""

	if len(parts) >= 1 && parts[0] != "" {
		containerName = parts[0]
	}
	if len(parts) >= 2 {
		blobName = parts[1]
	}

	log.Printf("Blob Storage: account=%s container=%s blob=%s restype=%s comp=%s", accountName, containerName, blobName, query.Get("restype"), query.Get("comp"))

	// List blobs in container (restype=container&comp=list)
	// Must check this BEFORE container operations since ListBlobs also has restype=container
	if containerName != "" && query.Get("comp") == "list" {
		s.handleListBlobs(w, r, accountName, containerName)
		return
	}

	// Check if this is a container operation (restype=container without comp=list)
	if query.Get("restype") == "container" {
		s.handleBlobContainer(w, r, accountName, containerName)
		return
	}

	// Otherwise, it's a blob operation
	if containerName != "" && blobName != "" {
		s.handleBlob(w, r, accountName, containerName, blobName)
		return
	}

	// Unknown operation
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusBadRequest)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="utf-8"?><Error><Code>InvalidUri</Code><Message>The requested URI does not represent any resource on the server.</Message></Error>`)
}

func (s *Server) handleBlobContainer(w http.ResponseWriter, r *http.Request, accountName, containerName string) {
	containerKey := fmt.Sprintf("%s/%s", accountName, containerName)

	switch r.Method {
	case http.MethodPut:
		// Create container
		now := time.Now().UTC().Format(time.RFC1123)
		etag := fmt.Sprintf("\"0x%X\"", time.Now().UnixNano())

		container := BlobContainer{
			Name: containerName,
			Properties: BlobContainerProps{
				LastModified: now,
				Etag:         etag,
			},
		}

		s.store.mu.Lock()
		s.store.blobContainers[containerKey] = container
		s.store.mu.Unlock()

		w.Header().Set("ETag", etag)
		w.Header().Set("Last-Modified", now)
		w.Header().Set("x-ms-request-id", fmt.Sprintf("%d", time.Now().UnixNano()))
		w.Header().Set("x-ms-version", "2021-06-08")
		w.WriteHeader(http.StatusCreated)

	case http.MethodGet, http.MethodHead:
		// Get container properties
		s.store.mu.RLock()
		container, exists := s.store.blobContainers[containerKey]
		s.store.mu.RUnlock()

		if !exists {
			s.blobNotFound(w, "ContainerNotFound", fmt.Sprintf("The specified container does not exist. Container: %s", containerName))
			return
		}

		w.Header().Set("ETag", container.Properties.Etag)
		w.Header().Set("Last-Modified", container.Properties.LastModified)
		w.Header().Set("x-ms-request-id", fmt.Sprintf("%d", time.Now().UnixNano()))
		w.Header().Set("x-ms-version", "2021-06-08")
		w.Header().Set("x-ms-lease-status", "unlocked")
		w.Header().Set("x-ms-lease-state", "available")
		w.Header().Set("x-ms-has-immutability-policy", "false")
		w.Header().Set("x-ms-has-legal-hold", "false")
		w.WriteHeader(http.StatusOK)

	case http.MethodDelete:
		// Delete container
		s.store.mu.Lock()
		delete(s.store.blobContainers, containerKey)
		// Also delete all blobs in the container
		for k := range s.store.blobs {
			if strings.HasPrefix(k, containerKey+"/") {
				delete(s.store.blobs, k)
			}
		}
		s.store.mu.Unlock()

		w.Header().Set("x-ms-request-id", fmt.Sprintf("%d", time.Now().UnixNano()))
		w.Header().Set("x-ms-version", "2021-06-08")
		w.WriteHeader(http.StatusAccepted)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleBlob(w http.ResponseWriter, r *http.Request, accountName, containerName, blobName string) {
	containerKey := fmt.Sprintf("%s/%s", accountName, containerName)
	blobKey := fmt.Sprintf("%s/%s/%s", accountName, containerName, blobName)
	query := r.URL.Query()

	// Handle lease operations
	if query.Get("comp") == "lease" {
		s.handleBlobLease(w, r, blobKey)
		return
	}

	// Handle metadata operations (used for state locking)
	if query.Get("comp") == "metadata" {
		s.handleBlobMetadata(w, r, blobKey)
		return
	}

	// Handle block blob operations (staged uploads)
	if query.Get("comp") == "block" {
		s.handlePutBlock(w, r, blobKey)
		return
	}

	if query.Get("comp") == "blocklist" {
		s.handleBlockList(w, r, accountName, containerName, blobName, blobKey)
		return
	}

	// Handle blob properties
	if query.Get("comp") == "properties" {
		s.handleBlobProperties(w, r, blobKey)
		return
	}

	switch r.Method {
	case http.MethodPut:
		// Upload blob
		s.store.mu.RLock()
		_, containerExists := s.store.blobContainers[containerKey]
		s.store.mu.RUnlock()

		if !containerExists {
			s.blobNotFound(w, "ContainerNotFound", fmt.Sprintf("The specified container does not exist. Container: %s", containerName))
			return
		}

		// Read request body
		body := make([]byte, 0)
		if r.Body != nil {
			body, _ = io.ReadAll(r.Body)
		}

		now := time.Now().UTC().Format(time.RFC1123)
		etag := fmt.Sprintf("\"0x%X\"", time.Now().UnixNano())
		contentType := r.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "application/octet-stream"
		}

		// Extract metadata from x-ms-meta-* headers
		metadata := make(map[string]string)
		for key, values := range r.Header {
			lowerKey := strings.ToLower(key)
			if strings.HasPrefix(lowerKey, "x-ms-meta-") {
				metaKey := strings.TrimPrefix(lowerKey, "x-ms-meta-")
				if len(values) > 0 {
					metadata[metaKey] = values[0]
				}
			}
		}

		blob := Blob{
			Name:     blobName,
			Content:  body,
			Metadata: metadata,
			Properties: BlobProps{
				LastModified:  now,
				Etag:          etag,
				ContentLength: len(body),
				ContentType:   contentType,
			},
		}

		s.store.mu.Lock()
		s.store.blobs[blobKey] = blob
		s.store.mu.Unlock()

		w.Header().Set("ETag", etag)
		w.Header().Set("Last-Modified", now)
		w.Header().Set("Content-MD5", "")
		w.Header().Set("x-ms-request-id", fmt.Sprintf("%d", time.Now().UnixNano()))
		w.Header().Set("x-ms-version", "2021-06-08")
		w.Header().Set("x-ms-request-server-encrypted", "true")
		w.WriteHeader(http.StatusCreated)

	case http.MethodGet:
		// Download blob
		s.store.mu.RLock()
		blob, exists := s.store.blobs[blobKey]
		s.store.mu.RUnlock()

		if !exists {
			s.blobNotFound(w, "BlobNotFound", fmt.Sprintf("The specified blob does not exist. Blob: %s", blobName))
			return
		}

		w.Header().Set("Content-Type", blob.Properties.ContentType)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", blob.Properties.ContentLength))
		w.Header().Set("ETag", blob.Properties.Etag)
		w.Header().Set("Last-Modified", blob.Properties.LastModified)
		w.Header().Set("x-ms-request-id", fmt.Sprintf("%d", time.Now().UnixNano()))
		w.Header().Set("x-ms-version", "2021-06-08")
		w.Header().Set("x-ms-blob-type", "BlockBlob")
		w.WriteHeader(http.StatusOK)
		w.Write(blob.Content)

	case http.MethodHead:
		// Get blob properties
		s.store.mu.RLock()
		blob, exists := s.store.blobs[blobKey]
		s.store.mu.RUnlock()

		if !exists {
			s.blobNotFound(w, "BlobNotFound", fmt.Sprintf("The specified blob does not exist. Blob: %s", blobName))
			return
		}

		// Return metadata as x-ms-meta-* headers
		for key, value := range blob.Metadata {
			w.Header().Set("x-ms-meta-"+key, value)
		}

		w.Header().Set("Content-Type", blob.Properties.ContentType)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", blob.Properties.ContentLength))
		w.Header().Set("ETag", blob.Properties.Etag)
		w.Header().Set("Last-Modified", blob.Properties.LastModified)
		w.Header().Set("x-ms-request-id", fmt.Sprintf("%d", time.Now().UnixNano()))
		w.Header().Set("x-ms-version", "2021-06-08")
		w.Header().Set("x-ms-blob-type", "BlockBlob")
		w.Header().Set("x-ms-lease-status", "unlocked")
		w.Header().Set("x-ms-lease-state", "available")
		w.WriteHeader(http.StatusOK)

	case http.MethodDelete:
		// Delete blob
		s.store.mu.Lock()
		_, exists := s.store.blobs[blobKey]
		if exists {
			delete(s.store.blobs, blobKey)
		}
		s.store.mu.Unlock()

		if !exists {
			s.blobNotFound(w, "BlobNotFound", fmt.Sprintf("The specified blob does not exist. Blob: %s", blobName))
			return
		}

		w.Header().Set("x-ms-request-id", fmt.Sprintf("%d", time.Now().UnixNano()))
		w.Header().Set("x-ms-version", "2021-06-08")
		w.Header().Set("x-ms-delete-type-permanent", "true")
		w.WriteHeader(http.StatusAccepted)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleBlobMetadata(w http.ResponseWriter, r *http.Request, blobKey string) {
	log.Printf("Blob Metadata: method=%s key=%s", r.Method, blobKey)

	switch r.Method {
	case http.MethodPut:
		// Set blob metadata - used for state locking
		// Extract metadata from x-ms-meta-* headers
		metadata := make(map[string]string)
		for key, values := range r.Header {
			lowerKey := strings.ToLower(key)
			if strings.HasPrefix(lowerKey, "x-ms-meta-") {
				metaKey := strings.TrimPrefix(lowerKey, "x-ms-meta-")
				if len(values) > 0 {
					metadata[metaKey] = values[0]
					log.Printf("Blob Metadata: storing %s=%s", metaKey, values[0])
				}
			}
		}

		s.store.mu.Lock()
		blob, exists := s.store.blobs[blobKey]
		if exists {
			blob.Metadata = metadata
			s.store.blobs[blobKey] = blob
		} else {
			// Create a placeholder blob if it doesn't exist (for lock files)
			now := time.Now().UTC().Format(time.RFC1123)
			etag := fmt.Sprintf("\"0x%X\"", time.Now().UnixNano())
			s.store.blobs[blobKey] = Blob{
				Name:     "",
				Content:  []byte{},
				Metadata: metadata,
				Properties: BlobProps{
					LastModified:  now,
					Etag:          etag,
					ContentLength: 0,
					ContentType:   "application/octet-stream",
				},
			}
		}
		s.store.mu.Unlock()

		w.Header().Set("ETag", fmt.Sprintf("\"0x%X\"", time.Now().UnixNano()))
		w.Header().Set("Last-Modified", time.Now().UTC().Format(time.RFC1123))
		w.Header().Set("x-ms-request-id", fmt.Sprintf("%d", time.Now().UnixNano()))
		w.Header().Set("x-ms-version", "2021-06-08")
		w.Header().Set("x-ms-request-server-encrypted", "true")
		w.WriteHeader(http.StatusOK)

	case http.MethodGet, http.MethodHead:
		// Get blob metadata
		s.store.mu.RLock()
		blob, exists := s.store.blobs[blobKey]
		s.store.mu.RUnlock()

		if !exists {
			s.blobNotFound(w, "BlobNotFound", "The specified blob does not exist.")
			return
		}

		// Return metadata as x-ms-meta-* headers
		for key, value := range blob.Metadata {
			w.Header().Set("x-ms-meta-"+key, value)
			log.Printf("Blob Metadata: returning x-ms-meta-%s=%s", key, value)
		}

		w.Header().Set("ETag", blob.Properties.Etag)
		w.Header().Set("Last-Modified", blob.Properties.LastModified)
		w.Header().Set("x-ms-request-id", fmt.Sprintf("%d", time.Now().UnixNano()))
		w.Header().Set("x-ms-version", "2021-06-08")
		w.WriteHeader(http.StatusOK)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleBlobLease(w http.ResponseWriter, r *http.Request, blobKey string) {
	leaseAction := r.Header.Get("x-ms-lease-action")
	log.Printf("Blob Lease: action=%s key=%s", leaseAction, blobKey)

	switch leaseAction {
	case "acquire":
		// Acquire lease - return a mock lease ID
		leaseID := fmt.Sprintf("lease-%d", time.Now().UnixNano())
		w.Header().Set("x-ms-lease-id", leaseID)
		w.Header().Set("x-ms-request-id", fmt.Sprintf("%d", time.Now().UnixNano()))
		w.Header().Set("x-ms-version", "2021-06-08")
		w.WriteHeader(http.StatusCreated)

	case "release", "break":
		// Release or break lease
		w.Header().Set("x-ms-request-id", fmt.Sprintf("%d", time.Now().UnixNano()))
		w.Header().Set("x-ms-version", "2021-06-08")
		w.WriteHeader(http.StatusOK)

	case "renew":
		// Renew lease
		leaseID := r.Header.Get("x-ms-lease-id")
		w.Header().Set("x-ms-lease-id", leaseID)
		w.Header().Set("x-ms-request-id", fmt.Sprintf("%d", time.Now().UnixNano()))
		w.Header().Set("x-ms-version", "2021-06-08")
		w.WriteHeader(http.StatusOK)

	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}

func (s *Server) handlePutBlock(w http.ResponseWriter, r *http.Request, blobKey string) {
	blockID := r.URL.Query().Get("blockid")
	log.Printf("Put Block: key=%s blockid=%s", blobKey, blockID)

	if r.Method != http.MethodPut {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Read block data
	body, _ := io.ReadAll(r.Body)

	// Store the block
	blockKey := fmt.Sprintf("%s/%s", blobKey, blockID)
	s.store.mu.Lock()
	s.store.blobBlocks[blockKey] = body
	s.store.mu.Unlock()

	w.Header().Set("x-ms-request-id", fmt.Sprintf("%d", time.Now().UnixNano()))
	w.Header().Set("x-ms-version", "2021-06-08")
	w.Header().Set("x-ms-content-crc64", "")
	w.Header().Set("x-ms-request-server-encrypted", "true")
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) handleBlockList(w http.ResponseWriter, r *http.Request, accountName, containerName, blobName, blobKey string) {
	log.Printf("Block List: method=%s key=%s", r.Method, blobKey)

	switch r.Method {
	case http.MethodPut:
		// Commit block list - assemble blocks into final blob
		// For simplicity, we just create an empty blob (the actual block assembly would be complex)
		// The terraform state is typically small enough to not use block uploads
		body, _ := io.ReadAll(r.Body)
		log.Printf("Block List body: %s", string(body))

		now := time.Now().UTC().Format(time.RFC1123)
		etag := fmt.Sprintf("\"0x%X\"", time.Now().UnixNano())

		// Create the blob (simplified - in reality would assemble from blocks)
		blob := Blob{
			Name:    blobName,
			Content: []byte{}, // Would normally assemble from blocks
			Properties: BlobProps{
				LastModified:  now,
				Etag:          etag,
				ContentLength: 0,
				ContentType:   "application/octet-stream",
			},
		}

		s.store.mu.Lock()
		s.store.blobs[blobKey] = blob
		// Clean up staged blocks
		for k := range s.store.blobBlocks {
			if strings.HasPrefix(k, blobKey+"/") {
				delete(s.store.blobBlocks, k)
			}
		}
		s.store.mu.Unlock()

		w.Header().Set("ETag", etag)
		w.Header().Set("Last-Modified", now)
		w.Header().Set("x-ms-request-id", fmt.Sprintf("%d", time.Now().UnixNano()))
		w.Header().Set("x-ms-version", "2021-06-08")
		w.Header().Set("x-ms-request-server-encrypted", "true")
		w.WriteHeader(http.StatusCreated)

	case http.MethodGet:
		// Get block list
		w.Header().Set("Content-Type", "application/xml")
		w.Header().Set("x-ms-request-id", fmt.Sprintf("%d", time.Now().UnixNano()))
		w.Header().Set("x-ms-version", "2021-06-08")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `<?xml version="1.0" encoding="utf-8"?><BlockList><CommittedBlocks></CommittedBlocks><UncommittedBlocks></UncommittedBlocks></BlockList>`)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleBlobProperties(w http.ResponseWriter, r *http.Request, blobKey string) {
	log.Printf("Blob Properties: method=%s key=%s", r.Method, blobKey)

	s.store.mu.RLock()
	blob, exists := s.store.blobs[blobKey]
	s.store.mu.RUnlock()

	if !exists {
		s.blobNotFound(w, "BlobNotFound", "The specified blob does not exist.")
		return
	}

	switch r.Method {
	case http.MethodPut:
		// Set blob properties
		w.Header().Set("ETag", blob.Properties.Etag)
		w.Header().Set("Last-Modified", blob.Properties.LastModified)
		w.Header().Set("x-ms-request-id", fmt.Sprintf("%d", time.Now().UnixNano()))
		w.Header().Set("x-ms-version", "2021-06-08")
		w.WriteHeader(http.StatusOK)

	case http.MethodGet, http.MethodHead:
		// Get blob properties
		w.Header().Set("Content-Type", blob.Properties.ContentType)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", blob.Properties.ContentLength))
		w.Header().Set("ETag", blob.Properties.Etag)
		w.Header().Set("Last-Modified", blob.Properties.LastModified)
		w.Header().Set("x-ms-request-id", fmt.Sprintf("%d", time.Now().UnixNano()))
		w.Header().Set("x-ms-version", "2021-06-08")
		w.Header().Set("x-ms-blob-type", "BlockBlob")
		w.WriteHeader(http.StatusOK)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListBlobs(w http.ResponseWriter, r *http.Request, accountName, containerName string) {
	containerKey := fmt.Sprintf("%s/%s", accountName, containerName)
	prefix := containerKey + "/"

	s.store.mu.RLock()
	_, containerExists := s.store.blobContainers[containerKey]
	var blobs []Blob
	for k, b := range s.store.blobs {
		if strings.HasPrefix(k, prefix) {
			blobs = append(blobs, b)
		}
	}
	s.store.mu.RUnlock()

	if !containerExists {
		s.blobNotFound(w, "ContainerNotFound", fmt.Sprintf("The specified container does not exist. Container: %s", containerName))
		return
	}

	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("x-ms-request-id", fmt.Sprintf("%d", time.Now().UnixNano()))
	w.Header().Set("x-ms-version", "2021-06-08")
	w.WriteHeader(http.StatusOK)

	fmt.Fprintf(w, `<?xml version="1.0" encoding="utf-8"?><EnumerationResults ServiceEndpoint="https://%s.blob.core.windows.net/" ContainerName="%s"><Blobs>`, accountName, containerName)
	for _, b := range blobs {
		fmt.Fprintf(w, `<Blob><Name>%s</Name><Properties><Content-Length>%d</Content-Length><Content-Type>%s</Content-Type><Last-Modified>%s</Last-Modified><Etag>%s</Etag><BlobType>BlockBlob</BlobType><LeaseStatus>unlocked</LeaseStatus><LeaseState>available</LeaseState></Properties></Blob>`,
			b.Name, b.Properties.ContentLength, b.Properties.ContentType, b.Properties.LastModified, b.Properties.Etag)
	}
	fmt.Fprintf(w, `</Blobs><NextMarker/></EnumerationResults>`)
}

func (s *Server) blobNotFound(w http.ResponseWriter, code, message string) {
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("x-ms-request-id", fmt.Sprintf("%d", time.Now().UnixNano()))
	w.Header().Set("x-ms-version", "2021-06-08")
	w.WriteHeader(http.StatusNotFound)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="utf-8"?><Error><Code>%s</Code><Message>%s</Message></Error>`, code, message)
}

// =============================================================================
// App Service Plan Handler
// =============================================================================

func (s *Server) handleAppServicePlan(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	parts := strings.Split(path, "/")

	subscriptionID := parts[2]
	resourceGroup := parts[4]
	planName := parts[8]

	// Build canonical resource ID (lowercase path for consistent storage key)
	resourceID := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Web/serverfarms/%s",
		subscriptionID, resourceGroup, planName)
	// Use lowercase key for storage to handle case-insensitive lookups
	storeKey := strings.ToLower(resourceID)

	switch r.Method {
	case http.MethodPut:
		var req struct {
			Location   string            `json:"location"`
			Tags       map[string]string `json:"tags"`
			Kind       string            `json:"kind"`
			Sku        AppServiceSku     `json:"sku"`
			Properties struct {
				PerSiteScaling bool `json:"perSiteScaling"`
				ZoneRedundant  bool `json:"zoneRedundant"`
				Reserved       bool `json:"reserved"`
			} `json:"properties"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.badRequest(w, "Invalid request body")
			return
		}

		// Derive SKU tier from name
		skuTier := "Standard"
		if strings.HasPrefix(req.Sku.Name, "P") {
			skuTier = "PremiumV3"
		} else if strings.HasPrefix(req.Sku.Name, "B") {
			skuTier = "Basic"
		} else if strings.HasPrefix(req.Sku.Name, "F") {
			skuTier = "Free"
		}

		plan := AppServicePlan{
			ID:       resourceID,
			Name:     planName,
			Type:     "Microsoft.Web/serverfarms",
			Location: req.Location,
			Tags:     req.Tags,
			Kind:     req.Kind,
			Sku: AppServiceSku{
				Name:     req.Sku.Name,
				Tier:     skuTier,
				Size:     req.Sku.Name,
				Family:   string(req.Sku.Name[0]),
				Capacity: 1,
			},
			Properties: AppServicePlanProps{
				ProvisioningState:      "Succeeded",
				Status:                 "Ready",
				MaximumNumberOfWorkers: 10,
				NumberOfSites:          0,
				PerSiteScaling:         req.Properties.PerSiteScaling,
				ZoneRedundant:          req.Properties.ZoneRedundant,
				Reserved:               req.Properties.Reserved,
			},
		}

		s.store.mu.Lock()
		s.store.appServicePlans[storeKey] = plan
		s.store.mu.Unlock()

		// Azure SDK for azurerm provider expects 200 for PUT operations
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(plan)

	case http.MethodGet:
		s.store.mu.RLock()
		plan, exists := s.store.appServicePlans[storeKey]
		s.store.mu.RUnlock()

		if !exists {
			s.resourceNotFound(w, "App Service Plan", planName)
			return
		}

		json.NewEncoder(w).Encode(plan)

	case http.MethodDelete:
		s.store.mu.Lock()
		delete(s.store.appServicePlans, storeKey)
		s.store.mu.Unlock()

		w.WriteHeader(http.StatusOK)

	default:
		s.methodNotAllowed(w)
	}
}

// =============================================================================
// Web App Auth Settings Handler
// =============================================================================

func (s *Server) handleWebAppAuthSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.methodNotAllowed(w)
		return
	}

	// Return default disabled auth settings
	response := map[string]interface{}{
		"id":   r.URL.Path,
		"name": "authsettings",
		"type": "Microsoft.Web/sites/config",
		"properties": map[string]interface{}{
			"enabled":                       false,
			"runtimeVersion":                "~1",
			"unauthenticatedClientAction":   "RedirectToLoginPage",
			"tokenStoreEnabled":             false,
			"allowedExternalRedirectUrls":   []string{},
			"defaultProvider":               "AzureActiveDirectory",
			"clientId":                      nil,
			"issuer":                        nil,
			"allowedAudiences":              nil,
			"additionalLoginParams":         nil,
			"isAadAutoProvisioned":          false,
			"aadClaimsAuthorization":        nil,
			"googleClientId":                nil,
			"facebookAppId":                 nil,
			"gitHubClientId":                nil,
			"twitterConsumerKey":            nil,
			"microsoftAccountClientId":      nil,
		},
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// =============================================================================
// Web App Auth Settings V2 Handler
// =============================================================================

func (s *Server) handleWebAppAuthSettingsV2(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.methodNotAllowed(w)
		return
	}

	// Return default disabled auth settings V2
	response := map[string]interface{}{
		"id":   r.URL.Path,
		"name": "authsettingsV2",
		"type": "Microsoft.Web/sites/config",
		"properties": map[string]interface{}{
			"platform": map[string]interface{}{
				"enabled":        false,
				"runtimeVersion": "~1",
			},
			"globalValidation": map[string]interface{}{
				"requireAuthentication":       false,
				"unauthenticatedClientAction": "RedirectToLoginPage",
			},
			"identityProviders": map[string]interface{}{},
			"login": map[string]interface{}{
				"routes":                     map[string]interface{}{},
				"tokenStore":                 map[string]interface{}{"enabled": false},
				"preserveUrlFragmentsForLogins": false,
			},
			"httpSettings": map[string]interface{}{
				"requireHttps": true,
			},
		},
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// =============================================================================
// Web App App Settings Handler
// =============================================================================

func (s *Server) handleWebAppAppSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.methodNotAllowed(w)
		return
	}

	// Return empty app settings
	response := map[string]interface{}{
		"id":         r.URL.Path,
		"name":       "appsettings",
		"type":       "Microsoft.Web/sites/config",
		"properties": map[string]string{},
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// =============================================================================
// Web App Connection Strings Handler
// =============================================================================

func (s *Server) handleWebAppConnStrings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.methodNotAllowed(w)
		return
	}

	// Return empty connection strings
	response := map[string]interface{}{
		"id":         r.URL.Path,
		"name":       "connectionstrings",
		"type":       "Microsoft.Web/sites/config",
		"properties": map[string]interface{}{},
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// =============================================================================
// Web App Sticky Settings Handler
// =============================================================================

func (s *Server) handleWebAppStickySettings(w http.ResponseWriter, r *http.Request) {
	// Handle both GET and PUT methods
	if r.Method != http.MethodGet && r.Method != http.MethodPut {
		s.methodNotAllowed(w)
		return
	}

	// Return default sticky settings
	response := map[string]interface{}{
		"id":   r.URL.Path,
		"name": "slotConfigNames",
		"type": "Microsoft.Web/sites/config",
		"properties": map[string]interface{}{
			"appSettingNames":              []string{},
			"connectionStringNames":        []string{},
			"azureStorageConfigNames":      []string{},
		},
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// =============================================================================
// Web App Config Logs Handler
// =============================================================================

func (s *Server) handleWebAppConfigLogs(w http.ResponseWriter, r *http.Request) {
	// Handle both GET and PUT methods
	if r.Method != http.MethodGet && r.Method != http.MethodPut {
		s.methodNotAllowed(w)
		return
	}

	// Return default logging configuration
	response := map[string]interface{}{
		"id":   r.URL.Path,
		"name": "logs",
		"type": "Microsoft.Web/sites/config",
		"properties": map[string]interface{}{
			"applicationLogs": map[string]interface{}{
				"fileSystem": map[string]interface{}{
					"level": "Off",
				},
				"azureBlobStorage": nil,
				"azureTableStorage": nil,
			},
			"httpLogs": map[string]interface{}{
				"fileSystem": map[string]interface{}{
					"retentionInMb":   35,
					"retentionInDays": 0,
					"enabled":         false,
				},
				"azureBlobStorage": nil,
			},
			"failedRequestsTracing": map[string]interface{}{
				"enabled": false,
			},
			"detailedErrorMessages": map[string]interface{}{
				"enabled": false,
			},
		},
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// =============================================================================
// Web App Storage Accounts Handler
// =============================================================================

func (s *Server) handleWebAppStorageAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.methodNotAllowed(w)
		return
	}

	// Return empty storage accounts
	response := map[string]interface{}{
		"id":         r.URL.Path,
		"name":       "azurestorageaccounts",
		"type":       "Microsoft.Web/sites/config",
		"properties": map[string]interface{}{},
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// =============================================================================
// Web App Backups Handler
// =============================================================================

func (s *Server) handleWebAppBackups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.methodNotAllowed(w)
		return
	}

	// Return empty backup config (no backup configured)
	response := map[string]interface{}{
		"id":   r.URL.Path,
		"name": "backup",
		"type": "Microsoft.Web/sites/config",
		"properties": map[string]interface{}{
			"backupName":          nil,
			"enabled":             false,
			"storageAccountUrl":   nil,
			"backupSchedule":      nil,
			"databases":           []interface{}{},
		},
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// =============================================================================
// Web App Metadata Handler
// =============================================================================

func (s *Server) handleWebAppMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.methodNotAllowed(w)
		return
	}

	// Return empty metadata
	response := map[string]interface{}{
		"id":         r.URL.Path,
		"name":       "metadata",
		"type":       "Microsoft.Web/sites/config",
		"properties": map[string]interface{}{},
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// =============================================================================
// Web App Publishing Credentials Handler
// =============================================================================

func (s *Server) handleWebAppPubCreds(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.methodNotAllowed(w)
		return
	}

	path := r.URL.Path
	parts := strings.Split(path, "/")
	appName := parts[8]

	// Return publishing credentials
	response := map[string]interface{}{
		"id":   path,
		"name": "publishingcredentials",
		"type": "Microsoft.Web/sites/config",
		"properties": map[string]interface{}{
			"name":                  "$" + appName,
			"publishingUserName":    "$" + appName,
			"publishingPassword":    "mock-publishing-password",
			"scmUri":                fmt.Sprintf("https://%s.scm.azurewebsites.net", appName),
		},
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// =============================================================================
// Web App Config Fallback Handler (for any unhandled config endpoints)
// =============================================================================

func (s *Server) handleWebAppConfigFallback(w http.ResponseWriter, r *http.Request) {
	// This handles any config endpoint we haven't explicitly implemented
	// Return an empty properties response which should work for most cases
	path := r.URL.Path

	// Extract config name from path
	parts := strings.Split(path, "/")
	configName := "unknown"
	for i, p := range parts {
		if p == "config" && i+1 < len(parts) {
			configName = parts[i+1]
			break
		}
	}

	response := map[string]interface{}{
		"id":         path,
		"name":       configName,
		"type":       "Microsoft.Web/sites/config",
		"properties": map[string]interface{}{},
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// =============================================================================
// Web App Basic Auth Policy Handler (ftp/scm publishing credentials)
// =============================================================================

func (s *Server) handleWebAppBasicAuthPolicy(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	parts := strings.Split(path, "/")
	policyType := parts[len(parts)-1] // "ftp" or "scm"

	if r.Method != http.MethodGet && r.Method != http.MethodPut {
		s.methodNotAllowed(w)
		return
	}

	// Return policy that allows basic auth
	response := map[string]interface{}{
		"id":   path,
		"name": policyType,
		"type": "Microsoft.Web/sites/basicPublishingCredentialsPolicies",
		"properties": map[string]interface{}{
			"allow": true,
		},
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// =============================================================================
// Web App Traffic Routing Handler
// Handles az webapp traffic-routing set/clear/show commands
// =============================================================================

func (s *Server) handleWebAppTrafficRouting(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	parts := strings.Split(path, "/")

	subscriptionID := parts[2]
	resourceGroup := parts[4]
	appName := parts[8]

	// Key for storing traffic routing rules
	routingKey := fmt.Sprintf("%s:%s:%s", subscriptionID, resourceGroup, appName)

	switch r.Method {
	case http.MethodGet:
		// Return current traffic routing rules
		s.store.mu.RLock()
		rules, exists := s.store.trafficRouting[routingKey]
		s.store.mu.RUnlock()

		if !exists {
			// Return empty routing rules
			response := []TrafficRoutingRule{}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(response)
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(rules)

	case http.MethodPost:
		// Set traffic routing (from az webapp traffic-routing set)
		var req struct {
			SlotName       string `json:"slotName"`
			TrafficPercent int    `json:"trafficPercent"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.badRequest(w, "Invalid request body")
			return
		}

		// Store the traffic routing rule
		rules := []TrafficRoutingRule{
			{
				ActionHostName:    fmt.Sprintf("%s-%s.azurewebsites.net", appName, req.SlotName),
				ReroutePercentage: req.TrafficPercent,
				Name:              req.SlotName,
			},
		}

		s.store.mu.Lock()
		s.store.trafficRouting[routingKey] = rules
		s.store.mu.Unlock()

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(rules)

	case http.MethodDelete:
		// Clear traffic routing (from az webapp traffic-routing clear)
		s.store.mu.Lock()
		delete(s.store.trafficRouting, routingKey)
		s.store.mu.Unlock()

		// Return empty array
		response := []TrafficRoutingRule{}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)

	default:
		s.methodNotAllowed(w)
	}
}

// =============================================================================
// Web App Check Name Availability Handler
// =============================================================================

func (s *Server) handleWebAppCheckName(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.methodNotAllowed(w)
		return
	}

	var req struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.badRequest(w, "Invalid request body")
		return
	}

	// Always return that the name is available (for testing purposes)
	response := struct {
		NameAvailable bool   `json:"nameAvailable"`
		Reason        string `json:"reason,omitempty"`
		Message       string `json:"message,omitempty"`
	}{
		NameAvailable: true,
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// =============================================================================
// Linux Web App Handler
// =============================================================================

func (s *Server) handleLinuxWebApp(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	parts := strings.Split(path, "/")

	subscriptionID := parts[2]
	resourceGroup := parts[4]
	appName := parts[8]

	resourceID := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Web/sites/%s",
		subscriptionID, resourceGroup, appName)
	// Use lowercase key for storage to handle case-insensitive lookups
	storeKey := strings.ToLower(resourceID)

	switch r.Method {
	case http.MethodPut:
		var req struct {
			Location   string            `json:"location"`
			Tags       map[string]string `json:"tags"`
			Kind       string            `json:"kind"`
			Identity   *AppIdentity      `json:"identity"`
			Properties struct {
				ServerFarmID          string `json:"serverFarmId"`
				HTTPSOnly             bool   `json:"httpsOnly"`
				ClientAffinityEnabled bool   `json:"clientAffinityEnabled"`
				SiteConfig            *WebAppSiteConfig `json:"siteConfig"`
			} `json:"properties"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.badRequest(w, "Invalid request body")
			return
		}

		// Generate mock identity if system-assigned requested
		var identity *AppIdentity
		if req.Identity != nil && (req.Identity.Type == "SystemAssigned" || req.Identity.Type == "SystemAssigned, UserAssigned") {
			identity = &AppIdentity{
				Type:        req.Identity.Type,
				PrincipalID: fmt.Sprintf("principal-%s", appName),
				TenantID:    "mock-tenant-id",
				UserIDs:     req.Identity.UserIDs,
			}
		} else if req.Identity != nil {
			identity = req.Identity
		}

		app := LinuxWebApp{
			ID:       resourceID,
			Name:     appName,
			Type:     "Microsoft.Web/sites",
			Location: req.Location,
			Tags:     req.Tags,
			Kind:     req.Kind,
			Identity: identity,
			Properties: LinuxWebAppProps{
				ProvisioningState:           "Succeeded",
				State:                       "Running",
				DefaultHostName:             fmt.Sprintf("%s.azurewebsites.net", appName),
				ServerFarmID:                req.Properties.ServerFarmID,
				HTTPSOnly:                   req.Properties.HTTPSOnly,
				ClientAffinityEnabled:       req.Properties.ClientAffinityEnabled,
				OutboundIPAddresses:         "20.42.0.1,20.42.0.2,20.42.0.3",
				PossibleOutboundIPAddresses: "20.42.0.1,20.42.0.2,20.42.0.3,20.42.0.4,20.42.0.5",
				CustomDomainVerificationID:  fmt.Sprintf("verification-id-%s", appName),
				SiteConfig:                  req.Properties.SiteConfig,
			},
		}

		s.store.mu.Lock()
		s.store.linuxWebApps[storeKey] = app
		s.store.mu.Unlock()

		// Azure SDK for azurerm provider expects 200 for PUT operations
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(app)

	case http.MethodGet:
		s.store.mu.RLock()
		app, exists := s.store.linuxWebApps[storeKey]
		s.store.mu.RUnlock()

		if !exists {
			s.resourceNotFound(w, "Web App", appName)
			return
		}

		json.NewEncoder(w).Encode(app)

	case http.MethodDelete:
		s.store.mu.Lock()
		delete(s.store.linuxWebApps, storeKey)
		// Also delete associated slots (use lowercase prefix for consistency)
		slotPrefix := strings.ToLower(resourceID + "/slots/")
		for k := range s.store.webAppSlots {
			if strings.HasPrefix(strings.ToLower(k), slotPrefix) {
				delete(s.store.webAppSlots, k)
			}
		}
		s.store.mu.Unlock()

		w.WriteHeader(http.StatusOK)

	default:
		s.methodNotAllowed(w)
	}
}

// =============================================================================
// Web App Config Handler
// =============================================================================

func (s *Server) handleWebAppConfig(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	parts := strings.Split(path, "/")

	subscriptionID := parts[2]
	resourceGroup := parts[4]
	appName := parts[8]

	appResourceID := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Web/sites/%s",
		subscriptionID, resourceGroup, appName)
	// Use lowercase key for storage to handle case-insensitive lookups
	storeKey := strings.ToLower(appResourceID)

	switch r.Method {
	case http.MethodPut, http.MethodPatch:
		var req struct {
			Properties WebAppSiteConfig `json:"properties"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.badRequest(w, "Invalid request body")
			return
		}

		s.store.mu.Lock()
		if app, exists := s.store.linuxWebApps[storeKey]; exists {
			app.Properties.SiteConfig = &req.Properties
			s.store.linuxWebApps[storeKey] = app
		}
		s.store.mu.Unlock()

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"properties": req.Properties,
		})

	case http.MethodGet:
		s.store.mu.RLock()
		app, exists := s.store.linuxWebApps[storeKey]
		s.store.mu.RUnlock()

		if !exists {
			s.resourceNotFound(w, "Web App", appName)
			return
		}

		config := app.Properties.SiteConfig
		if config == nil {
			config = &WebAppSiteConfig{}
		}
		// Ensure Experiments is always initialized (Azure CLI expects it for traffic routing)
		if config.Experiments == nil {
			config.Experiments = &WebAppExperiments{
				RampUpRules: []RampUpRule{},
			}
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"properties": config,
		})

	default:
		s.methodNotAllowed(w)
	}
}

// =============================================================================
// Web App Slot Handler
// =============================================================================

func (s *Server) handleWebAppSlot(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	parts := strings.Split(path, "/")

	subscriptionID := parts[2]
	resourceGroup := parts[4]
	appName := parts[8]
	slotName := parts[10]

	resourceID := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Web/sites/%s/slots/%s",
		subscriptionID, resourceGroup, appName, slotName)

	switch r.Method {
	case http.MethodPut:
		var req struct {
			Location   string            `json:"location"`
			Tags       map[string]string `json:"tags"`
			Kind       string            `json:"kind"`
			Properties struct {
				ServerFarmID string `json:"serverFarmId"`
				SiteConfig   *WebAppSiteConfig `json:"siteConfig"`
			} `json:"properties"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.badRequest(w, "Invalid request body")
			return
		}

		slot := WebAppSlot{
			ID:       resourceID,
			Name:     fmt.Sprintf("%s/%s", appName, slotName),
			Type:     "Microsoft.Web/sites/slots",
			Location: req.Location,
			Tags:     req.Tags,
			Kind:     req.Kind,
			Properties: LinuxWebAppProps{
				ProvisioningState:           "Succeeded",
				State:                       "Running",
				DefaultHostName:             fmt.Sprintf("%s-%s.azurewebsites.net", appName, slotName),
				ServerFarmID:                req.Properties.ServerFarmID,
				OutboundIPAddresses:         "20.42.0.1,20.42.0.2,20.42.0.3",
				PossibleOutboundIPAddresses: "20.42.0.1,20.42.0.2,20.42.0.3,20.42.0.4,20.42.0.5",
				CustomDomainVerificationID:  fmt.Sprintf("verification-id-%s-%s", appName, slotName),
				SiteConfig:                  req.Properties.SiteConfig,
			},
		}

		s.store.mu.Lock()
		s.store.webAppSlots[resourceID] = slot
		s.store.mu.Unlock()

		// Azure SDK for azurerm provider expects 200 for PUT operations
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(slot)

	case http.MethodGet:
		s.store.mu.RLock()
		slot, exists := s.store.webAppSlots[resourceID]
		s.store.mu.RUnlock()

		if !exists {
			s.resourceNotFound(w, "Web App Slot", slotName)
			return
		}

		json.NewEncoder(w).Encode(slot)

	case http.MethodDelete:
		s.store.mu.Lock()
		delete(s.store.webAppSlots, resourceID)
		s.store.mu.Unlock()

		w.WriteHeader(http.StatusOK)

	default:
		s.methodNotAllowed(w)
	}
}

// =============================================================================
// Web App Slot Config Handler
// =============================================================================

func (s *Server) handleWebAppSlotConfig(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	parts := strings.Split(path, "/")

	subscriptionID := parts[2]
	resourceGroup := parts[4]
	appName := parts[8]
	slotName := parts[10]

	slotResourceID := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Web/sites/%s/slots/%s",
		subscriptionID, resourceGroup, appName, slotName)

	switch r.Method {
	case http.MethodGet:
		// Return the site config from the stored slot
		s.store.mu.RLock()
		slot, exists := s.store.webAppSlots[slotResourceID]
		s.store.mu.RUnlock()

		if !exists {
			s.resourceNotFound(w, "Web App Slot", slotName)
			return
		}

		// Return site config
		config := struct {
			ID         string            `json:"id"`
			Name       string            `json:"name"`
			Type       string            `json:"type"`
			Properties *WebAppSiteConfig `json:"properties"`
		}{
			ID:         slotResourceID + "/config/web",
			Name:       "web",
			Type:       "Microsoft.Web/sites/slots/config",
			Properties: slot.Properties.SiteConfig,
		}

		// If no site config stored, return a default
		if config.Properties == nil {
			config.Properties = &WebAppSiteConfig{
				AlwaysOn:          false,
				HTTP20Enabled:     true,
				MinTLSVersion:     "1.2",
				FtpsState:         "Disabled",
				LinuxFxVersion:    "DOCKER|nginx:latest",
				WebSocketsEnabled: false,
			}
		}
		// Ensure Experiments is always initialized (Azure CLI expects it for traffic routing)
		if config.Properties.Experiments == nil {
			config.Properties.Experiments = &WebAppExperiments{
				RampUpRules: []RampUpRule{},
			}
		}

		json.NewEncoder(w).Encode(config)

	case http.MethodPut:
		var req struct {
			Properties *WebAppSiteConfig `json:"properties"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.badRequest(w, "Invalid request body")
			return
		}

		// Update the slot's site config
		s.store.mu.Lock()
		if slot, exists := s.store.webAppSlots[slotResourceID]; exists {
			slot.Properties.SiteConfig = req.Properties
			s.store.webAppSlots[slotResourceID] = slot
		}
		s.store.mu.Unlock()

		config := struct {
			ID         string            `json:"id"`
			Name       string            `json:"name"`
			Type       string            `json:"type"`
			Properties *WebAppSiteConfig `json:"properties"`
		}{
			ID:         slotResourceID + "/config/web",
			Name:       "web",
			Type:       "Microsoft.Web/sites/slots/config",
			Properties: req.Properties,
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(config)

	default:
		s.methodNotAllowed(w)
	}
}

// =============================================================================
// Web App Slot Config Fallback Handler
// Handles various slot config endpoints like appSettings, connectionstrings, etc.
// =============================================================================

func (s *Server) handleWebAppSlotConfigFallback(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	parts := strings.Split(path, "/")

	subscriptionID := parts[2]
	resourceGroup := parts[4]
	appName := parts[8]
	slotName := parts[10]
	configType := parts[12]

	slotResourceID := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Web/sites/%s/slots/%s",
		subscriptionID, resourceGroup, appName, slotName)

	// Check if slot exists
	s.store.mu.RLock()
	_, exists := s.store.webAppSlots[slotResourceID]
	s.store.mu.RUnlock()

	if !exists {
		s.resourceNotFound(w, "Web App Slot", slotName)
		return
	}

	// Return empty/default response for various config types
	switch configType {
	case "appSettings":
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":         slotResourceID + "/config/appSettings",
			"name":       "appSettings",
			"type":       "Microsoft.Web/sites/slots/config",
			"properties": map[string]string{},
		})
	case "connectionstrings":
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":         slotResourceID + "/config/connectionstrings",
			"name":       "connectionstrings",
			"type":       "Microsoft.Web/sites/slots/config",
			"properties": map[string]interface{}{},
		})
	case "authsettings":
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":   slotResourceID + "/config/authsettings",
			"name": "authsettings",
			"type": "Microsoft.Web/sites/slots/config",
			"properties": map[string]interface{}{
				"enabled": false,
			},
		})
	case "authsettingsV2":
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":   slotResourceID + "/config/authsettingsV2",
			"name": "authsettingsV2",
			"type": "Microsoft.Web/sites/slots/config",
			"properties": map[string]interface{}{
				"platform": map[string]interface{}{
					"enabled": false,
				},
			},
		})
	case "logs":
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":   slotResourceID + "/config/logs",
			"name": "logs",
			"type": "Microsoft.Web/sites/slots/config",
			"properties": map[string]interface{}{
				"applicationLogs": map[string]interface{}{
					"fileSystem": map[string]interface{}{
						"level": "Off",
					},
				},
				"httpLogs": map[string]interface{}{
					"fileSystem": map[string]interface{}{
						"enabled": false,
					},
				},
				"detailedErrorMessages": map[string]interface{}{
					"enabled": false,
				},
				"failedRequestsTracing": map[string]interface{}{
					"enabled": false,
				},
			},
		})
	case "slotConfigNames":
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":   slotResourceID + "/config/slotConfigNames",
			"name": "slotConfigNames",
			"type": "Microsoft.Web/sites/slots/config",
			"properties": map[string]interface{}{
				"appSettingNames":       []string{},
				"connectionStringNames": []string{},
			},
		})
	case "azurestorageaccounts":
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":         slotResourceID + "/config/azurestorageaccounts",
			"name":       "azurestorageaccounts",
			"type":       "Microsoft.Web/sites/slots/config",
			"properties": map[string]interface{}{},
		})
	case "backup":
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":   slotResourceID + "/config/backup",
			"name": "backup",
			"type": "Microsoft.Web/sites/slots/config",
			"properties": map[string]interface{}{
				"enabled": false,
			},
		})
	case "metadata":
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":         slotResourceID + "/config/metadata",
			"name":       "metadata",
			"type":       "Microsoft.Web/sites/slots/config",
			"properties": map[string]interface{}{},
		})
	case "publishingcredentials":
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":   slotResourceID + "/config/publishingcredentials",
			"name": "publishingcredentials",
			"type": "Microsoft.Web/sites/slots/config",
			"properties": map[string]interface{}{
				"publishingUserName": fmt.Sprintf("$%s__%s", appName, slotName),
				"publishingPassword": "mock-password",
			},
		})
	default:
		// Generic empty response for unknown config types
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":         fmt.Sprintf("%s/config/%s", slotResourceID, configType),
			"name":       configType,
			"type":       "Microsoft.Web/sites/slots/config",
			"properties": map[string]interface{}{},
		})
	}
}

// =============================================================================
// Web App Slot Basic Auth Policy Handler
// Handles /sites/{app}/slots/{slot}/basicPublishingCredentialsPolicies/(ftp|scm)
// =============================================================================

func (s *Server) handleWebAppSlotBasicAuthPolicy(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	parts := strings.Split(path, "/")

	subscriptionID := parts[2]
	resourceGroup := parts[4]
	appName := parts[8]
	slotName := parts[10]
	policyType := parts[12] // "ftp" or "scm"

	slotResourceID := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Web/sites/%s/slots/%s",
		subscriptionID, resourceGroup, appName, slotName)

	policyID := fmt.Sprintf("%s/basicPublishingCredentialsPolicies/%s", slotResourceID, policyType)

	switch r.Method {
	case http.MethodGet:
		// Return default policy (basic auth allowed)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":   policyID,
			"name": policyType,
			"type": "Microsoft.Web/sites/slots/basicPublishingCredentialsPolicies",
			"properties": map[string]interface{}{
				"allow": true,
			},
		})

	case http.MethodPut:
		var req struct {
			Properties struct {
				Allow bool `json:"allow"`
			} `json:"properties"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		response := map[string]interface{}{
			"id":   policyID,
			"name": policyType,
			"type": "Microsoft.Web/sites/slots/basicPublishingCredentialsPolicies",
			"properties": map[string]interface{}{
				"allow": req.Properties.Allow,
			},
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)

	default:
		s.methodNotAllowed(w)
	}
}

// =============================================================================
// Log Analytics Workspace Handler
// =============================================================================

func (s *Server) handleLogAnalytics(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	parts := strings.Split(path, "/")

	subscriptionID := parts[2]
	resourceGroup := parts[4]
	workspaceName := parts[8]

	resourceID := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.OperationalInsights/workspaces/%s",
		subscriptionID, resourceGroup, workspaceName)

	switch r.Method {
	case http.MethodPut:
		var req struct {
			Location   string            `json:"location"`
			Tags       map[string]string `json:"tags"`
			Properties struct {
				Sku struct {
					Name string `json:"name"`
				} `json:"sku"`
				RetentionInDays int `json:"retentionInDays"`
			} `json:"properties"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.badRequest(w, "Invalid request body")
			return
		}

		workspace := LogAnalyticsWorkspace{
			ID:       resourceID,
			Name:     workspaceName,
			Type:     "Microsoft.OperationalInsights/workspaces",
			Location: req.Location,
			Tags:     req.Tags,
			Properties: LogAnalyticsWorkspaceProps{
				ProvisioningState: "Succeeded",
				CustomerID:        fmt.Sprintf("customer-id-%s", workspaceName),
				Sku: struct {
					Name string `json:"name"`
				}{
					Name: req.Properties.Sku.Name,
				},
				RetentionInDays: req.Properties.RetentionInDays,
			},
		}

		s.store.mu.Lock()
		s.store.logAnalyticsWorkspaces[resourceID] = workspace
		s.store.mu.Unlock()

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(workspace)

	case http.MethodGet:
		s.store.mu.RLock()
		workspace, exists := s.store.logAnalyticsWorkspaces[resourceID]
		s.store.mu.RUnlock()

		if !exists {
			s.resourceNotFound(w, "Log Analytics Workspace", workspaceName)
			return
		}

		json.NewEncoder(w).Encode(workspace)

	case http.MethodDelete:
		s.store.mu.Lock()
		delete(s.store.logAnalyticsWorkspaces, resourceID)
		s.store.mu.Unlock()

		w.WriteHeader(http.StatusOK)

	default:
		s.methodNotAllowed(w)
	}
}

// =============================================================================
// Application Insights Handler
// =============================================================================

func (s *Server) handleAppInsights(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	parts := strings.Split(path, "/")

	subscriptionID := parts[2]
	resourceGroup := parts[4]
	insightsName := parts[8]

	resourceID := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Insights/components/%s",
		subscriptionID, resourceGroup, insightsName)

	switch r.Method {
	case http.MethodPut:
		var req struct {
			Location   string            `json:"location"`
			Tags       map[string]string `json:"tags"`
			Kind       string            `json:"kind"`
			Properties struct {
				ApplicationType     string `json:"Application_Type"`
				WorkspaceResourceID string `json:"WorkspaceResourceId"`
			} `json:"properties"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.badRequest(w, "Invalid request body")
			return
		}

		instrumentationKey := fmt.Sprintf("ikey-%s", insightsName)
		appID := fmt.Sprintf("appid-%s", insightsName)

		insights := ApplicationInsights{
			ID:       resourceID,
			Name:     insightsName,
			Type:     "Microsoft.Insights/components",
			Location: req.Location,
			Tags:     req.Tags,
			Kind:     req.Kind,
			Properties: ApplicationInsightsProps{
				ProvisioningState:   "Succeeded",
				ApplicationID:       appID,
				InstrumentationKey:  instrumentationKey,
				ConnectionString:    fmt.Sprintf("InstrumentationKey=%s;IngestionEndpoint=https://eastus-0.in.applicationinsights.azure.com/", instrumentationKey),
				WorkspaceResourceID: req.Properties.WorkspaceResourceID,
			},
		}

		s.store.mu.Lock()
		s.store.appInsights[resourceID] = insights
		s.store.mu.Unlock()

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(insights)

	case http.MethodGet:
		s.store.mu.RLock()
		insights, exists := s.store.appInsights[resourceID]
		s.store.mu.RUnlock()

		if !exists {
			s.resourceNotFound(w, "Application Insights", insightsName)
			return
		}

		json.NewEncoder(w).Encode(insights)

	case http.MethodDelete:
		s.store.mu.Lock()
		delete(s.store.appInsights, resourceID)
		s.store.mu.Unlock()

		w.WriteHeader(http.StatusOK)

	default:
		s.methodNotAllowed(w)
	}
}

// =============================================================================
// Autoscale Setting Handler
// =============================================================================

func (s *Server) handleAutoscaleSetting(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	parts := strings.Split(path, "/")

	subscriptionID := parts[2]
	resourceGroup := parts[4]
	settingName := parts[8]

	resourceID := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Insights/autoscalesettings/%s",
		subscriptionID, resourceGroup, settingName)

	switch r.Method {
	case http.MethodPut:
		var req struct {
			Location   string               `json:"location"`
			Tags       map[string]string    `json:"tags"`
			Properties AutoscaleSettingProps `json:"properties"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.badRequest(w, "Invalid request body")
			return
		}

		setting := AutoscaleSetting{
			ID:       resourceID,
			Name:     settingName,
			Type:     "Microsoft.Insights/autoscalesettings",
			Location: req.Location,
			Tags:     req.Tags,
			Properties: AutoscaleSettingProps{
				ProvisioningState:      "Succeeded",
				Enabled:                req.Properties.Enabled,
				TargetResourceURI:      req.Properties.TargetResourceURI,
				TargetResourceLocation: req.Location,
				Profiles:               req.Properties.Profiles,
				Notifications:          req.Properties.Notifications,
			},
		}

		s.store.mu.Lock()
		s.store.autoscaleSettings[resourceID] = setting
		s.store.mu.Unlock()

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(setting)

	case http.MethodGet:
		s.store.mu.RLock()
		setting, exists := s.store.autoscaleSettings[resourceID]
		s.store.mu.RUnlock()

		if !exists {
			s.resourceNotFound(w, "Autoscale Setting", settingName)
			return
		}

		json.NewEncoder(w).Encode(setting)

	case http.MethodDelete:
		s.store.mu.Lock()
		delete(s.store.autoscaleSettings, resourceID)
		s.store.mu.Unlock()

		w.WriteHeader(http.StatusOK)

	default:
		s.methodNotAllowed(w)
	}
}

// =============================================================================
// Action Group Handler
// =============================================================================

func (s *Server) handleActionGroup(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	parts := strings.Split(path, "/")

	subscriptionID := parts[2]
	resourceGroup := parts[4]
	groupName := parts[8]

	resourceID := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Insights/actionGroups/%s",
		subscriptionID, resourceGroup, groupName)

	switch r.Method {
	case http.MethodPut:
		var req struct {
			Location   string            `json:"location"`
			Tags       map[string]string `json:"tags"`
			Properties ActionGroupProps  `json:"properties"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.badRequest(w, "Invalid request body")
			return
		}

		group := ActionGroup{
			ID:       resourceID,
			Name:     groupName,
			Type:     "Microsoft.Insights/actionGroups",
			Location: "global",
			Tags:     req.Tags,
			Properties: ActionGroupProps{
				GroupShortName:   req.Properties.GroupShortName,
				Enabled:          req.Properties.Enabled,
				EmailReceivers:   req.Properties.EmailReceivers,
				WebhookReceivers: req.Properties.WebhookReceivers,
			},
		}

		s.store.mu.Lock()
		s.store.actionGroups[resourceID] = group
		s.store.mu.Unlock()

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(group)

	case http.MethodGet:
		s.store.mu.RLock()
		group, exists := s.store.actionGroups[resourceID]
		s.store.mu.RUnlock()

		if !exists {
			s.resourceNotFound(w, "Action Group", groupName)
			return
		}

		json.NewEncoder(w).Encode(group)

	case http.MethodDelete:
		s.store.mu.Lock()
		delete(s.store.actionGroups, resourceID)
		s.store.mu.Unlock()

		w.WriteHeader(http.StatusOK)

	default:
		s.methodNotAllowed(w)
	}
}

// =============================================================================
// Metric Alert Handler
// =============================================================================

func (s *Server) handleMetricAlert(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	parts := strings.Split(path, "/")

	subscriptionID := parts[2]
	resourceGroup := parts[4]
	alertName := parts[8]

	resourceID := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Insights/metricAlerts/%s",
		subscriptionID, resourceGroup, alertName)

	switch r.Method {
	case http.MethodPut:
		var req struct {
			Location   string            `json:"location"`
			Tags       map[string]string `json:"tags"`
			Properties MetricAlertProps  `json:"properties"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.badRequest(w, "Invalid request body")
			return
		}

		alert := MetricAlert{
			ID:       resourceID,
			Name:     alertName,
			Type:     "Microsoft.Insights/metricAlerts",
			Location: "global",
			Tags:     req.Tags,
			Properties: MetricAlertProps{
				Description:         req.Properties.Description,
				Severity:            req.Properties.Severity,
				Enabled:             req.Properties.Enabled,
				Scopes:              req.Properties.Scopes,
				EvaluationFrequency: req.Properties.EvaluationFrequency,
				WindowSize:          req.Properties.WindowSize,
				Criteria:            req.Properties.Criteria,
				Actions:             req.Properties.Actions,
			},
		}

		s.store.mu.Lock()
		s.store.metricAlerts[resourceID] = alert
		s.store.mu.Unlock()

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(alert)

	case http.MethodGet:
		s.store.mu.RLock()
		alert, exists := s.store.metricAlerts[resourceID]
		s.store.mu.RUnlock()

		if !exists {
			s.resourceNotFound(w, "Metric Alert", alertName)
			return
		}

		json.NewEncoder(w).Encode(alert)

	case http.MethodDelete:
		s.store.mu.Lock()
		delete(s.store.metricAlerts, resourceID)
		s.store.mu.Unlock()

		w.WriteHeader(http.StatusOK)

	default:
		s.methodNotAllowed(w)
	}
}

// =============================================================================
// Diagnostic Setting Handler
// =============================================================================

func (s *Server) handleDiagnosticSetting(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	// Diagnostic settings are nested under resources, extract name from end
	parts := strings.Split(path, "/")
	settingName := parts[len(parts)-1]

	// Use full path as resource ID
	resourceID := path

	switch r.Method {
	case http.MethodPut:
		var req struct {
			Properties DiagnosticSettingProps `json:"properties"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.badRequest(w, "Invalid request body")
			return
		}

		setting := DiagnosticSetting{
			ID:   resourceID,
			Name: settingName,
			Type: "Microsoft.Insights/diagnosticSettings",
			Properties: DiagnosticSettingProps{
				WorkspaceID: req.Properties.WorkspaceID,
				Logs:        req.Properties.Logs,
				Metrics:     req.Properties.Metrics,
			},
		}

		s.store.mu.Lock()
		s.store.diagnosticSettings[resourceID] = setting
		s.store.mu.Unlock()

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(setting)

	case http.MethodGet:
		s.store.mu.RLock()
		setting, exists := s.store.diagnosticSettings[resourceID]
		s.store.mu.RUnlock()

		if !exists {
			s.resourceNotFound(w, "Diagnostic Setting", settingName)
			return
		}

		json.NewEncoder(w).Encode(setting)

	case http.MethodDelete:
		s.store.mu.Lock()
		delete(s.store.diagnosticSettings, resourceID)
		s.store.mu.Unlock()

		w.WriteHeader(http.StatusOK)

	default:
		s.methodNotAllowed(w)
	}
}

// =============================================================================
// Error Responses
// =============================================================================

func (s *Server) notFound(w http.ResponseWriter, path string) {
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(AzureError{
		Error: AzureErrorDetail{
			Code:    "PathNotFound",
			Message: fmt.Sprintf("The path '%s' is not a valid Azure API path", path),
		},
	})
}

func (s *Server) resourceNotFound(w http.ResponseWriter, resourceType, name string) {
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(AzureError{
		Error: AzureErrorDetail{
			Code:    "ResourceNotFound",
			Message: fmt.Sprintf("The %s '%s' was not found.", resourceType, name),
		},
	})
}

func (s *Server) badRequest(w http.ResponseWriter, message string) {
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(AzureError{
		Error: AzureErrorDetail{
			Code:    "BadRequest",
			Message: message,
		},
	})
}

func (s *Server) methodNotAllowed(w http.ResponseWriter) {
	w.WriteHeader(http.StatusMethodNotAllowed)
	json.NewEncoder(w).Encode(AzureError{
		Error: AzureErrorDetail{
			Code:    "MethodNotAllowed",
			Message: "The HTTP method is not allowed for this resource",
		},
	})
}

// =============================================================================
// OAuth Token Handler (for Azure AD authentication)
// =============================================================================

type OAuthToken struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	ExpiresOn    int64  `json:"expires_on,omitempty"`
	NotBefore    int64  `json:"not_before,omitempty"`
	TokenType    string `json:"token_type"`
	Resource     string `json:"resource,omitempty"`
	Scope        string `json:"scope,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

func (s *Server) handleOpenIDConfiguration(w http.ResponseWriter, r *http.Request) {
	// Return OpenID Connect configuration document
	// This is required by MSAL for Azure CLI authentication
	host := r.Host
	if host == "" {
		host = "login.microsoftonline.com"
	}

	config := map[string]interface{}{
		"issuer":                                fmt.Sprintf("https://%s/mock-tenant-id/v2.0", host),
		"authorization_endpoint":               fmt.Sprintf("https://%s/mock-tenant-id/oauth2/v2.0/authorize", host),
		"token_endpoint":                        fmt.Sprintf("https://%s/mock-tenant-id/oauth2/v2.0/token", host),
		"device_authorization_endpoint":         fmt.Sprintf("https://%s/mock-tenant-id/oauth2/v2.0/devicecode", host),
		"userinfo_endpoint":                     fmt.Sprintf("https://%s/oidc/userinfo", host),
		"end_session_endpoint":                  fmt.Sprintf("https://%s/mock-tenant-id/oauth2/v2.0/logout", host),
		"jwks_uri":                              fmt.Sprintf("https://%s/mock-tenant-id/discovery/v2.0/keys", host),
		"response_types_supported":              []string{"code", "id_token", "code id_token", "token id_token", "token"},
		"response_modes_supported":              []string{"query", "fragment", "form_post"},
		"subject_types_supported":               []string{"pairwise"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"scopes_supported":                      []string{"openid", "profile", "email", "offline_access"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_post", "client_secret_basic"},
		"claims_supported":                      []string{"sub", "iss", "aud", "exp", "iat", "name", "email"},
		"tenant_region_scope":                   "NA",
		"cloud_instance_name":                   "microsoftonline.com",
		"cloud_graph_host_name":                 "graph.windows.net",
		"msgraph_host":                          "graph.microsoft.com",
	}

	json.NewEncoder(w).Encode(config)
}

func (s *Server) handleInstanceDiscovery(w http.ResponseWriter, r *http.Request) {
	// Return instance discovery response for MSAL
	response := map[string]interface{}{
		"tenant_discovery_endpoint": "https://login.microsoftonline.com/mock-tenant-id/v2.0/.well-known/openid-configuration",
		"api-version":               "1.1",
		"metadata": []map[string]interface{}{
			{
				"preferred_network":   "login.microsoftonline.com",
				"preferred_cache":     "login.windows.net",
				"aliases":             []string{"login.microsoftonline.com", "login.windows.net", "login.microsoft.com"},
			},
		},
	}

	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleOAuth(w http.ResponseWriter, r *http.Request) {
	// Return a mock OAuth token that looks like a valid JWT
	// JWT format: header.payload.signature (all base64url encoded)
	// The Azure SDK parses claims from the token, so it must be valid JWT format

	now := time.Now().Unix()
	exp := now + 3600

	// JWT Header (typ: JWT, alg: RS256)
	header := "eyJ0eXAiOiJKV1QiLCJhbGciOiJSUzI1NiJ9"

	// JWT Payload with required Azure claims
	// Decoded: {"aud":"https://management.azure.com/","iss":"https://sts.windows.net/mock-tenant-id/","iat":NOW,"nbf":NOW,"exp":EXP,"oid":"mock-object-id","sub":"mock-subject","tid":"mock-tenant-id"}
	payloadJSON := fmt.Sprintf(`{"aud":"https://management.azure.com/","iss":"https://sts.windows.net/mock-tenant-id/","iat":%d,"nbf":%d,"exp":%d,"oid":"mock-object-id","sub":"mock-subject","tid":"mock-tenant-id"}`, now, now, exp)
	payload := base64.RawURLEncoding.EncodeToString([]byte(payloadJSON))

	// Mock signature (doesn't need to be valid, just present)
	signature := "mock-signature-placeholder"

	mockJWT := header + "." + payload + "." + signature

	token := OAuthToken{
		AccessToken:  mockJWT,
		ExpiresIn:    3600,
		ExpiresOn:    exp,
		NotBefore:    now,
		TokenType:    "Bearer",
		Resource:     "https://management.azure.com/",
		Scope:        "https://management.azure.com/.default",
		RefreshToken: "mock-refresh-token",
	}
	json.NewEncoder(w).Encode(token)
}

// =============================================================================
// Provider Registration Handler
// =============================================================================

func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	// Return a list of registered providers that the azurerm provider needs
	providers := []map[string]interface{}{
		{"namespace": "Microsoft.Cdn", "registrationState": "Registered"},
		{"namespace": "Microsoft.Network", "registrationState": "Registered"},
		{"namespace": "Microsoft.Storage", "registrationState": "Registered"},
		{"namespace": "Microsoft.Resources", "registrationState": "Registered"},
		{"namespace": "Microsoft.Authorization", "registrationState": "Registered"},
		{"namespace": "Microsoft.Web", "registrationState": "Registered"},
		{"namespace": "Microsoft.Insights", "registrationState": "Registered"},
		{"namespace": "Microsoft.OperationalInsights", "registrationState": "Registered"},
	}
	response := map[string]interface{}{
		"value": providers,
	}
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleProviderRegistration(w http.ResponseWriter, r *http.Request) {
	// Return success for provider registration checks
	response := map[string]interface{}{
		"registrationState": "Registered",
	}
	json.NewEncoder(w).Encode(response)
}

// =============================================================================
// Subscription Handler
// =============================================================================

func (s *Server) handleSubscription(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	parts := strings.Split(path, "/")
	subscriptionID := parts[2]

	subscription := map[string]interface{}{
		"id":             fmt.Sprintf("/subscriptions/%s", subscriptionID),
		"subscriptionId": subscriptionID,
		"displayName":    "Mock Subscription",
		"state":          "Enabled",
	}
	json.NewEncoder(w).Encode(subscription)
}

// =============================================================================
// Main
// =============================================================================

func main() {
	server := NewServer()

	log.Println("Azure Mock API Server")
	log.Println("=====================")
	log.Println("ARM Endpoints:")
	log.Println("  OAuth Token:      /{tenant}/oauth2/token (POST)")
	log.Println("  Subscriptions:    /subscriptions/{sub}")
	log.Println("  CDN Profiles:     .../Microsoft.Cdn/profiles/{name}")
	log.Println("  CDN Endpoints:    .../Microsoft.Cdn/profiles/{profile}/endpoints/{name}")
	log.Println("  DNS Zones:        .../Microsoft.Network/dnszones/{name}")
	log.Println("  DNS CNAME:        .../Microsoft.Network/dnszones/{zone}/CNAME/{name}")
	log.Println("  Storage Accounts: .../Microsoft.Storage/storageAccounts/{name}")
	log.Println("")
	log.Println("App Service Endpoints:")
	log.Println("  Service Plans:    .../Microsoft.Web/serverfarms/{name}")
	log.Println("  Web Apps:         .../Microsoft.Web/sites/{name}")
	log.Println("  Web App Slots:    .../Microsoft.Web/sites/{app}/slots/{slot}")
	log.Println("  Web App Config:   .../Microsoft.Web/sites/{app}/config/web")
	log.Println("")
	log.Println("Monitoring Endpoints:")
	log.Println("  Log Analytics:    .../Microsoft.OperationalInsights/workspaces/{name}")
	log.Println("  App Insights:     .../Microsoft.Insights/components/{name}")
	log.Println("  Autoscale:        .../Microsoft.Insights/autoscalesettings/{name}")
	log.Println("  Action Groups:    .../Microsoft.Insights/actionGroups/{name}")
	log.Println("  Metric Alerts:    .../Microsoft.Insights/metricAlerts/{name}")
	log.Println("")
	log.Println("Blob Storage Endpoints (Host: {account}.blob.core.windows.net):")
	log.Println("  Containers:       /{container}?restype=container")
	log.Println("  Blobs:            /{container}/{blob}")
	log.Println("")
	log.Println("Starting server on :8080...")

	if err := http.ListenAndServe(":8080", server); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}