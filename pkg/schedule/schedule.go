/*
Copyright 2026 Adobe. All rights reserved.
This file is licensed to you under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License. You may obtain a copy
of the License at http://www.apache.org/licenses/LICENSE/2.0
Unless required by applicable law or agreed to in writing, software distributed under
the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
OF ANY KIND, either express or implied. See the License for the specific language
governing permissions and limitations under the License.
*/

package schedule

import (
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/robfig/cron/v3"
)

// Schedule represents a time window defined by a cron schedule and duration
type Schedule struct {
	// CronSchedule is the cron expression (supports macros like @daily, @hourly, etc.)
	CronSchedule string
	// Duration is how long the window stays active after the schedule triggers
	Duration time.Duration
	// parser is the cron parser instance
	parser cron.Parser
	// schedule is the parsed cron schedule
	schedule cron.Schedule
}

// NewSchedule creates a new Schedule instance from a cron expression and duration string
// The cron expression supports standard cron syntax and macros (@yearly, @monthly, @weekly, @daily, @hourly)
// The duration string supports compound durations with minutes and hours (e.g., "10h5m", "30m", "160h")
func NewSchedule(cronExpr string, durationStr string) (*Schedule, error) {
	if cronExpr == "" {
		return nil, errors.New("cron schedule cannot be empty")
	}

	if durationStr == "" {
		return nil, errors.New("duration cannot be empty")
	}

	// Parse duration - supports compound durations like "10h5m", "30m", "160h"
	duration, err := parseDuration(durationStr)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse duration: %s", durationStr)
	}

	if duration <= 0 {
		return nil, errors.New("duration must be greater than zero")
	}

	// Create parser with support for standard cron format and macros
	// Try parsing with seconds first (6 fields), then without seconds (5 fields - Kubernetes format)
	var schedule cron.Schedule
	var parser cron.Parser

	// First try with seconds (6 fields: second minute hour dom month dow)
	parser6 := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	schedule, err = parser6.Parse(cronExpr)
	if err != nil {
		// If that fails, try without seconds (5 fields: minute hour dom month dow - Kubernetes format)
		parser5 := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
		schedule, err = parser5.Parse(cronExpr)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to parse cron schedule: %s", cronExpr)
		}
		// Use the 5-field parser for future operations
		parser = parser5
	} else {
		parser = parser6
	}

	return &Schedule{
		CronSchedule: cronExpr,
		Duration:     duration,
		parser:       parser,
		schedule:     schedule,
	}, nil
}

// IsActive checks if the current time (or provided time) falls within the active window
// The window is active from when the schedule triggers until Duration time has passed
func (s *Schedule) IsActive(now time.Time) bool {
	if s.schedule == nil {
		return false
	}

	// Get the most recent time the schedule triggered (before or at now)
	// We need to find the last trigger time that is <= now
	lastTrigger := s.getLastTriggerTime(now)

	if lastTrigger.IsZero() {
		return false
	}

	// Check if we're still within the duration window
	windowEnd := lastTrigger.Add(s.Duration)
	return now.Before(windowEnd) || now.Equal(windowEnd)
}

