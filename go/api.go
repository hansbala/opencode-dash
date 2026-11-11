package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const cycleLength = 30 * 24 * time.Hour

type serviceInfo struct {
	URL      string `json:"url"`
	Password string `json:"password"`
}

type limitsEnvelope struct {
	Data limitsInfo `json:"data"`
}

type limitsInfo struct {
	Workspace struct {
		Name     string `json:"name"`
		UsageURL string `json:"usageURL"`
	} `json:"workspace"`
	Go *goInfo `json:"go"`
}

type goInfo struct {
	Rolling limitWindow `json:"rolling"`
	Weekly  limitWindow `json:"weekly"`
	Monthly limitWindow `json:"monthly"`
}

type limitWindow struct {
	Percent  float64 `json:"percent"`
	ResetsAt int64   `json:"resetsAt"`
}

func (w limitWindow) resetDate() time.Time {
	return time.UnixMilli(w.ResetsAt)
}

type statsEnvelope struct {
	Data statsInfo `json:"data"`
}

type statsInfo struct {
	Sessions int           `json:"sessions"`
	Prompts  int           `json:"prompts"`
	Steps    int           `json:"steps"`
	Tokens   tokenSummary  `json:"tokens"`
	Cost     float64       `json:"cost"`
	Activity []dayActivity `json:"activity"`
	Models   []modelUsage  `json:"models"`
}

type tokenSummary struct {
	Input     int         `json:"input"`
	Output    int         `json:"output"`
	Reasoning int         `json:"reasoning"`
	Cache     cacheTokens `json:"cache"`
}

func (t tokenSummary) Total() int {
	return t.Input + t.Output + t.Reasoning + t.Cache.Read + t.Cache.Write
}

type cacheTokens struct {
	Read  int `json:"read"`
	Write int `json:"write"`
}

type dayActivity struct {
	Date  string `json:"date"`
	Steps int    `json:"steps"`
}

type modelUsage struct {
	Model modelRef `json:"model"`
	Steps int      `json:"steps"`
	Cost  float64  `json:"cost"`
}

type modelRef struct {
	ID string `json:"id"`
}

type sessionList struct {
	Data []sessionSummary `json:"data"`
}

type sessionSummary struct {
	ID    string      `json:"id"`
	Title string      `json:"title"`
	Model *modelRef   `json:"model"`
	Cost  float64     `json:"cost"`
	Time  sessionTime `json:"time"`
}

type sessionTime struct {
	Created int64 `json:"created"`
}

type snapshot struct {
	workspace string
	usageURL  string
	goInfo    *goInfo
	stats     statsInfo
	fetchedAt time.Time
}

func (s *snapshot) percent() float64 {
	if s.goInfo == nil {
		return 0
	}
	return s.goInfo.Monthly.Percent
}

func (s *snapshot) resetDate() time.Time {
	if s.goInfo == nil {
		return time.Time{}
	}
	return s.goInfo.Monthly.resetDate()
}

func (s *snapshot) cycleStart() time.Time {
	reset := s.resetDate()
	if reset.IsZero() {
		return time.Time{}
	}
	return reset.Add(-cycleLength)
}

func (s *snapshot) pace() (float64, bool) {
	reset := s.resetDate()
	if reset.IsZero() {
		return 0, false
	}
	start := reset.Add(-cycleLength)
	total := reset.Sub(start)
	elapsed := time.Since(start)
	if elapsed <= time.Second || total <= time.Second {
		return 0, false
	}
	return s.percent() * total.Seconds() / elapsed.Seconds(), true
}

func (s *snapshot) dailyAverage() (float64, bool) {
	start := s.cycleStart()
	if start.IsZero() {
		return 0, false
	}
	days := time.Since(start).Hours() / 24
	if days <= 0.2 {
		return 0, false
	}
	return s.percent() / days, true
}

func (s *snapshot) modelsByUsage() []modelUsage {
	models := append([]modelUsage(nil), s.stats.Models...)
	sort.SliceStable(models, func(i, j int) bool { return models[i].Steps > models[j].Steps })
	return models
}

func (s *snapshot) share(usage modelUsage) float64 {
	total := 0
	for _, model := range s.stats.Models {
		total += model.Steps
	}
	if total == 0 {
		return 0
	}
	return float64(usage.Steps) / float64(total) * 100
}

type chartPoint struct {
	day     float64
	percent float64
}

