package client

import (
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("newTencentCloudCredential", func() {
	AfterEach(func() {
		config.SetSecretID("")
		config.SetSecretKey("")
		config.SetToken("")
	})

	It("uses a token credential when STS token is configured", func() {
		config.SetSecretID("sid")
		config.SetSecretKey("skey")
		config.SetToken("session-token")

		cred := newTencentCloudCredential()

		Expect(cred.SecretId).To(Equal("sid"))
		Expect(cred.SecretKey).To(Equal("skey"))
		Expect(cred.Token).To(Equal("session-token"))
	})

	It("uses the legacy credential shape when token is absent", func() {
		config.SetSecretID("sid")
		config.SetSecretKey("skey")

		cred := newTencentCloudCredential()

		Expect(cred.SecretId).To(Equal("sid"))
		Expect(cred.SecretKey).To(Equal("skey"))
		Expect(cred.Token).To(BeEmpty())
	})
})
