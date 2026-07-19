package githubintegration

import (
	"os"
	"testing"

	"github.com/bradleymackey/track-slash/internal/testutil"
)

func TestMain(m *testing.M) {
	os.Exit(testutil.RunWithMigratedTemplate(m))
}
