package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BitaceS/talepanel/api/internal/version"
)

// Modtale (modtale.net) is a community mod repository for Hytale. Its public
// API needs no key; anonymous callers get 60 requests per minute per IP, so
// the panel caches what it reads. An optional key (MODTALE_API_KEY) raises
// the limit.
const modtaleAPIBase = "https://api.modtale.net/api/v1"

var (
	// ErrModtaleNotFound: project or version does not exist (or is not public).
	ErrModtaleNotFound = errors.New("modtale project or version not found")
	// ErrModtaleNotInstallable: the project is a world, modpack or other type
	// that does not belong in a server's mods/ directory.
	ErrModtaleNotInstallable = errors.New("only Modtale plugins, data packs and art packs can be installed")
	// ErrModtaleInvalidID: not a Modtale project ID.
	ErrModtaleInvalidID = errors.New("invalid Modtale project ID")
)

// ModtaleError carries the upstream HTTP status.
type ModtaleError struct {
	Status int
}

func (e *ModtaleError) Error() string {
	if e.Status == http.StatusTooManyRequests {
		return "Modtale rate limit reached, try again in a minute"
	}
	return fmt.Sprintf("Modtale returned HTTP %d", e.Status)
}

// modtaleInstallable are the classifications whose files go into mods/:
// plugins (.jar) and data/art packs (.zip). Worlds (SAVE) and modpacks are not.
var modtaleInstallable = map[string]bool{"PLUGIN": true, "DATA": true, "ART": true}

// modtaleSorts are the sort orders the panel passes through.
var modtaleSorts = map[string]bool{"relevance": true, "downloads": true, "updated": true, "new": true, "popular": true, "trending": true}

var modtaleIDRe = regexp.MustCompile(`^[0-9a-fA-F-]{8,64}$`)

// ModtaleProject is one search hit, as the panel passes it to the browser.
type ModtaleProject struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Description    string `json:"description"`
	Author         string `json:"author"`
	ImageURL       string `json:"image_url"`
	Classification string `json:"classification"`
	Downloads      int64  `json:"downloads"`
	UpdatedAt      string `json:"updated_at"`
	PageURL        string `json:"page_url"`
}

// ModtaleSearchResult is a page of search hits.
type ModtaleSearchResult struct {
	Projects []ModtaleProject `json:"projects"`
	Page     int              `json:"page"`
	Total    int              `json:"total"`
}

// ModtaleVersion is one downloadable version of a project, newest first.
type ModtaleVersion struct {
	ID           string   `json:"id"`
	Version      string   `json:"version"`
	FileName     string   `json:"file_name"`
	Channel      string   `json:"channel"`
	GameVersions []string `json:"game_versions"`
	ReleasedAt   string   `json:"released_at"`
	Downloads    int64    `json:"downloads"`
}

// ModtaleDownload is what an install needs: the file name on disk and a
// freshly signed link (valid for a few minutes).
type ModtaleDownload struct {
	ProjectID string
	Title     string
	Version   string
	FileName  string
	URL       string
}

type modtaleCacheEntry struct {
	expires time.Time
	value   any
}

// ModtaleService wraps the Modtale REST API with a small in-memory cache.
type ModtaleService struct {
	base   string
	apiKey string
	client *http.Client

	mu    sync.Mutex
	cache map[string]modtaleCacheEntry
}

func NewModtaleService(apiKey string) *ModtaleService {
	return &ModtaleService{
		base:   modtaleAPIBase,
		apiKey: apiKey,
		client: &http.Client{Timeout: 15 * time.Second},
		cache:  map[string]modtaleCacheEntry{},
	}
}

const (
	modtaleSearchTTL   = 2 * time.Minute
	modtaleProjectTTL  = 5 * time.Minute
	modtaleCacheMaxLen = 500
)

func (s *ModtaleService) cached(key string) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.cache[key]
	if !ok || time.Now().After(e.expires) {
		return nil, false
	}
	return e.value, true
}

func (s *ModtaleService) store(key string, v any, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cache) >= modtaleCacheMaxLen {
		now := time.Now()
		for k, e := range s.cache {
			if now.After(e.expires) {
				delete(s.cache, k)
			}
		}
		if len(s.cache) >= modtaleCacheMaxLen {
			s.cache = map[string]modtaleCacheEntry{}
		}
	}
	s.cache[key] = modtaleCacheEntry{expires: time.Now().Add(ttl), value: v}
}

