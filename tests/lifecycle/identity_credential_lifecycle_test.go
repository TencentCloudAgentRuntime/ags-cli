package lifecycle

import (
	"context"
	"fmt"

	"github.com/TencentCloudAgentRuntime/ags-cli/tests/testutil"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Identity + Credential real E2E lifecycle", func() {
	var cli *testutil.CLI

	BeforeEach(func() {
		cli = newCLI()
		cli.InitConfig()
	})

	It("covers identity/provider/secret lifecycle with direct and stdin secret inputs", func() {
		ctx := context.Background()
		identityName := uniqueName("ags-cli-e2e-identity")
		updatedIdentityName := identityName + "-updated"
		providerName := uniqueName("ags-cli-e2e-provider")
		userID := uniqueName("ags-cli-user")
		directSecret := "direct-secret-value"
		stdinSecret := "stdin-secret-value"

		var identityID string
		var providerID string

		DeferCleanup(func() {
			if providerID != "" {
				cli.Run(ctx, "--output", "json", "credential", "provider", "update", providerID, "--status", "DISABLED")
				cli.Run(ctx, "--output", "json", "credential", "provider", "delete", providerID)
			}
			if identityID != "" {
				cli.Run(ctx, "--output", "json", "identity", "delete", identityID)
			}
		})

		By("identity create -> get -> list --identity-ids -> update -> token create")
		createIdentity := cli.Run(ctx, "--output", "json", "identity", "create", "--name", identityName)
		createIdentity.ExpectSuccess()
		createIdentityEnv := createIdentity.Envelope()
		Expect(createIdentityEnv.Status).To(Equal("succeeded"), createIdentity.Diagnostics())
		identityID = stringField(createIdentityEnv.Data, "WorkloadIdentityId")
		Expect(identityID).NotTo(BeEmpty())

		getIdentity := cli.Run(ctx, "--output", "json", "identity", "get", identityID)
		getIdentity.ExpectSuccess()
		getIdentityEnv := getIdentity.Envelope()
		Expect(stringField(getIdentityEnv.Data, "WorkloadIdentityId")).To(Equal(identityID))
		Expect(stringField(getIdentityEnv.Data, "Name")).To(Equal(identityName))

		listIdentity := cli.Run(ctx, "--output", "json", "identity", "list", "--identity-ids", identityID, "--limit", "10")
		listIdentity.ExpectSuccess()
		listIdentityEnv := listIdentity.Envelope()
		identitySet := anySliceField(listIdentityEnv.Data, "WorkloadIdentitySet")
		Expect(identitySet).NotTo(BeEmpty(), listIdentity.Diagnostics())
		Expect(hasItemWithField(identitySet, "WorkloadIdentityId", identityID)).To(BeTrue(), listIdentity.Diagnostics())

		updateIdentity := cli.Run(ctx, "--output", "json", "identity", "update", identityID, "--name", updatedIdentityName)
		updateIdentity.ExpectSuccess()

		getUpdatedIdentity := cli.Run(ctx, "--output", "json", "identity", "get", identityID)
		getUpdatedIdentity.ExpectSuccess()
		Expect(stringField(getUpdatedIdentity.Envelope().Data, "Name")).To(Equal(updatedIdentityName))

		createToken := cli.Run(ctx, "--output", "json", "identity", "token", "create", "--identity-id", identityID, "--user-id", userID)
		createToken.ExpectSuccess()
		createTokenEnv := createToken.Envelope()
		token := stringField(createTokenEnv.Data, "WorkloadAccessToken")
		Expect(token).NotTo(BeEmpty(), createToken.Diagnostics())

		By("credential provider create SecretMultiUser -> get -> list --provider-ids -> update --status DISABLED")
		createProvider := cli.Run(ctx, "--output", "json", "credential", "provider", "create", "--name", providerName, "--type", "SecretMultiUser")
		createProvider.ExpectSuccess()
		createProviderEnv := createProvider.Envelope()
		providerID = stringField(createProviderEnv.Data, "ProviderId")
		Expect(providerID).NotTo(BeEmpty())

		getProvider := cli.Run(ctx, "--output", "json", "credential", "provider", "get", providerID)
		getProvider.ExpectSuccess()
		getProviderEnv := getProvider.Envelope()
		Expect(stringField(getProviderEnv.Data, "ProviderId")).To(Equal(providerID))
		Expect(stringField(getProviderEnv.Data, "Name")).To(Equal(providerName))
		Expect(stringField(getProviderEnv.Data, "Type")).To(Equal("SecretMultiUser"))

		listProvider := cli.Run(ctx, "--output", "json", "credential", "provider", "list", "--provider-ids", providerID, "--limit", "10")
		listProvider.ExpectSuccess()
		providerSet := anySliceField(listProvider.Envelope().Data, "ProviderSet")
		Expect(providerSet).NotTo(BeEmpty(), listProvider.Diagnostics())
		Expect(hasItemWithField(providerSet, "ProviderId", providerID)).To(BeTrue(), listProvider.Diagnostics())

		By("credential provider update -> verify status is DISABLED")
		updateProvider := cli.Run(ctx, "--output", "json", "credential", "provider", "update", providerID, "--status", "DISABLED")
		updateProvider.ExpectSuccess()

		getDisabledProvider := cli.Run(ctx, "--output", "json", "credential", "provider", "get", providerID)
		getDisabledProvider.ExpectSuccess()
		Expect(stringField(getDisabledProvider.Envelope().Data, "Status")).To(Equal("DISABLED"), getDisabledProvider.Diagnostics())

		// Re-enable for secret operations
		reEnableProvider := cli.Run(ctx, "--output", "json", "credential", "provider", "update", providerID, "--status", "ACTIVE")
		reEnableProvider.ExpectSuccess()

		By("secret set/list/get/delete with both --secret and --from-stdin")
		setDirect := cli.Run(ctx, "--output", "json", "credential", "secret", "set",
			"--credential-provider-id", providerID,
			"--user-id", userID,
			"--secret", directSecret,
			"--scope", "scope-direct",
			"--overwrite-allowed",
		)
		setDirect.ExpectSuccess()

		setStdin := cli.RunWithInput(ctx, stdinSecret,
			"--output", "json", "credential", "secret", "set",
			"--credential-provider-id", providerID,
			"--user-id", userID,
			"--from-stdin",
			"--scope", "scope-stdin",
			"--overwrite-allowed",
		)
		setStdin.ExpectSuccess()

		By("--secret and --from-stdin are mutually exclusive")
		setConflict := cli.RunWithInput(ctx, "conflict-val",
			"--output", "json", "credential", "secret", "set",
			"--credential-provider-id", providerID,
			"--user-id", userID,
			"--secret", "inline-val",
			"--from-stdin",
			"--scope", "scope-conflict",
		)
		Expect(setConflict.ExitCode).NotTo(Equal(0), "expected mutual exclusion error for --secret + --from-stdin")

		listSecrets := cli.Run(ctx, "--output", "json", "credential", "secret", "list",
			"--credential-provider-id", providerID,
			"--user-ids", userID,
			"--limit", "20",
		)
		listSecrets.ExpectSuccess()
		secretSet := anySliceField(listSecrets.Envelope().Data, "ManagedSecretSet")
		Expect(secretSet).To(HaveLen(2), listSecrets.Diagnostics())
		Expect(hasItemWithField(secretSet, "Scope", "scope-direct")).To(BeTrue(), listSecrets.Diagnostics())
		Expect(hasItemWithField(secretSet, "Scope", "scope-stdin")).To(BeTrue(), listSecrets.Diagnostics())

		getDirect := cli.Run(ctx, "--output", "json", "credential", "secret", "get",
			"--credential-provider-id", providerID,
			"--token", token,
			"--scope", "scope-direct",
		)
		getDirect.ExpectSuccess()
		Expect(stringField(getDirect.Envelope().Data, "Secret")).To(Equal(directSecret), getDirect.Diagnostics())

		getStdin := cli.Run(ctx, "--output", "json", "credential", "secret", "get",
			"--credential-provider-id", providerID,
			"--token", token,
			"--scope", "scope-stdin",
		)
		getStdin.ExpectSuccess()
		Expect(stringField(getStdin.Envelope().Data, "Secret")).To(Equal(stdinSecret), getStdin.Diagnostics())

		deleteDirect := cli.Run(ctx, "--output", "json", "credential", "secret", "delete",
			"--credential-provider-id", providerID,
			"--user-id", userID,
			"--scope", "scope-direct",
		)
		deleteDirect.ExpectSuccess()

		deleteStdin := cli.Run(ctx, "--output", "json", "credential", "secret", "delete",
			"--credential-provider-id", providerID,
			"--user-id", userID,
			"--scope", "scope-stdin",
		)
		deleteStdin.ExpectSuccess()

		By("secret list after deletion returns empty set")
		listSecretsAfterDelete := cli.Run(ctx, "--output", "json", "credential", "secret", "list",
			"--credential-provider-id", providerID,
			"--user-ids", userID,
			"--limit", "20",
		)
		listSecretsAfterDelete.ExpectSuccess()
		secretSetAfterDelete := anySliceField(listSecretsAfterDelete.Envelope().Data, "ManagedSecretSet")
		Expect(secretSetAfterDelete).To(BeEmpty(), listSecretsAfterDelete.Diagnostics())

		By("provider delete and identity delete, then verify get/list behavior after deletion")
		disableProvider2 := cli.Run(ctx, "--output", "json", "credential", "provider", "update", providerID, "--status", "DISABLED")
		disableProvider2.ExpectSuccess()

		deletedProviderID := providerID
		deleteProvider := cli.Run(ctx, "--output", "json", "credential", "provider", "delete", providerID)
		deleteProvider.ExpectSuccess()
		providerID = ""

		missingProvider := cli.Run(ctx, "--output", "json", "credential", "provider", "get", deletedProviderID)
		Expect(missingProvider.ExitCode).NotTo(Equal(0), missingProvider.Diagnostics())
		missingProviderEnv := missingProvider.Envelope()
		Expect(missingProviderEnv.Status).To(Equal("failed"))
		Expect(missingProviderEnv.Failure).NotTo(BeNil())
		Expect(missingProviderEnv.Failure.Kind).To(Equal("not_found"))

		providerListAfterDelete := cli.Run(ctx, "--output", "json", "credential", "provider", "list", "--provider-ids", deletedProviderID, "--limit", "10")
		providerListAfterDelete.ExpectSuccess()
		Expect(anySliceField(providerListAfterDelete.Envelope().Data, "ProviderSet")).To(BeEmpty(), providerListAfterDelete.Diagnostics())

		deletedIdentityID := identityID
		deleteIdentity := cli.Run(ctx, "--output", "json", "identity", "delete", identityID)
		deleteIdentity.ExpectSuccess()
		identityID = ""

		missingIdentity := cli.Run(ctx, "--output", "json", "identity", "get", deletedIdentityID)
		Expect(missingIdentity.ExitCode).NotTo(Equal(0), missingIdentity.Diagnostics())
		missingIdentityEnv := missingIdentity.Envelope()
		Expect(missingIdentityEnv.Status).To(Equal("failed"))
		Expect(missingIdentityEnv.Failure).NotTo(BeNil())
		Expect(missingIdentityEnv.Failure.Kind).To(Equal("not_found"))

		identityListAfterDelete := cli.Run(ctx, "--output", "json", "identity", "list", "--identity-ids", deletedIdentityID, "--limit", "10")
		identityListAfterDelete.ExpectSuccess()
		Expect(anySliceField(identityListAfterDelete.Envelope().Data, "WorkloadIdentitySet")).To(BeEmpty(), identityListAfterDelete.Diagnostics())
	})
})

func anySliceField(data map[string]any, key string) []any {
	value, ok := data[key]
	if !ok {
		return nil
	}
	set, _ := value.([]any)
	return set
}

func hasItemWithField(items []any, key, expected string) bool {
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if fmt.Sprintf("%v", m[key]) == expected {
			return true
		}
	}
	return false
}
