package controlplane

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

type sdkShape struct{ request, response reflect.Type }

var sdkShapes = map[string]sdkShape{
	"CreateDeployment":            {reflect.TypeFor[ags.CreateDeploymentRequest](), reflect.TypeFor[ags.CreateDeploymentResponseParams]()},
	"DeleteDeployment":            {reflect.TypeFor[ags.DeleteDeploymentRequest](), reflect.TypeFor[ags.DeleteDeploymentResponseParams]()},
	"DescribeDeployment":          {reflect.TypeFor[ags.DescribeDeploymentRequest](), reflect.TypeFor[ags.DescribeDeploymentResponseParams]()},
	"DescribeDeploymentList":      {reflect.TypeFor[ags.DescribeDeploymentListRequest](), reflect.TypeFor[ags.DescribeDeploymentListResponseParams]()},
	"ModifyDeployment":            {reflect.TypeFor[ags.ModifyDeploymentRequest](), reflect.TypeFor[ags.ModifyDeploymentResponseParams]()},
	"AcquireDeploymentToken":      {reflect.TypeFor[ags.AcquireDeploymentTokenRequest](), reflect.TypeFor[ags.AcquireDeploymentTokenResponseParams]()},
	"CreateAPIKey":                {reflect.TypeFor[ags.CreateAPIKeyRequest](), reflect.TypeFor[ags.CreateAPIKeyResponseParams]()},
	"DescribeAPIKeyList":          {reflect.TypeFor[ags.DescribeAPIKeyListRequest](), reflect.TypeFor[ags.DescribeAPIKeyListResponseParams]()},
	"DeleteAPIKey":                {reflect.TypeFor[ags.DeleteAPIKeyRequest](), reflect.TypeFor[ags.DeleteAPIKeyResponseParams]()},
	"CreateSandboxTool":           {reflect.TypeFor[ags.CreateSandboxToolRequest](), reflect.TypeFor[ags.CreateSandboxToolResponseParams]()},
	"DescribeSandboxToolList":     {reflect.TypeFor[ags.DescribeSandboxToolListRequest](), reflect.TypeFor[ags.DescribeSandboxToolListResponseParams]()},
	"UpdateSandboxTool":           {reflect.TypeFor[ags.UpdateSandboxToolRequest](), reflect.TypeFor[ags.UpdateSandboxToolResponseParams]()},
	"StartSandboxInstance":        {reflect.TypeFor[ags.StartSandboxInstanceRequest](), reflect.TypeFor[ags.StartSandboxInstanceResponseParams]()},
	"DescribeSandboxInstanceList": {reflect.TypeFor[ags.DescribeSandboxInstanceListRequest](), reflect.TypeFor[ags.DescribeSandboxInstanceListResponseParams]()},
	"UpdateSandboxInstance":       {reflect.TypeFor[ags.UpdateSandboxInstanceRequest](), reflect.TypeFor[ags.UpdateSandboxInstanceResponseParams]()},
	"PauseSandboxInstance":        {reflect.TypeFor[ags.PauseSandboxInstanceRequest](), reflect.TypeFor[ags.PauseSandboxInstanceResponseParams]()},
	"ResumeSandboxInstance":       {reflect.TypeFor[ags.ResumeSandboxInstanceRequest](), reflect.TypeFor[ags.ResumeSandboxInstanceResponseParams]()},
	"CreatePreCacheImageTask":     {reflect.TypeFor[ags.CreatePreCacheImageTaskRequest](), reflect.TypeFor[ags.CreatePreCacheImageTaskResponseParams]()},
	"DescribePreCacheImageTask":   {reflect.TypeFor[ags.DescribePreCacheImageTaskRequest](), reflect.TypeFor[ags.DescribePreCacheImageTaskResponseParams]()},
	"DeleteSandboxTool":           {reflect.TypeFor[ags.DeleteSandboxToolRequest](), reflect.TypeFor[ags.DeleteSandboxToolResponseParams]()},
	"StopSandboxInstance":         {reflect.TypeFor[ags.StopSandboxInstanceRequest](), reflect.TypeFor[ags.StopSandboxInstanceResponseParams]()},
	"AcquireSandboxInstanceToken": {reflect.TypeFor[ags.AcquireSandboxInstanceTokenRequest](), reflect.TypeFor[ags.AcquireSandboxInstanceTokenResponseParams]()},
}

// needsDynamic compares the entire active contract, including nested responses,
// before any SDK decoding. Promotion does not imply SDK compatibility.
func needsDynamic(spec *apimeta.Spec, action string) bool {
	shape, ok := sdkShapes[action]
	if !ok {
		return true
	}
	a, ok := spec.Actions[action]
	if !ok {
		return true
	}
	return !sdkObjectSupports(spec, a.Input, shape.request, map[string]bool{}) || !sdkObjectSupports(spec, a.Output, shape.response, map[string]bool{})
}

func sdkObjectSupports(spec *apimeta.Spec, name string, typ reflect.Type, seen map[string]bool) bool {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return false
	}
	key := name + "|" + typ.String()
	if seen[key] {
		return true
	}
	seen[key] = true
	object := spec.Object(name)
	if object == nil {
		return false
	}
	fields := map[string]reflect.Type{}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		key := strings.Split(f.Tag.Get("json"), ",")[0]
		if key != "" && key != "-" {
			fields[key] = f.Type
		}
	}
	for _, member := range object.Members {
		if member.Disabled {
			continue
		}
		field, ok := fields[member.Name]
		if !ok {
			return false
		}
		if !sdkValueSupports(spec, member.Type, member.Member, field, seen) {
			return false
		}
	}
	return true
}

func sdkValueSupports(spec *apimeta.Spec, kind, member string, typ reflect.Type, seen map[string]bool) bool {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	switch kind {
	case "object":
		return sdkObjectSupports(spec, member, typ, seen)
	case "list", "array":
		if typ.Kind() != reflect.Slice {
			return false
		}
		element := member
		if spec.Object(member) != nil {
			element = "object"
		}
		return sdkValueSupports(spec, element, member, typ.Elem(), seen)
	case "string", "binary":
		return typ.Kind() == reflect.String
	case "bool":
		return typ.Kind() == reflect.Bool
	case "int", "int64", "integer":
		return typ.Kind() == reflect.Int64
	case "uint", "uint64":
		return typ.Kind() == reflect.Uint64
	case "float", "double":
		return typ.Kind() == reflect.Float64
	}
	return false
}

func invalidContractRequest(action string, err error) error {
	return fmt.Errorf("%s request does not match %s contract: %w", action, apimeta.BuildChannel, err)
}
