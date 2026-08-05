package identity_test

import (
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/tests/testutil"
	. "github.com/onsi/ginkgo/v2"
)

func TestIdentity(t *testing.T) { testutil.RunSpecs(t, "AGR identity command E2E") }

var _ = BeforeSuite(testutil.SetupSuite)
var _ = AfterSuite(testutil.CleanupSuite)
