// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"os"
	"testing"

	"github.com/scrape-do/sdo-mcp/internal/testenv"
)

// TestMain clears the SDK's environment (token, base URLs, timeout), so a
// value exported in the developer's shell cannot change what these tests see.
func TestMain(m *testing.M) {
	testenv.Clear()
	os.Exit(m.Run())
}