func (s *ModtaleService) get(ctx context.Context, pathAndQuery string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+pathAndQuery, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "TalePanel/"+version.Version+" (+https://talepanel.com)")
	req.Header.Set("X-Modtale-Client", "TalePanel")
	if s.apiKey != "" {
		req.Header.Set("X-MODTALE-KEY", s.apiKey)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("modtale request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrModtaleNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return &ModtaleError{Status: resp.StatusCode}
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out); err != nil {
		return fmt.Errorf("decoding modtale response: %w", err)
	}
	return nil
}

func modtalePageURL(id string) string {
	return "https://modtale.net/mod/" + url.PathEscape(id)
}

// Search lists installable projects. classification is PLUGIN, DATA or ART
// (default PLUGIN); sort is one of modtaleSorts (default relevance with a
// query, downloads without one).
func (s *ModtaleService) Search(ctx context.Context, query, classification, sort string, page, size int) (*ModtaleSearchResult, error) {
	classification = strings.ToUpper(classification)
	if !modtaleInstallable[classification] {
		classification = "PLUGIN"
	}
	if !modtaleSorts[sort] {
		sort = "relevance"
		if strings.TrimSpace(query) == "" {
			sort = "downloads"
		}
	}
	if page < 0 {
		page = 0
	}
	if size < 1 || size > 50 {
		size = 20
	}
	q := url.Values{}
	if query = strings.TrimSpace(query); query != "" {
		if len(query) > 100 {
			query = query[:100]
		}
		q.Set("search", query)
	}
	q.Set("classification", classification)
	q.Set("sort", sort)
	q.Set("page", strconv.Itoa(page))
	q.Set("size", strconv.Itoa(size))
	key := "search?" + q.Encode()
	if v, ok := s.cached(key); ok {
		return v.(*ModtaleSearchResult), nil
	}

	var raw struct {
		Content []struct {
			ID             string `json:"id"`
			Title          string `json:"title"`
			Description    string `json:"description"`
			Author         string `json:"author"`
			ImageURL       string `json:"imageUrl"`
			Classification string `json:"classification"`
			DownloadCount  int64  `json:"downloadCount"`
			UpdatedAt      string `json:"updatedAt"`
		} `json:"content"`
		TotalElements int `json:"totalElements"`
		Number        int `json:"number"`
	}
	if err := s.get(ctx, "/projects?"+q.Encode(), &raw); err != nil {
		return nil, err
	}
	res := &ModtaleSearchResult{Projects: []ModtaleProject{}, Page: raw.Number, Total: raw.TotalElements}
	for _, p := range raw.Content {
		if !modtaleIDRe.MatchString(p.ID) {
			continue
		}
		res.Projects = append(res.Projects, ModtaleProject{
			ID:             p.ID,
			Title:          p.Title,
			Description:    p.Description,
			Author:         p.Author,
			ImageURL:       httpsOnly(p.ImageURL),
			Classification: p.Classification,
			Downloads:      p.DownloadCount,
			UpdatedAt:      p.UpdatedAt,
			PageURL:        modtalePageURL(p.ID),
		})
	}
	s.store(key, res, modtaleSearchTTL)
	return res, nil
}

// httpsOnly drops anything but an https URL, so the browser never loads an
// image over plain http or a javascript: URL from upstream data.
func httpsOnly(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return ""
	}
	return u.String()
}

type modtaleProjectInfo struct {
	ID             string
	Title          string
	Classification string
	Versions       []ModtaleVersion
}

// project returns the project's title, classification and versions (newest
// first), cached for a few minutes.
func (s *ModtaleService) project(ctx context.Context, id string) (*modtaleProjectInfo, error) {
	if !modtaleIDRe.MatchString(id) {
		return nil, ErrModtaleInvalidID
	}
	key := "project/" + strings.ToLower(id)
	if v, ok := s.cached(key); ok {
		return v.(*modtaleProjectInfo), nil
	}
	var meta struct {
		ID             string `json:"id"`
		Title          string `json:"title"`
		Classification string `json:"classification"`
	}
	if err := s.get(ctx, "/projects/"+url.PathEscape(id), &meta); err != nil {
		return nil, err
	}
	var raw struct {
		Versions []struct {
			ID            string   `json:"id"`
			VersionNumber string   `json:"versionNumber"`
			FileURL       string   `json:"fileUrl"`
			Channel       string   `json:"channel"`
			GameVersions  []string `json:"gameVersions"`
			ReleaseDate   string   `json:"releaseDate"`
			DownloadCount int64    `json:"downloadCount"`
		} `json:"versions"`
	}
	if err := s.get(ctx, "/projects/"+url.PathEscape(id)+"/versions", &raw); err != nil {
		return nil, err
	}
	info := &modtaleProjectInfo{ID: meta.ID, Title: meta.Title, Classification: meta.Classification, Versions: []ModtaleVersion{}}
	for _, v := range raw.Versions {
		name := modtaleFileName(v.FileURL)
		if name == "" || v.VersionNumber == "" {
			continue
		}
		gv := v.GameVersions
		if gv == nil {
			gv = []string{}
		}
		info.Versions = append(info.Versions, ModtaleVersion{
			ID:           v.ID,
			Version:      v.VersionNumber,
			FileName:     name,
			Channel:      v.Channel,
			GameVersions: gv,
			ReleasedAt:   v.ReleaseDate,
			Downloads:    v.DownloadCount,
		})
	}
	s.store(key, info, modtaleProjectTTL)
	return info, nil
}

