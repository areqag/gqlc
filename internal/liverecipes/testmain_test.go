package liverecipes

import (
	"testing"

	"github.com/areqag/gqlc/internal/testtmp"
)

// See internal/testtmp: without this redirect, this package's t.TempDir
// calls leave shared-/tmp records in the test log and its results never
// replay from go's test cache on CI.
func TestMain(m *testing.M) { testtmp.Main(m) }
