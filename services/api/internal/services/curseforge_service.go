package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
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
		return "CurseForge game ID not found — Hytale may not be registered yet"
	default:
		return fmt.Sprintf("CurseForge returned HTTP %d", e.Status)
	}
}

// CurseForgeService wraps the CurseForge REST API.
type CurseForgeService struct {
	apiKey string
	gameID int
	client *http.Client
}

func NewCurseForgeService(apiKey string, gameID int) *CurseForgeService {
	return &CurseForgeService{
		apiKey: apiKey,
		gameID: gameID,
		client: &http.Client{},
	}
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
	if s.apiKey == "" {
		return nil, &CurseForgeError{Status: 403}
	}
	params := url.Values{}
	params.Set("gameId", strconv.Itoa(s.gameID))
	params.Set("searchFilter", query)
	params.Set("index", strconv.Itoa(page*pageSize))
	params.Set("pageSize", strconv.Itoa(pageSize))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		curseForgeAPIBase+"/mods/search?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", s.apiKey)
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
	req.Header.Set("x-api-key", s.apiKey)
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
