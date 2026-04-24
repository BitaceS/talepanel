package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const updateCacheKey = "update:latest_release"
const updateCacheTTL = 24 * time.Hour

type UpdateInfo struct {
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	HasUpdate      bool   `json:"has_update"`
	ReleaseURL     string `json:"release_url"`
	PublishedAt    string `json:"published_at"`
}

type UpdateService struct {
	rdb            *redis.Client
	currentVersion string
	log            *zap.Logger
}

func NewUpdateService(rdb *redis.Client, currentVersion string, log *zap.Logger) *UpdateService {
	return &UpdateService{rdb: rdb, currentVersion: currentVersion, log: log}
}

func (s *UpdateService) CheckForUpdate(ctx context.Context) (*UpdateInfo, error) {
	// Try cache first
	cached, err := s.rdb.Get(ctx, updateCacheKey).Result()
	if err == nil {
		var info UpdateInfo
		if jsonErr := json.Unmarshal([]byte(cached), &info); jsonErr == nil {
			return &info, nil
		}
	}

	// Fetch from GitHub
	info, err := s.fetchFromGitHub(ctx)
	if err != nil {
		s.log.Warn("update check failed", zap.Error(err))
		// Return a safe default if fetch fails
		return &UpdateInfo{CurrentVersion: s.currentVersion, LatestVersion: s.currentVersion, HasUpdate: false}, nil
	}

	// Cache result
	if data, jsonErr := json.Marshal(info); jsonErr == nil {
		_ = s.rdb.Set(ctx, updateCacheKey, data, updateCacheTTL).Err()
	}
	return info, nil
}

type githubRelease struct {
	TagName     string `json:"tag_name"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
}

func (s *UpdateService) fetchFromGitHub(ctx context.Context) (*UpdateInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.github.com/repos/BitaceS/talepanel/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github API returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var release githubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, err
	}

	hasUpdate := release.TagName != "" && release.TagName != s.currentVersion && s.currentVersion != "dev"

	return &UpdateInfo{
		CurrentVersion: s.currentVersion,
		LatestVersion:  release.TagName,
		HasUpdate:      hasUpdate,
		ReleaseURL:     release.HTMLURL,
		PublishedAt:    release.PublishedAt,
	}, nil
}
