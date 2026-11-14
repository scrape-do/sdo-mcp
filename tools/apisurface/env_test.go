// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"testing"

	"github.com/scrape-do/sdo-mcp/internal/testenv"
)

// TestMain clears the SDK's environment, so the developer's shell cannot
// change the surface these tests render.
func TestMain(m *testing.M) {
	testenv.Clear()
	os.Exit(m.Run())
}
