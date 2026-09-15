// Package model defines the normalized domain model shared by the ingestion pipeline,
// the public API and the admin console.
//
// Design rule (architecture v0.4 §176): stable opaque IDs are the contract between
// platforms; display labels are localization data and never primary identifiers.
package model

import (
	"strings"
	"time"
)

// SourceType distinguishes the origin of a feed. Aggregator feeds are useful for
// discovery but must never silently look like a direct publisher (§32, §72).
type SourceType string

const (
	SourceValidatedDirect     SourceType = "VALIDATED_DIRECT"
	SourceDirectPublisher     SourceType = "DIRECT_PUBLISHER"
	SourceOfficialRealtime    SourceType = "OFFICIAL_REALTIME"
	SourceOfficialInstitution SourceType = "OFFICIAL_INSTITUTION"
	SourceAggregatorTopic     SourceType = "AGGREGATOR_TOPIC"
	SourceAggregatorSearch    SourceType = "AGGREGATOR_SEARCH"
	SourceOpportunityFeed     SourceType = "OPPORTUNITY_FEED"
	SourceTenderFeed          SourceType = "TENDER_FEED"
	SourceUnknown             SourceType = "UNKNOWN"
)

// TrustWeight maps a source type onto the 1..5 priority scale used for ranking (§141).
func (s SourceType) TrustWeight() int {
	switch s {
	case SourceOfficialRealtime, SourceOfficialInstitution:
		return 5
	case SourceValidatedDirect:
		return 5
	case SourceDirectPublisher:
		return 4
	case SourceOpportunityFeed, SourceTenderFeed:
		return 3
	case SourceAggregatorTopic:
		return 3
	case SourceAggregatorSearch:
		return 2
	default:
		return 1
	}
}

// IsAggregator reports whether the source is a discovery layer rather than a publisher.
func (s SourceType) IsAggregator() bool {
	return s == SourceAggregatorTopic || s == SourceAggregatorSearch
}

// Label returns the transparency badge shown in the client (§15).
func (s SourceType) Label() string {
	switch s {
	case SourceDirectPublisher, SourceValidatedDirect:
		return "Direct RSS"
	case SourceOfficialRealtime, SourceOfficialInstitution:
		return "Official institution"
	case SourceAggregatorTopic, SourceAggregatorSearch:
		return "Aggregated discovery"
	default:
		return "Unclassified"
	}
}

// ParseSourceType converts an OPML sourceType attribute into the internal enum (§264).
func ParseSourceType(raw string) SourceType {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "validated-direct":
		return SourceValidatedDirect
	case "direct":
		return SourceDirectPublisher
	case "official-realtime":
		return SourceOfficialRealtime
	case "official":
		return SourceOfficialInstitution
	case "aggregator-topic":
		return SourceAggregatorTopic
	case "aggregator-search":
		return SourceAggregatorSearch
	case "opportunity":
		return SourceOpportunityFeed
	case "tender":
		return SourceTenderFeed
	default:
		return SourceUnknown
	}
}

// PollTier is the scheduler class of a feed (§148, §269).
type PollTier string

const (
	TierBreaking    PollTier = "BREAKING"
	TierHigh        PollTier = "HIGH"
	TierNormal      PollTier = "NORMAL"
	TierSlow        PollTier = "SLOW"
	TierOpportunity PollTier = "OPPORTUNITY"
)

// Interval returns the target polling interval for the tier. These are operational
// targets, not guarantees; adaptive polling and publisher limits may stretch them (§148).
func (t PollTier) Interval() time.Duration {
	switch t {
	case TierBreaking:
		return 3 * time.Minute
	case TierHigh:
		return 5 * time.Minute
	case TierNormal:
		return 12 * time.Minute
	case TierSlow:
		return 30 * time.Minute
	case TierOpportunity:
		return 60 * time.Minute
	default:
		return 15 * time.Minute
	}
}

// HealthStatus is the operational state of a feed (§33, §270).
type HealthStatus string

const (
	HealthUnknown     HealthStatus = "UNKNOWN"
	HealthHealthy     HealthStatus = "HEALTHY"
	HealthDegraded    HealthStatus = "DEGRADED"
	HealthUnstable    HealthStatus = "UNSTABLE"
	HealthStale       HealthStatus = "STALE"
	HealthEmpty       HealthStatus = "EMPTY"
	HealthParserError HealthStatus = "PARSER_ERROR"
	HealthHTTPError   HealthStatus = "HTTP_ERROR"
	HealthRateLimited HealthStatus = "RATE_LIMITED"
	HealthQuarantined HealthStatus = "QUARANTINED"
	HealthDisabled    HealthStatus = "DISABLED"
)

