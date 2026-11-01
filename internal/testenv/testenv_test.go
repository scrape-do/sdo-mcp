// SPDX-License-Identifier: Apache-2.0

package testenv

import (
	"os"
	"testing"
)

func TestClearUnsetsEveryVar(t *testing.T) {
	for _, k := range Vars {
		t.Setenv(k, "hostile")
	}
	Clear()
	for _, k := range Vars {
		if _, set := os.LookupEnv(k); set {
			t.Errorf("%s is still set", k)
		}
	}
}
