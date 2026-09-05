package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const latestReleaseURL = "https://api.github.com/repos/gallez-tech/pageferry-cli/releases/latest"

type Result struct {
	Current    string
	Latest     string
	Outdated   bool
	ReleaseURL string
}

func Check(ctx context.Context, current string, client *http.Client) (Result, error) {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "pageferry/"+current)

	response, err := client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("check GitHub releases: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("check GitHub releases: HTTP %d", response.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(response.Body).Decode(&release); err != nil {
		return Result{}, fmt.Errorf("decode GitHub release: %w", err)
	}
	latest, ok := parseVersion(release.TagName)
	if !ok {
		return Result{}, fmt.Errorf("latest release has invalid version %q", release.TagName)
	}
	currentVersion, currentOK := parseVersion(current)

	return Result{
		Current:    strings.TrimPrefix(current, "v"),
		Latest:     strings.TrimPrefix(release.TagName, "v"),
		Outdated:   currentOK && compare(currentVersion, latest) < 0,
		ReleaseURL: release.HTMLURL,
	}, nil
}

func parseVersion(value string) ([3]int, bool) {
	var result [3]int
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	value = strings.SplitN(value, "-", 2)[0]
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return result, false
	}
	for index, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return result, false
		}
		result[index] = number
	}
	return result, true
}

func compare(left, right [3]int) int {
	for index := range left {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	return 0
}