// ArticleStatus controls visibility of an ingested article.
type ArticleStatus string

const (
	ArticleActive   ArticleStatus = "ACTIVE"
	ArticleHidden   ArticleStatus = "HIDDEN"
	ArticleRejected ArticleStatus = "REJECTED"
)

// CategoryAssignment is one topic assignment produced by the classifier with its
// confidence and origin (§37, §163).
type CategoryAssignment struct {
	ID         string               `json:"id"`
	Confidence float64              `json:"confidence"`
	Origin     ClassificationOrigin `json:"origin"`
}

// ProvinceAssignment is one province assignment produced by the classifier.
type ProvinceAssignment struct {
	ID         string               `json:"id"`
	Confidence float64              `json:"confidence"`
	Origin     ClassificationOrigin `json:"origin"`
}

// ClassificationOrigin records how a category/province assignment was produced (§37, §163).
type ClassificationOrigin string

const (
	OriginFeedCategory ClassificationOrigin = "FEED_CATEGORY"
	OriginRule         ClassificationOrigin = "RULE"
	OriginEditor       ClassificationOrigin = "EDITOR"
	OriginModel        ClassificationOrigin = "MODEL"
)

// Source is a publisher or discovery provider. One source may own several feeds (§117).
type Source struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	WebsiteURL      string     `json:"websiteUrl,omitempty"`
	SourceType      SourceType `json:"type"`
	DefaultLanguage string     `json:"language,omitempty"`
	TrustWeight     int        `json:"trustWeight"`
	Enabled         bool       `json:"enabled"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

// Feed is a single RSS/Atom/RDF endpoint tracked by the ingestion pipeline (§25.1, §118).
type Feed struct {
	ID                  string       `json:"id"`
	SourceID            string       `json:"sourceId"`
	SourceName          string       `json:"sourceName,omitempty"`
	XMLURL              string       `json:"xmlUrl"`
	NormalizedXMLURL    string       `json:"normalizedXmlUrl"`
	HTMLURL             string       `json:"htmlUrl,omitempty"`
	Title               string       `json:"title"`
	Language            string       `json:"language,omitempty"`
	Scope               string       `json:"scope,omitempty"`
	CategoryKey         string       `json:"categoryKey,omitempty"`
	SourceType          SourceType   `json:"sourceType"`
	Priority            int          `json:"priority"`
	PollTier            PollTier     `json:"pollTier"`
	Enabled             bool         `json:"enabled"`
	HealthStatus        HealthStatus `json:"healthStatus"`
	ETag                string       `json:"etag,omitempty"`
	LastModified        string       `json:"lastModified,omitempty"`
	LastCheckedAt       *time.Time   `json:"lastCheckedAt,omitempty"`
	LastSuccessAt       *time.Time   `json:"lastSuccessAt,omitempty"`
	NewestItemAt        *time.Time   `json:"newestItemAt,omitempty"`
	NextPollAt          *time.Time   `json:"nextPollAt,omitempty"`
	ConsecutiveFailures int          `json:"consecutiveFailures"`
	HealthScore         int          `json:"healthScore"`
	FeedPackVersion     string       `json:"feedPackVersion,omitempty"`
	NeedsReview         bool         `json:"needsReview"`
	LeaseOwner          string       `json:"-"`
	LeaseUntil          *time.Time   `json:"-"`
	CreatedAt           time.Time    `json:"createdAt"`
	UpdatedAt           time.Time    `json:"updatedAt"`
}

// Article is the normalized article record stored in PostgreSQL (§119).
type Article struct {
	ID              string        `json:"id"`
	SourceID        string        `json:"sourceId"`
	FeedID          string        `json:"feedId,omitempty"`
	ExternalGUID    string        `json:"externalGuid,omitempty"`
	CanonicalURL    string        `json:"canonicalUrl"`
	OriginalURL     string        `json:"originalUrl"`
	NormalizedURL   string        `json:"normalizedUrl"`
	Title           string        `json:"title"`
	NormalizedTitle string        `json:"normalizedTitle"`
	Summary         string        `json:"summary,omitempty"`
	FeedContent     string        `json:"feedContent,omitempty"`
	ImageURL        string        `json:"imageUrl,omitempty"`
	Author          string        `json:"author,omitempty"`
	PublishedAt     *time.Time    `json:"publishedAt,omitempty"`
	UpdatedAt       *time.Time    `json:"updatedAt,omitempty"`
	DiscoveredAt    time.Time     `json:"discoveredAt"`
	Language        string        `json:"language"`
	IsBreaking      bool          `json:"isBreaking"`
	ClusterID       string        `json:"clusterId,omitempty"`
	ContentHash     string        `json:"contentHash,omitempty"`
	Status          ArticleStatus `json:"status"`
	CreatedAt       time.Time     `json:"createdAt"`
}

// Candidate is a normalized but not yet persisted article produced by the parser (§156).
type Candidate struct {
	ExternalGUID  string
	Title         string
	Link          string
	Summary       string
	Content       string
	ImageURL      string
	Author        string
	Language      string
	Categories    []string
	PublishedAt   *time.Time
	UpdatedAt     *time.Time
	RawCategories []string
}

// ArticleCard is the lean list DTO sent to mobile clients (§353).
type ArticleCard struct {
	ID           string       `json:"id"`
	Title        string       `json:"title"`
	Summary      string       `json:"summary,omitempty"`
	ImageURL     string       `json:"imageUrl,omitempty"`
	OriginalURL  string       `json:"originalUrl"`
	PublishedAt  *time.Time   `json:"publishedAt,omitempty"`
	DiscoveredAt time.Time    `json:"discoveredAt"`
	Language     string       `json:"language"`
	IsBreaking   bool         `json:"isBreaking"`
	Source       SourceRef    `json:"source"`
	Category     *CategoryRef `json:"category,omitempty"`
	Province     *ProvinceRef `json:"province,omitempty"`
	Cluster      *ClusterRef  `json:"cluster,omitempty"`
	Opportunity  *Opportunity `json:"opportunity,omitempty"`
	SourceType   SourceType   `json:"sourceType"`
	// Status is the moderation state. It is only meaningful on the admin surface
	// (the public API never returns hidden articles), but it is always populated
	// so the console can render the hide/publish toggle without a second request.
	Status ArticleStatus `json:"status,omitempty"`
}

// SourceRef is the compact source representation embedded in article responses.
type SourceRef struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	Type    SourceType `json:"type"`
	Label   string     `json:"transparencyLabel,omitempty"`
	LogoURL string     `json:"logoUrl,omitempty"`
}

// CategoryRef is the compact category representation with localized display names.
type CategoryRef struct {
	ID           string            `json:"id"`
	DisplayNames map[string]string `json:"displayNames"`
}

// ProvinceRef is the compact province representation.
type ProvinceRef struct {
	ID           string            `json:"id"`
	DisplayNames map[string]string `json:"displayNames"`
}

// ClusterRef summarizes multi-source coverage of one story (§160, §298).
type ClusterRef struct {
	ID            string `json:"id"`
	CoverageCount int    `json:"coverageCount"`
}

// Opportunity carries structured jobs/tender fields when they can be extracted (§54, §299).
type Opportunity struct {
	Organization    string     `json:"organization,omitempty"`
	Location        string     `json:"location,omitempty"`
	Deadline        *time.Time `json:"deadline,omitempty"`
	EmploymentType  string     `json:"employmentType,omitempty"`
	OpportunityType string     `json:"opportunityType,omitempty"`
	ReferenceNumber string     `json:"referenceNumber,omitempty"`
	Expired         bool       `json:"expired"`
}

// Category is a topic definition with localization.
type Category struct {
	ID           string            `json:"id"`
	DisplayNames map[string]string `json:"displayNames"`
	Enabled      bool              `json:"enabled"`
	SortOrder    int               `json:"sortOrder"`
}

// Province is one of the 34 Afghan provinces.
type Province struct {
	ID           string            `json:"id"`
	DisplayNames map[string]string `json:"displayNames"`
	SortOrder    int               `json:"sortOrder"`
	StoryCount   int               `json:"recentStoryCount,omitempty"`
	NewestAt     *time.Time        `json:"newestAt,omitempty"`
}

// FeedHealthEvent is one ingestion attempt record (§249).
type FeedHealthEvent struct {
	ID         int64     `json:"id"`
	FeedID     string    `json:"feedId"`
	CheckedAt  time.Time `json:"checkedAt"`
	HTTPStatus int       `json:"httpStatus,omitempty"`
	ParseOK    bool      `json:"parseOk"`
	ItemCount  int       `json:"itemCount"`
	DurationMS int       `json:"durationMs"`
	EventType  string    `json:"eventType"`
	ErrorCode  string    `json:"errorCode,omitempty"`
	Message    string    `json:"message,omitempty"`
}

// HomePayload is the aggregated home response (§120, §228).
type HomePayload struct {
	GeneratedAt          time.Time     `json:"generatedAt"`
	Breaking             []ArticleCard `json:"breaking"`
	TopStories           []ArticleCard `json:"topStories"`
	LatestAfghanistan    []ArticleCard `json:"latestAfghanistan"`
	FollowedProvince     []ArticleCard `json:"followedProvincePreview"`
	Economy              []ArticleCard `json:"economy"`
	Jobs                 []ArticleCard `json:"jobs"`
	World                []ArticleCard `json:"world"`
	PersonalizedSections []HomeSection `json:"personalizedSections"`
}

// HomeSection is a client-requested topic section (technology, sports, ...).
type HomeSection struct {
	Key      string        `json:"key"`
	Title    string        `json:"title"`
	Articles []ArticleCard `json:"articles"`
}

// Page is the cursor-paginated list envelope (§229).
type Page struct {
	Items      []ArticleCard `json:"items"`
	NextCursor string        `json:"nextCursor"`
}

// ArticleQuery is the filter set accepted by /v1/articles and /v1/search (§230, §231).
type ArticleQuery struct {
	// Digest marks a curated-digest query (the home payload). Digest queries drop items whose
	// whole category set is non-editorial — instrument readings and reference tables belong to
	// their topical lists, not to the front page.
	Digest   bool
	Scope    string
	Category string
	Province string
	Language string
	Source   string
	Cluster  string
	Feed     string
	Breaking *bool
	From     *time.Time
	To       *time.Time
	Query    string
	Cursor   string
	Limit    int
	// Offset enables page-number paging. It is only used by the admin console tables
	// (moderation work needs "page 4 of 21"); the public API stays cursor-only.
	Offset        int
	Sort          string
	Status        ArticleStatus
	IncludeHidden bool
}

// FeedPackImport summarizes an OPML import attempt (§266).
type FeedPackImport struct {
	Version       string         `json:"version"`
	SHA256        string         `json:"sha256"`
	Total         int            `json:"total"`
	New           int            `json:"new"`
	Changed       int            `json:"changed"`
	Unchanged     int            `json:"unchanged"`
	Missing       int            `json:"missingFromPack"`
	Invalid       int            `json:"invalid"`
	Duplicates    int            `json:"duplicates"`
	Inserted      int            `json:"insertedCount"`
	Updated       int            `json:"updatedCount"`
	Disabled      int            `json:"disabledCount"`
	Committed     bool           `json:"committed"`
	ImportedBy    string         `json:"importedBy,omitempty"`
	ImportedAt    time.Time      `json:"importedAt"`
	ValidationErr []string       `json:"validationErrors,omitempty"`
	SampleChanges []ImportChange `json:"sampleChanges,omitempty"`
}

// ImportChange describes one outline-level difference found during an OPML dry run.
type ImportChange struct {
	XMLURL string `json:"xmlUrl"`
	Title  string `json:"title"`
	Kind   string `json:"kind"` // new | changed | missing | invalid | unchanged | duplicate
	Detail string `json:"detail,omitempty"`
}

// PushRegistration is a device registered to receive notifications (§236).
type PushRegistration struct {
	ID         string     `json:"id"`
	Token      string     `json:"token"`
	Platform   string     `json:"platform"`
	AppVersion string     `json:"appVersion,omitempty"`
	Language   string     `json:"language,omitempty"`
	Topics     []string   `json:"topics"`
	Active     bool       `json:"active"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	LastSeenAt *time.Time `json:"lastSeenAt,omitempty"`
}

