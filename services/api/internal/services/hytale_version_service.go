package services

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// HytaleMavenBaseURL is the official Maven repository that publishes the
// Hytale server build per patchline (release and pre-release).
const HytaleMavenBaseURL = "https://maven.hytale.com"

const hytaleVersionsCacheKey = "hytale:versions"
const hytaleVersionsCacheTTL = 6 * time.Hour

// hytalePatchlines are the values the official downloader accepts for
// -patchline, in display order.
var hytalePatchlines = []string{"release", "pre-release"}

// NormalizeHytaleVersion validates a server's hytale_version.  The value is
// handed to the downloader as -patchline, so only patchline names (or "latest",
// meaning release) are valid; a pinned build number would fail provisioning.
func NormalizeHytaleVersion(v string) (string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" || v == "latest" {
		return "latest", nil
	}
	if slices.Contains(hytalePatchlines, v) {
		return v, nil
	}
	return "", fmt.Errorf("hytale_version must be one of latest, release, pre-release")
}

type HytalePatchline struct {
	Patchline string `json:"patchline"`
	Latest    string `json:"latest"`               // empty when Maven could not be reached
	UpdatedAt string `json:"updated_at,omitempty"` // RFC 3339, from Maven lastUpdated
}

type HytaleVersions struct {
	Patchlines []HytalePatchline `json:"patchlines"`
	CheckedAt  time.Time         `json:"checked_at"`
}

// HytaleVersionService reports the newest Hytale server build per patchline.
type HytaleVersionService struct {
	rdb     *redis.Client // optional; nil disables caching
	baseURL string
	client  *http.Client
	log     *zap.Logger
}

func NewHytaleVersionService(rdb *redis.Client, baseURL string, log *zap.Logger) *HytaleVersionService {
	return &HytaleVersionService{
		rdb:     rdb,
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 10 * time.Second},
		log:     log,
	}
}

// Versions never fails: a patchline whose metadata cannot be fetched is listed
// with an empty Latest so callers can still offer it.
func (s *HytaleVersionService) Versions(ctx context.Context) *HytaleVersions {
	if s.rdb != nil {
		if cached, err := s.rdb.Get(ctx, hytaleVersionsCacheKey).Result(); err == nil {
			var v HytaleVersions
			if json.Unmarshal([]byte(cached), &v) == nil {
				return &v
			}
		}
	}

	out := &HytaleVersions{CheckedAt: time.Now().UTC()}
	complete := true
	for _, p := range hytalePatchlines {
		entry := HytalePatchline{Patchline: p}
		latest, updated, err := s.fetchLatest(ctx, p)
		if err != nil {
			complete = false
			s.log.Warn("hytale version check failed", zap.String("patchline", p), zap.Error(err))
		} else {
			entry.Latest, entry.UpdatedAt = latest, updated
		}
		out.Patchlines = append(out.Patchlines, entry)
	}

	// Only cache a full answer, so a transient Maven outage is retried on the
	// next request instead of being pinned for hours.
	if s.rdb != nil && complete {
		if data, err := json.Marshal(out); err == nil {
			_ = s.rdb.Set(ctx, hytaleVersionsCacheKey, data, hytaleVersionsCacheTTL).Err()
		}
	}
	return out
}

type mavenMetadata struct {
	Versioning struct {
		Latest      string `xml:"latest"`
		Release     string `xml:"release"`
		LastUpdated string `xml:"lastUpdated"`
	} `xml:"versioning"`
}

func (s *HytaleVersionService) fetchLatest(ctx context.Context, patchline string) (latest, updatedAt string, err error) {
	url := fmt.Sprintf("%s/%s/com/hypixel/hytale/Server/maven-metadata.xml", s.baseURL, patchline)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("maven returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", "", err
	}
	var meta mavenMetadata
	if err := xml.Unmarshal(body, &meta); err != nil {
		return "", "", fmt.Errorf("parsing maven metadata: %w", err)
	}

	latest = meta.Versioning.Latest
	if latest == "" {
		latest = meta.Versioning.Release
	}
	if latest == "" {
		return "", "", fmt.Errorf("maven metadata has no latest version")
	}
	if t, err := time.Parse("20060102150405", meta.Versioning.LastUpdated); err == nil {
		updatedAt = t.UTC().Format(time.RFC3339)
	}
	return latest, updatedAt, nil
}
