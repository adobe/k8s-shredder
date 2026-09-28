/*
Copyright 2025 Adobe. All rights reserved.
This file is licensed to you under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License. You may obtain a copy
of the License at http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software distributed under
the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
OF ANY KIND, either express or implied. See the License for the specific language
governing permissions and limitations under the License.
*/

package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestValidateKarpenterStuckTerminationTTL tests the ValidateKarpenterStuckTerminationTTL function
func TestValidateKarpenterStuckTerminationTTL(t *testing.T) {
	tests := []struct {
		name        string
		cfg         Config
		expectError bool
	}{
		{
			name: "feature disabled, inverted TTLs are not validated",
			cfg: Config{
				EnableKarpenterStuckTerminationDetection: false,
				KarpenterStuckTerminationTTL:             24 * time.Hour,
				ParkedNodeTTL:                            60 * time.Minute,
			},
			expectError: false,
		},
		{
			name: "feature enabled, valid TTL ordering (shipped defaults)",
			cfg: Config{
				EnableKarpenterStuckTerminationDetection: true,
				KarpenterStuckTerminationTTL:             24 * time.Hour,
				ParkedNodeTTL:                            168 * time.Hour,
			},
			expectError: false,
		},
		{
			name: "feature enabled, KarpenterStuckTerminationTTL greater than ParkedNodeTTL",
			cfg: Config{
				EnableKarpenterStuckTerminationDetection: true,
				KarpenterStuckTerminationTTL:             24 * time.Hour,
				ParkedNodeTTL:                            60 * time.Minute,
			},
			expectError: true,
		},
		{
			name: "feature enabled, KarpenterStuckTerminationTTL equal to ParkedNodeTTL",
			cfg: Config{
				EnableKarpenterStuckTerminationDetection: true,
				KarpenterStuckTerminationTTL:             24 * time.Hour,
				ParkedNodeTTL:                            24 * time.Hour,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.ValidateKarpenterStuckTerminationTTL()

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