// PushEvent records a sent notification for deduplication and auditing (§254, §165).
type PushEvent struct {
	ID        string     `json:"id"`
	ArticleID string     `json:"articleId,omitempty"`
	Topic     string     `json:"topic"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Status    string     `json:"status"`
	DedupKey  string     `json:"dedupKey"`
	SentAt    *time.Time `json:"sentAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	Actor     string     `json:"actor,omitempty"`
	Audience  int        `json:"audience,omitempty"`
}

// AdminUser is an operator account (§131).
type AdminUser struct {
	ID           string     `json:"id"`
	Username     string     `json:"username"`
	PasswordHash string     `json:"-"`
	Role         string     `json:"role"`
	Active       bool       `json:"active"`
	CreatedAt    time.Time  `json:"createdAt"`
	LastLoginAt  *time.Time `json:"lastLoginAt,omitempty"`
	MFAEnabled   bool       `json:"mfaEnabled"`
}

// AuditEntry is an immutable operator action record (§143).
type AuditEntry struct {
	ID        int64     `json:"id"`
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	Entity    string    `json:"entity"`
	EntityID  string    `json:"entityId"`
	Before    string    `json:"before,omitempty"`
	After     string    `json:"after,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// Roles used by the admin console.
const (
	RoleSuperAdmin = "SUPER_ADMIN"
	RoleEditor     = "EDITOR"
	RoleSourceMgr  = "SOURCE_MANAGER"
	RoleViewer     = "VIEWER"
)