// getLastTriggerTime finds the most recent time the schedule triggered before or at the given time
func (s *Schedule) getLastTriggerTime(now time.Time) time.Time {
	// For macros, we can calculate directly for efficiency
	cronLower := strings.ToLower(s.CronSchedule)
	switch cronLower {
	case "@yearly", "@annually":
		// Triggers at 00:00:00 UTC on January 1st
		// Convert to UTC first for consistent calculations
		nowUTC := now.In(time.UTC)
		lastYear := time.Date(nowUTC.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		if lastYear.After(nowUTC) {
			lastYear = time.Date(nowUTC.Year()-1, 1, 1, 0, 0, 0, 0, time.UTC)
		}
		return lastYear
	case "@monthly":
		// Triggers at 00:00:00 UTC on the 1st of each month
		// Convert to UTC first for consistent calculations
		nowUTC := now.In(time.UTC)
		lastMonth := time.Date(nowUTC.Year(), nowUTC.Month(), 1, 0, 0, 0, 0, time.UTC)
		if lastMonth.After(nowUTC) {
			if nowUTC.Month() == 1 {
				lastMonth = time.Date(nowUTC.Year()-1, 12, 1, 0, 0, 0, 0, time.UTC)
			} else {
				lastMonth = time.Date(nowUTC.Year(), nowUTC.Month()-1, 1, 0, 0, 0, 0, time.UTC)
			}
		}
		return lastMonth
	case "@weekly":
		// Triggers at 00:00:00 UTC on Sunday
		// Convert to UTC first to ensure consistent day-of-week calculations
		nowUTC := now.In(time.UTC)
		// Start from midnight of the current day
		lastWeek := time.Date(nowUTC.Year(), nowUTC.Month(), nowUTC.Day(), 0, 0, 0, 0, time.UTC)
		// Go back to the most recent Sunday at midnight
		for lastWeek.Weekday() != time.Sunday || lastWeek.After(nowUTC) {
			lastWeek = lastWeek.AddDate(0, 0, -1)
		}
		return lastWeek
	case "@daily", "@midnight":
		// Triggers at 00:00:00 UTC each day
		// Convert to UTC first for consistent calculations
		nowUTC := now.In(time.UTC)
		lastDay := time.Date(nowUTC.Year(), nowUTC.Month(), nowUTC.Day(), 0, 0, 0, 0, time.UTC)
		if lastDay.After(nowUTC) {
			lastDay = lastDay.AddDate(0, 0, -1)
		}
		return lastDay
	case "@hourly":
		// Triggers at the top of each hour
		// Convert to UTC first for consistent calculations
		nowUTC := now.In(time.UTC)
		lastHour := time.Date(nowUTC.Year(), nowUTC.Month(), nowUTC.Day(), nowUTC.Hour(), 0, 0, 0, time.UTC)
		if lastHour.After(nowUTC) {
			lastHour = lastHour.Add(-time.Hour)
		}
		return lastHour
	}

	// For standard cron expressions, find the last trigger by iterating forward
	// The cron library's Next() only looks forward, so we start from before the expected trigger
	// Convert to UTC first for consistency
	nowUTC := now.In(time.UTC)
	checkWindow := s.getCheckWindow()
	
	// Start checking from checkWindow before now
	startTime := nowUTC.Add(-checkWindow)
	
	var lastTrigger time.Time
	currentTime := startTime
	maxIterations := 10000 // Safety limit (increased since we're going forward)
	
	for i := 0; i < maxIterations; i++ {
		// Get the next trigger from currentTime
		nextTrigger := s.schedule.Next(currentTime)
		
		// If the next trigger is after now, we've gone past - return the last one we found
		if nextTrigger.After(nowUTC) {
			return lastTrigger
		}
		
		// This trigger is at or before now, so it's a candidate
		lastTrigger = nextTrigger
		
		// Move forward to just after this trigger to find the next one
		currentTime = nextTrigger.Add(time.Second)
		
		// Safety check: if we've gone past now, stop
		if currentTime.After(nowUTC) {
			return lastTrigger
		}
	}
	
	// If we hit max iterations, return the best we found
	return lastTrigger
}

// getCheckWindow returns the maximum time window to check backwards
// This is optimized based on the schedule type
func (s *Schedule) getCheckWindow() time.Duration {
	cronLower := strings.ToLower(s.CronSchedule)

	// Handle macros
	switch cronLower {
	case "@yearly", "@annually":
		return 2 * 365 * 24 * time.Hour
	case "@monthly":
		return 2 * 30 * 24 * time.Hour
	case "@weekly":
		return 2 * 7 * 24 * time.Hour
	case "@daily", "@midnight":
		return 2 * 24 * time.Hour
	case "@hourly":
		return 2 * time.Hour
	default:
		// For standard cron, check up to 7 days back
		// This should cover most common schedules
		return 7 * 24 * time.Hour
	}
}

// parseDuration parses a duration string supporting compound durations
// Supports formats like "10h5m", "30m", "160h", "1h30m", etc.
// Only supports hours and minutes as per Karpenter's duration format
func parseDuration(durationStr string) (time.Duration, error) {
	durationStr = strings.TrimSpace(durationStr)
	if durationStr == "" {
		return 0, errors.New("duration string cannot be empty")
	}

	var totalDuration time.Duration

	// Parse hours
	if strings.Contains(durationStr, "h") {
		parts := strings.Split(durationStr, "h")
		if len(parts) > 0 && parts[0] != "" {
			var hours int64
			_, err := fmt.Sscanf(parts[0], "%d", &hours)
			if err != nil {
				return 0, errors.Wrapf(err, "invalid hours in duration: %s", durationStr)
			}
			totalDuration += time.Duration(hours) * time.Hour
		}
		// Remaining part might contain minutes
		if len(parts) > 1 && parts[1] != "" {
			durationStr = parts[1]
		} else {
			durationStr = ""
		}
	}

	// Parse minutes
	if strings.Contains(durationStr, "m") {
		parts := strings.Split(durationStr, "m")
		if len(parts) > 0 && parts[0] != "" {
			var minutes int64
			_, err := fmt.Sscanf(parts[0], "%d", &minutes)
			if err != nil {
				return 0, errors.Wrapf(err, "invalid minutes in duration: %s", durationStr)
			}
			totalDuration += time.Duration(minutes) * time.Minute
		}
	} else if durationStr != "" {
		// If there's remaining string that's not "m", it's invalid
		return 0, errors.Errorf("invalid duration format: %s (only hours 'h' and minutes 'm' are supported)", durationStr)
	}

	if totalDuration == 0 {
		return 0, errors.New("duration must be greater than zero")
	}

	return totalDuration, nil
}
