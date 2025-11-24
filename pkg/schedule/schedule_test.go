/*
Copyright 2025 Adobe. All rights reserved.
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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSchedule(t *testing.T) {
	tests := []struct {
		name        string
		cronExpr    string
		durationStr string
		wantErr     bool
		errContains string
	}{
		{
			name:        "valid daily schedule with hours",
			cronExpr:    "@daily",
			durationStr: "10h",
			wantErr:     false,
		},
		{
			name:        "valid hourly schedule with minutes",
			cronExpr:    "@hourly",
			durationStr: "30m",
			wantErr:     false,
		},
		{
			name:        "valid standard cron with compound duration",
			cronExpr:    "0 2 * * *",
			durationStr: "10h5m",
			wantErr:     false,
		},
		{
			name:        "empty cron expression",
			cronExpr:    "",
			durationStr: "10h",
			wantErr:     true,
			errContains: "cron schedule cannot be empty",
		},
		{
			name:        "empty duration",
			cronExpr:    "@daily",
			durationStr: "",
			wantErr:     true,
			errContains: "duration cannot be empty",
		},
		{
			name:        "invalid cron expression",
			cronExpr:    "invalid",
			durationStr: "10h",
			wantErr:     true,
			errContains: "failed to parse cron schedule",
		},
		{
			name:        "invalid duration format",
			cronExpr:    "@daily",
			durationStr: "invalid",
			wantErr:     true,
			errContains: "failed to parse duration",
		},
		{
			name:        "zero duration",
			cronExpr:    "@daily",
			durationStr: "0h",
			wantErr:     true,
			errContains: "duration must be greater than zero",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sched, err := NewSchedule(tt.cronExpr, tt.durationStr)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Nil(t, sched)
			} else {
				require.NoError(t, err)
				require.NotNil(t, sched)
				assert.Equal(t, tt.cronExpr, sched.CronSchedule)
			}
		})
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		name        string
		durationStr string
		want        time.Duration
		wantErr     bool
	}{
		{
			name:        "hours only",
			durationStr: "10h",
			want:        10 * time.Hour,
			wantErr:     false,
		},
		{
			name:        "minutes only",
			durationStr: "30m",
			want:        30 * time.Minute,
			wantErr:     false,
		},
		{
			name:        "compound duration",
			durationStr: "10h5m",
			want:        10*time.Hour + 5*time.Minute,
			wantErr:     false,
		},
		{
			name:        "large hours",
			durationStr: "160h",
			want:        160 * time.Hour,
			wantErr:     false,
		},
		{
			name:        "empty string",
			durationStr: "",
			want:        0,
			wantErr:     true,
		},
		{
			name:        "invalid format",
			durationStr: "invalid",
			want:        0,
			wantErr:     true,
		},
		{
			name:        "zero hours",
			durationStr: "0h",
			want:        0,
			wantErr:     true,
		},
		{
			name:        "zero minutes",
			durationStr: "0m",
			want:        0,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDuration(tt.durationStr)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestSchedule_IsActive_Daily(t *testing.T) {
	sched, err := NewSchedule("@daily", "10h")
	require.NoError(t, err)
	require.NotNil(t, sched)

	// Test at midnight UTC (should be active)
	midnight := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	assert.True(t, sched.IsActive(midnight), "should be active at midnight")

	// Test 5 hours after midnight (should be active)
	fiveHoursLater := midnight.Add(5 * time.Hour)
	assert.True(t, sched.IsActive(fiveHoursLater), "should be active 5 hours after midnight")

	// Test 9 hours after midnight (should be active)
	nineHoursLater := midnight.Add(9 * time.Hour)
	assert.True(t, sched.IsActive(nineHoursLater), "should be active 9 hours after midnight")

	// Test 11 hours after midnight (should NOT be active, outside 10h window)
	elevenHoursLater := midnight.Add(11 * time.Hour)
	assert.False(t, sched.IsActive(elevenHoursLater), "should NOT be active 11 hours after midnight")

	// Test at 2 PM UTC (should NOT be active, outside window)
	twoPM := time.Date(2025, 1, 15, 14, 0, 0, 0, time.UTC)
	assert.False(t, sched.IsActive(twoPM), "should NOT be active at 2 PM UTC")

	// Test at 11 PM UTC (should NOT be active, before next window)
	elevenPM := time.Date(2025, 1, 15, 23, 0, 0, 0, time.UTC)
	assert.False(t, sched.IsActive(elevenPM), "should NOT be active at 11 PM UTC")
}

func TestSchedule_IsActive_Hourly(t *testing.T) {
	sched, err := NewSchedule("@hourly", "30m")
	require.NoError(t, err)
	require.NotNil(t, sched)

	// Test at top of hour (should be active)
	topOfHour := time.Date(2025, 1, 15, 14, 0, 0, 0, time.UTC)
	assert.True(t, sched.IsActive(topOfHour), "should be active at top of hour")

	// Test 15 minutes after hour (should be active)
	fifteenMinLater := topOfHour.Add(15 * time.Minute)
	assert.True(t, sched.IsActive(fifteenMinLater), "should be active 15 minutes after hour")

	// Test 29 minutes after hour (should be active)
	twentyNineMinLater := topOfHour.Add(29 * time.Minute)
	assert.True(t, sched.IsActive(twentyNineMinLater), "should be active 29 minutes after hour")

	// Test 31 minutes after hour (should NOT be active)
	thirtyOneMinLater := topOfHour.Add(31 * time.Minute)
	assert.False(t, sched.IsActive(thirtyOneMinLater), "should NOT be active 31 minutes after hour")

	// Test 45 minutes after hour (should NOT be active)
	fortyFiveMinLater := topOfHour.Add(45 * time.Minute)
	assert.False(t, sched.IsActive(fortyFiveMinLater), "should NOT be active 45 minutes after hour")
}

func TestSchedule_IsActive_StandardCron(t *testing.T) {
	// Schedule at 2 AM UTC daily with 8 hour duration
	sched, err := NewSchedule("0 2 * * *", "8h")
	require.NoError(t, err)
	require.NotNil(t, sched)

	// Test at 2 AM UTC (should be active)
	twoAM := time.Date(2025, 1, 15, 2, 0, 0, 0, time.UTC)
	assert.True(t, sched.IsActive(twoAM), "should be active at 2 AM UTC")

	// Test 4 hours after 2 AM (should be active)
	fourHoursLater := twoAM.Add(4 * time.Hour)
	assert.True(t, sched.IsActive(fourHoursLater), "should be active 4 hours after 2 AM")

	// Test 7 hours after 2 AM (should be active)
	sevenHoursLater := twoAM.Add(7 * time.Hour)
	assert.True(t, sched.IsActive(sevenHoursLater), "should be active 7 hours after 2 AM")

	// Test 9 hours after 2 AM (should NOT be active)
	nineHoursLater := twoAM.Add(9 * time.Hour)
	assert.False(t, sched.IsActive(nineHoursLater), "should NOT be active 9 hours after 2 AM")

	// Test at 1 AM UTC (should NOT be active, before trigger)
	oneAM := time.Date(2025, 1, 15, 1, 0, 0, 0, time.UTC)
	assert.False(t, sched.IsActive(oneAM), "should NOT be active at 1 AM UTC")
}

func TestSchedule_IsActive_Weekly(t *testing.T) {
	sched, err := NewSchedule("@weekly", "48h")
	require.NoError(t, err)
	require.NotNil(t, sched)

	// Test on Sunday at midnight UTC (should be active)
	sundayMidnight := time.Date(2025, 1, 12, 0, 0, 0, 0, time.UTC) // Jan 12, 2025 is a Sunday
	assert.True(t, sched.IsActive(sundayMidnight), "should be active on Sunday at midnight")

	// Test 24 hours after Sunday midnight (should be active)
	oneDayLater := sundayMidnight.Add(24 * time.Hour)
	assert.True(t, sched.IsActive(oneDayLater), "should be active 24 hours after Sunday midnight")

	// Test 47 hours after Sunday midnight (should be active)
	fortySevenHoursLater := sundayMidnight.Add(47 * time.Hour)
	assert.True(t, sched.IsActive(fortySevenHoursLater), "should be active 47 hours after Sunday midnight")

	// Test 49 hours after Sunday midnight (should NOT be active)
	fortyNineHoursLater := sundayMidnight.Add(49 * time.Hour)
	assert.False(t, sched.IsActive(fortyNineHoursLater), "should NOT be active 49 hours after Sunday midnight")

	// Test on Monday (should be active if within 48h window)
	monday := time.Date(2025, 1, 13, 0, 0, 0, 0, time.UTC) // Jan 13, 2025 is a Monday
	assert.True(t, sched.IsActive(monday), "should be active on Monday if within 48h window")
}

func TestSchedule_IsActive_Monthly(t *testing.T) {
	sched, err := NewSchedule("@monthly", "24h")
	require.NoError(t, err)
	require.NotNil(t, sched)

	// Test on 1st of month at midnight UTC (should be active)
	firstOfMonth := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	assert.True(t, sched.IsActive(firstOfMonth), "should be active on 1st of month at midnight")

	// Test 12 hours after 1st (should be active)
	twelveHoursLater := firstOfMonth.Add(12 * time.Hour)
	assert.True(t, sched.IsActive(twelveHoursLater), "should be active 12 hours after 1st")

	// Test 25 hours after 1st (should NOT be active)
	twentyFiveHoursLater := firstOfMonth.Add(25 * time.Hour)
	assert.False(t, sched.IsActive(twentyFiveHoursLater), "should NOT be active 25 hours after 1st")

	// Test on 2nd of month at midnight (exactly 24h after trigger - should be active at boundary)
	secondOfMonthMidnight := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)
	assert.True(t, sched.IsActive(secondOfMonthMidnight), "should be active exactly 24h after trigger (at boundary)")

	// Test on 2nd of month at 1 AM (should NOT be active, outside 24h window)
	secondOfMonthOneAM := time.Date(2025, 1, 2, 1, 0, 0, 0, time.UTC)
	assert.False(t, sched.IsActive(secondOfMonthOneAM), "should NOT be active on 2nd of month at 1 AM")
}

func TestSchedule_IsActive_Yearly(t *testing.T) {
	sched, err := NewSchedule("@yearly", "24h")
	require.NoError(t, err)
	require.NotNil(t, sched)

	// Test on January 1st at midnight UTC (should be active)
	janFirst := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	assert.True(t, sched.IsActive(janFirst), "should be active on January 1st at midnight")

	// Test 12 hours after January 1st (should be active)
	twelveHoursLater := janFirst.Add(12 * time.Hour)
	assert.True(t, sched.IsActive(twelveHoursLater), "should be active 12 hours after January 1st")

	// Test 25 hours after January 1st (should NOT be active)
	twentyFiveHoursLater := janFirst.Add(25 * time.Hour)
	assert.False(t, sched.IsActive(twentyFiveHoursLater), "should NOT be active 25 hours after January 1st")

	// Test on January 2nd at midnight (exactly 24h after trigger - should be active at boundary)
	janSecondMidnight := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)
	assert.True(t, sched.IsActive(janSecondMidnight), "should be active exactly 24h after trigger (at boundary)")

	// Test on January 2nd at 1 AM (should NOT be active, outside 24h window)
	janSecondOneAM := time.Date(2025, 1, 2, 1, 0, 0, 0, time.UTC)
	assert.False(t, sched.IsActive(janSecondOneAM), "should NOT be active on January 2nd at 1 AM")
}

func TestSchedule_IsActive_EdgeCases(t *testing.T) {
	sched, err := NewSchedule("@daily", "10h")
	require.NoError(t, err)
	require.NotNil(t, sched)

	// Test exactly at window end (should be active)
	midnight := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	exactlyAtEnd := midnight.Add(10 * time.Hour)
	assert.True(t, sched.IsActive(exactlyAtEnd), "should be active exactly at window end")

	// Test just after window end (should NOT be active)
	justAfterEnd := midnight.Add(10*time.Hour + time.Second)
	assert.False(t, sched.IsActive(justAfterEnd), "should NOT be active just after window end")
}
