package apimeta

import "strings"

// WorkflowSpec describes the existing identity/credential command wire inputs.
// These interfaces predate the public API snapshot; keep their authority
// separate instead of editing the upstream snapshot or allowing unknown Actions.
func WorkflowSpec() *Spec {
	spec := &Spec{Actions: map[string]Action{}, Objects: map[string]*Object{
		"WorkflowTag":    {Members: []Member{{Name: "Key", Type: "string", Member: "string"}, {Name: "Value", Type: "string", Member: "string"}}},
		"WorkflowFilter": {Members: []Member{{Name: "Name", Type: "string", Member: "string"}, {Name: "Values", Type: "list", Member: "string"}}},
	}}
	requests := map[string]string{
		"CreateWorkloadIdentity":             "Name AllowedOAuth2ReturnUrls Tags",
		"UpdateWorkloadIdentity":             "WorkloadIdentityId Name AllowedOAuth2ReturnUrls Tags",
		"DeleteWorkloadIdentity":             "WorkloadIdentityId",
		"DescribeWorkloadIdentityList":       "WorkloadIdentityIds Limit Offset Filters",
		"CreateWorkloadAccessTokenForUserId": "WorkloadIdentityId UserId",
		"CreateCredentialProvider":           "Name Type Description Config Tags",
		"UpdateCredentialProvider":           "ProviderId Name Description Config Tags Status",
		"DeleteCredentialProvider":           "ProviderId",
		"DescribeCredentialProviderList":     "ProviderIds Limit Offset Filters",
		"SetManagedSecret":                   "CredentialProviderId UserId Secret Scope OverwriteAllowed Metadata",
		"GetManagedSecret":                   "CredentialProviderId WorkloadIdentityToken Scope",
		"DeleteManagedSecret":                "CredentialProviderId UserId Scope",
		"DescribeManagedSecretList":          "CredentialProviderId Limit Offset UserIds Filters",
		"AcquireOAuth2AccessToken":           "WorkloadIdentityToken CredentialProviderId OAuth2Flow Scopes OAuth2ReturnUrl CustomState ForceAuthentication SessionUri",
		"CompleteOAuth2AccessTokenAuth":      "SessionUri UserId",
	}
	for action, fields := range requests {
		request := action + "Request"
		obj := &Object{Name: request, Type: "object"}
		for name := range strings.FieldsSeq(fields) {
			kind, member := "string", "string"
			switch name {
			case "Limit", "Offset":
				kind, member = "int", "int"
			case "ForceAuthentication", "OverwriteAllowed":
				kind, member = "bool", "bool"
			case "Tags":
				kind, member = "list", "WorkflowTag"
			case "Filters":
				kind, member = "list", "WorkflowFilter"
			case "Metadata", "Config":
				kind, member = "list", "string_map" // Existing commands accept free string-keyed configuration maps.
			case "AllowedOAuth2ReturnUrls", "WorkloadIdentityIds", "ProviderIds", "UserIds", "Scopes":
				kind, member = "list", "string"
			}
			obj.Members = append(obj.Members, Member{Name: name, Type: kind, Member: member})
		}
		spec.Objects[request] = obj
		// Workflow responses intentionally retain an open JSON object. Their
		// resource-specific readers predate the public snapshot; do not infer a
		// closed official response schema or discard additional fields here.
		response := action + "Response"
		spec.Objects[response] = &Object{Name: response, Type: "object"}
		spec.Actions[action] = Action{Name: action, Input: request, Output: response}
	}
	return spec
}
