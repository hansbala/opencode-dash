package main

import (
	"strings"
	"testing"
	"time"
)

func testSnapshot() *snapshot {
	now := time.Now()
	return &snapshot{
		workspace: "Test",
		usageURL:  "https://example.com",
		goInfo: &goInfo{
			Rolling: limitWindow{Percent: 5, ResetsAt: now.Add(time.Hour).UnixMilli()},
			Weekly:  limitWindow{Percent: 27, ResetsAt: now.Add(4 * 24 * time.Hour).UnixMilli()},
			Monthly: limitWindow{Percent: 24, ResetsAt: now.Add(21 * 24 * time.Hour).UnixMilli()},
		},
		stats: statsInfo{
			Sessions: 2,
			Prompts:  10,
			Steps:    100,
			Tokens:   tokenSummary{Input: 1000, Output: 200, Cache: cacheTokens{Read: 5000}},
			Cost:     1.23,
			Activity: []dayActivity{{Date: now.Format("2006-01-02"), Steps: 100}},
			Models: []modelUsage{
				{Model: modelRef{ID: "deepseek-v4.1-flash"}, Steps: 94, Cost: 1.16},
				{Model: modelRef{ID: "gpt-6-luna"}, Steps: 6, Cost: 0.07},
			},
		},
		fetchedAt: now,
	}
}

func TestRenderFitsTerminal(t *testing.T) {
	snap := testSnapshot()
	sessions := []sessionSummary{{
		ID:    "ses_test",
		Title: "A very long session title that should be truncated cleanly",
		Model: &modelRef{ID: "deepseek-v4.1-flash"},
		Cost:  1.23,
		Time:  sessionTime{Created: time.Now().UnixMilli()},
	}}

	for _, color := range []bool{false, true} {
		for _, size := range [][2]int{{80, 24}, {50, 20}, {120, 40}} {
			for _, showSessions := range []bool{false, true} {
				output := render(snap, sessions, showSessions, size[0], size[1], color, "")
				lines := strings.Split(output, "\n")
				if len(lines) > size[1] {
					t.Fatalf("color=%v sessions=%v size=%v: %d rows", color, showSessions, size, len(lines))
				}
				for index, line := range lines {
					if width := visibleLength(line); width > size[0] {
						t.Fatalf("color=%v sessions=%v size=%v: row %d is %d columns", color, showSessions, size, index, width)
					}
				}
			}
		}
	}
}

func TestRenderWithoutColorHasNoEscapes(t *testing.T) {
	output := render(testSnapshot(), nil, false, 80, 24, false, "")
	if strings.ContainsRune(output, '\x1b') {
		t.Fatal("monochrome output contains an ANSI escape")
	}
}
