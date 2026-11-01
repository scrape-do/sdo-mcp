// SPDX-License-Identifier: Apache-2.0

// Package testenv isolates a test binary from the developer's own Scrape.do
// settings, so a token, gateway URL or timeout exported in the shell can
// neither turn a test red nor send a test request to a real host.
package testenv

import "os"

// Vars are the environment variables the CLI and the SDK read.
var Vars = []string{"SDO_TOKEN", "SDO_BASE_URL", "SDO_ASYNC_BASE_URL", "SDO_TIMEOUT", "SDO_CONFIG_FILE"}

// Clear unsets every variable in Vars. Call it from TestMain, before m.Run.
func Clear() {
	for _, k := range Vars {
		os.Unsetenv(k)
	}
}