func (s *snapshot) chartPoints() []chartPoint {
	start := s.cycleStart()
	if start.IsZero() {
		return nil
	}
	startDay := dayStart(start)
	totalSteps := 0
	for _, activity := range s.stats.Activity {
		totalSteps += activity.Steps
	}
	if totalSteps == 0 {
		totalSteps = 1
	}

	points := []chartPoint{{0, 0}}
	cumulative := 0
	for _, activity := range s.stats.Activity {
		date, err := time.ParseInLocation("2006-01-02", activity.Date, time.Local)
		if err != nil {
			continue
		}
		index := daysBetween(startDay, date)
		cumulative += activity.Steps
		points = append(points, chartPoint{
			day:     float64(index + 1),
			percent: s.percent() * float64(cumulative) / float64(totalSteps),
		})
	}

	today := daysBetween(startDay, dayStart(time.Now()))
	if points[len(points)-1].day < float64(today) {
		points = append(points, chartPoint{day: float64(today), percent: s.percent()})
	}
	return points
}

func dayStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
}

func daysBetween(start, end time.Time) int {
	return int(end.Sub(start).Hours()/24 + 0.5)
}

type apiClient struct {
	base     string
	password string
	http     *http.Client
}

func serviceFilePath() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "opencode", "service.json"), nil
}

func newAPIClient() (*apiClient, error) {
	path, err := serviceFilePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("OpenCode service not found at %s", path)
	}
	var service serviceInfo
	if err := json.Unmarshal(data, &service); err != nil || service.URL == "" {
		return nil, fmt.Errorf("invalid OpenCode service file at %s", path)
	}
	return &apiClient{
		base:     strings.TrimRight(service.URL, "/"),
		password: service.Password,
		http:     &http.Client{Timeout: 8 * time.Second},
	}, nil
}

func (c *apiClient) get(path string, query url.Values, target any) error {
	endpoint := c.base + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.SetBasicAuth("opencode", c.password)

	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("OpenCode API returned HTTP %d", response.StatusCode)
	}
	return json.NewDecoder(response.Body).Decode(target)
}

func fetchSnapshot() (*snapshot, error) {
	client, err := newAPIClient()
	if err != nil {
		return nil, err
	}

	var limits limitsEnvelope
	if err := client.get("/api/experimental/usage/limits", nil, &limits); err != nil {
		return nil, err
	}

	reset := time.Now().Add(cycleLength)
	if limits.Data.Go != nil {
		reset = limits.Data.Go.Monthly.resetDate()
	}
	query := url.Values{}
	query.Set("from", strconv.FormatInt(reset.Add(-cycleLength).UnixMilli(), 10))
	query.Set("to", strconv.FormatInt(time.Now().UnixMilli(), 10))
	query.Set("tools", "none")

	var stats statsEnvelope
	if err := client.get("/api/experimental/session/stats", query, &stats); err != nil {
		return nil, err
	}

	return &snapshot{
		workspace: limits.Data.Workspace.Name,
		usageURL:  limits.Data.Workspace.UsageURL,
		goInfo:    limits.Data.Go,
		stats:     stats.Data,
		fetchedAt: time.Now(),
	}, nil
}

func fetchSessions(limit int) ([]sessionSummary, error) {
	client, err := newAPIClient()
	if err != nil {
		return nil, err
	}
	var list sessionList
	query := url.Values{}
	query.Set("limit", strconv.Itoa(limit))
	if err := client.get("/api/session", query, &list); err != nil {
		return nil, err
	}
	if len(list.Data) > limit {
		list.Data = list.Data[:limit]
	}
	return list.Data, nil
}

func compactCount(value int) string {
	switch {
	case value >= 1_000_000_000:
		return fmt.Sprintf("%.2fB", float64(value)/1_000_000_000)
	case value >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(value)/1_000_000)
	case value >= 1_000:
		return fmt.Sprintf("%.0fK", float64(value)/1_000)
	default:
		return strconv.Itoa(value)
	}
}

func countdown(date time.Time) string {
	seconds := time.Until(date).Seconds()
	if seconds < 60 {
		return "now"
	}
	days := int(seconds / 86_400)
	hours := int(math.Mod(seconds, 86_400) / 3_600)
	minutes := int(math.Mod(seconds, 3_600) / 60)
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

func shortDate(date time.Time) string {
	return date.Format("Jan 2")
}

func relativeTime(date time.Time) string {
	seconds := time.Since(date).Seconds()
	if seconds < 60 {
		return "now"
	}
	if seconds < 3_600 {
		return fmt.Sprintf("%dm ago", int(seconds/60))
	}
	if seconds < 86_400 {
		return fmt.Sprintf("%dh ago", int(seconds/3_600))
	}
	return fmt.Sprintf("%dd ago", int(seconds/86_400))
}
