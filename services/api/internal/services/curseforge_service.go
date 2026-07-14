package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

const curseForgeAPIBase = "https://api.curseforge.com/v1"

// CurseForgeError carries the upstream HTTP status so callers can react to it.
type CurseForgeError struct {
	Status int
}

func (e *CurseForgeError) Error() string {
	switch e.Status {
	case 401, 403:
		return "CurseForge API key is invalid or not configured"
	case 404:
		return "CurseForge rejected the game ID"
	default:
		return fmt.Sprintf("CurseForge returned HTTP %d", e.Status)
	}
}

// hytaleSlug is how CurseForge names the game in its own catalogue.  We resolve
// the numeric id from it at runtime rather than making every operator hunt down
// a magic number: CURSEFORGE_GAME_ID stays available as an override, but an
// empty one is no longer a broken install.
const hytaleSlug = "hytale"

// CurseForgeService wraps the CurseForge REST API.  apiKey is mutable at
// runtime so the admin UI can update it without restarting the API.
type CurseForgeService struct {
	mu         sync.RWMutex
	apiKey     string
	gameID     int // operator override; 0 means "resolve it yourself"
	resolvedID int // discovered from /v1/games, cached for the process lifetime
	client     *http.Client
}

func NewCurseForgeService(apiKey string, gameID int) *CurseForgeService {
	return &CurseForgeService{
		apiKey: apiKey,
		gameID: gameID,
		client: &http.Client{},
	}
}

type cfGame struct {
	ID   int    `json:"id"`
	Slug string `json:"slug"`
}

type cfGamesResult struct {
	Data       []cfGame     `json:"data"`
	Pagination CFPagination `json:"pagination"`
}

// gameID returns the CurseForge id for Hytale: the configured override if the
// operator set one, otherwise the id looked up once from /v1/games by slug.
func (s *CurseForgeService) resolveGameID(ctx context.Context) (int, error) {
	s.mu.RLock()
	if s.gameID != 0 {
		defer s.mu.RUnlock()
		return s.gameID, nil
	}
	if s.resolvedID != 0 {
		defer s.mu.RUnlock()
		return s.resolvedID, nil
	}
	s.mu.RUnlock()

	const pageSize = 50
	for index := 0; ; index += pageSize {
		params := url.Values{}
		params.Set("index", strconv.Itoa(index))
		params.Set("pageSize", strconv.Itoa(pageSize))

		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			curseForgeAPIBase+"/games?"+params.Encode(), nil)
		if err != nil {
			return 0, err
		}
		req.Header.Set("x-api-key", s.currentKey())
		req.Header.Set("Accept", "application/json")

		resp, err := s.client.Do(req)
		if err != nil {
			return 0, fmt.Errorf("curseforge games request: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return 0, &CurseForgeError{Status: resp.StatusCode}
		}

		var result cfGamesResult
		err = json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		if err != nil {
			return 0, fmt.Errorf("decoding games response: %w", err)
		}

		for _, g := range result.Data {
			if strings.EqualFold(g.Slug, hytaleSlug) {
				s.mu.Lock()
				s.resolvedID = g.ID
				s.mu.Unlock()
				return g.ID, nil
			}
		}

		if len(result.Data) < pageSize || index+pageSize >= result.Pagination.TotalCount {
			break
		}
	}
	return 0, fmt.Errorf("CurseForge does not list a game with slug %q — set CURSEFORGE_GAME_ID to override", hytaleSlug)
}

// SetAPIKey replaces the in-memory key.  Called by the admin handler after a
// successful save so subsequent CurseForge calls use the new credential.
func (s *CurseForgeService) SetAPIKey(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apiKey = key
}

// HasAPIKey reports whether a key is currently configured (without leaking it).
func (s *CurseForgeService) HasAPIKey() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.apiKey != ""
}

func (s *CurseForgeService) currentKey() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.apiKey
}

// ─── Response types ───────────────────────────────────────────────────────────

type CFMod struct {
	ID            int      `json:"id"`
	Name          string   `json:"name"`
	Summary       string   `json:"summary"`
	DownloadCount float64  `json:"downloadCount"`
	Logo          *CFAsset `json:"logo"`
	Links         CFLinks  `json:"links"`
	LatestFiles   []CFFile `json:"latestFiles"`
}

type CFAsset struct {
	ThumbnailURL string `json:"thumbnailUrl"`
	URL          string `json:"url"`
}

type CFLinks struct {
	WebsiteURL string `json:"websiteUrl"`
}

type CFFile struct {
	ID           int      `json:"id"`
	DisplayName  string   `json:"displayName"`
	FileName     string   `json:"fileName"`
	DownloadURL  string   `json:"downloadUrl"`
	GameVersions []string `json:"gameVersions"`
	FileDate     string   `json:"fileDate"`
}

type CFSearchResult struct {
	Data       []CFMod      `json:"data"`
	Pagination CFPagination `json:"pagination"`
}

type CFPagination struct {
	Index       int `json:"index"`
	PageSize    int `json:"pageSize"`
	ResultCount int `json:"resultCount"`
	TotalCount  int `json:"totalCount"`
}

// ─── Methods ──────────────────────────────────────────────────────────────────

// SearchMods queries the CurseForge search endpoint for the configured game.
func (s *CurseForgeService) SearchMods(ctx context.Context, query string, page, pageSize int) (*CFSearchResult, error) {
	if s.currentKey() == "" {
		return nil, &CurseForgeError{Status: 403}
	}
	gameID, err := s.resolveGameID(ctx)
	if err != nil {
		return nil, err
	}
	params := url.Values{}
	params.Set("gameId", strconv.Itoa(gameID))
	params.Set("searchFilter", query)
	params.Set("index", strconv.Itoa(page*pageSize))
	params.Set("pageSize", strconv.Itoa(pageSize))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		curseForgeAPIBase+"/mods/search?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", s.currentKey())
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("curseforge request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &CurseForgeError{Status: resp.StatusCode}
	}

	var result CFSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result, nil
}

// GetModFiles returns all files available for the given mod ID.
func (s *CurseForgeService) GetModFiles(ctx context.Context, modID int) ([]CFFile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/mods/%d/files", curseForgeAPIBase, modID), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", s.currentKey())
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("curseforge request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &CurseForgeError{Status: resp.StatusCode}
	}

	var result struct {
		Data []CFFile `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return result.Data, nil
}