// Versions lists a project's versions, newest first.
func (s *ModtaleService) Versions(ctx context.Context, id string) ([]ModtaleVersion, error) {
	info, err := s.project(ctx, id)
	if err != nil {
		return nil, err
	}
	return info.Versions, nil
}

// modtaleStoredPrefix is the "<uuid>-" Modtale puts in front of a stored file
// name; the download is served under the name without it.
var modtaleStoredPrefix = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}-`)

// modtaleFileName turns a version's fileUrl ("files/plugin/<uuid>-Name-1.0.jar")
// into the file name to install ("Name-1.0.jar"). It returns "" for anything
// that is not a plain .jar or .zip name.
func modtaleFileName(fileURL string) string {
	name := modtaleStoredPrefix.ReplaceAllString(path.Base(fileURL), "")
	if !safeModFileName(name) {
		return ""
	}
	return name
}

// safeModFileName: a single path component ending in .jar or .zip, made of
// characters that are harmless in a file name and a shell.
func safeModFileName(name string) bool {
	if len(name) < 5 || len(name) > 200 || strings.HasPrefix(name, ".") {
		return false
	}
	lower := strings.ToLower(name)
	if !strings.HasSuffix(lower, ".jar") && !strings.HasSuffix(lower, ".zip") {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.', r == '-', r == '_', r == '+', r == ' ', r == '(', r == ')', r == '[', r == ']':
		default:
			return false
		}
	}
	return true
}

// ResolveDownload picks a version of an installable project (versionNumber
// empty: the newest release, else the newest version of any channel) and
// asks Modtale for a signed download link.
func (s *ModtaleService) ResolveDownload(ctx context.Context, projectID, versionNumber string) (*ModtaleDownload, error) {
	info, err := s.project(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if !modtaleInstallable[strings.ToUpper(info.Classification)] {
		return nil, ErrModtaleNotInstallable
	}
	var picked *ModtaleVersion
	for i := range info.Versions {
		v := &info.Versions[i]
		if versionNumber != "" {
			if v.Version == versionNumber {
				picked = v
				break
			}
			continue
		}
		if strings.EqualFold(v.Channel, "RELEASE") {
			picked = v
			break
		}
	}
	if picked == nil && versionNumber == "" && len(info.Versions) > 0 {
		picked = &info.Versions[0]
	}
	if picked == nil {
		return nil, ErrModtaleNotFound
	}

	var dl struct {
		DownloadURL string `json:"downloadUrl"`
	}
	if err := s.get(ctx, "/projects/"+url.PathEscape(info.ID)+"/versions/"+url.PathEscape(picked.Version)+"/download-url", &dl); err != nil {
		return nil, err
	}
	link, err := s.downloadLink(dl.DownloadURL)
	if err != nil {
		return nil, err
	}
	return &ModtaleDownload{
		ProjectID: info.ID,
		Title:     info.Title,
		Version:   picked.Version,
		FileName:  picked.FileName,
		URL:       link,
	}, nil
}

// downloadLink turns the relative "/download/<token>" Modtale returns into an
// absolute URL on the API host. Anything else (another host, another path) is
// refused, so upstream data cannot point the node somewhere else.
func (s *ModtaleService) downloadLink(rel string) (string, error) {
	base, err := url.Parse(s.base)
	if err != nil {
		return "", err
	}
	ref, err := url.Parse(rel)
	if err != nil || ref.IsAbs() || ref.Host != "" || !strings.HasPrefix(ref.Path, "/download/") ||
		strings.Contains(ref.Path[len("/download/"):], "/") || len(ref.Path) <= len("/download/") {
		return "", fmt.Errorf("unexpected Modtale download link %q", rel)
	}
	return base.Scheme + "://" + base.Host + strings.TrimSuffix(base.Path, "/") + ref.Path, nil
}
