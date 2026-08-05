package credential_test

import (
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/tests/testutil"
	. "github.com/onsi/ginkgo/v2"
)

func TestCredential(t *testing.T) { testutil.RunSpecs(t, "AGR credential command E2E") }

var _ = BeforeSuite(testutil.SetupSuite)
var _ = AfterSuite(testutil.CleanupSuite)
