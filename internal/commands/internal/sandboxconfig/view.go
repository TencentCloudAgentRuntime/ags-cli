// Package sandboxconfig renders the preview-only sandbox keepalive and log
// content-format configuration from the raw wire object. The original object
// stays authoritative: fields are shown only when the service returned them and
// no client-side default is added.
package sandboxconfig

import (
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
)

// Field is one configuration row in the text details view.
type Field struct {
	Name  string
	Value string
}

type member struct {
	name   string
	header string
}

var keepaliveMembers = []member{
	{name: "KeepaliveMode", header: "KEEPALIVE MODE"},
	{name: "KeepaliveTimeout", header: "KEEPALIVE TIMEOUT"},
	{name: "AutoPause", header: "AUTO PAUSE"},
	{name: "AutoResume", header: "AUTO RESUME"},
}

const logFormatName = "LogConfiguration.ContentFormat"

// Details returns the keepalive configuration rows that the response actually
// carried. tool adds the Tool log content format. Non-preview builds expose no
// keepalive view, so stable text output stays unchanged.
func Details(object apivalue.Object, tool bool) []Field {
	if apimeta.BuildChannel != apimeta.Preview || object == nil {
		return nil
	}
	var fields []Field
	for _, item := range keepaliveMembers {
		if value, ok := memberValue(object, item.name); ok && value != "" {
			fields = append(fields, Field{Name: item.name, Value: value})
		}
	}
	if tool {
		if value, ok := logFormatValue(object); ok && value != "" {
			fields = append(fields, Field{Name: logFormatName, Value: value})
		}
	}
	return fields
}

// Headers returns the text columns that preview table output appends.
func Headers(tool bool) []string {
	if apimeta.BuildChannel != apimeta.Preview {
		return nil
	}
	headers := make([]string, 0, len(keepaliveMembers)+1)
	for _, item := range keepaliveMembers {
		headers = append(headers, item.header)
	}
	if tool {
		headers = append(headers, "LOG FORMAT")
	}
	return headers
}

// Values returns one cell per Headers entry, in the same order. A member the
// response omitted renders as "-"; an explicit false or an empty value is kept
// as returned.
func Values(object apivalue.Object, tool bool) []string {
	if apimeta.BuildChannel != apimeta.Preview {
		return nil
	}
	values := make([]string, 0, len(keepaliveMembers)+1)
	for _, item := range keepaliveMembers {
		value, ok := memberValue(object, item.name)
		if !ok {
			value = "-"
		}
		values = append(values, value)
	}
	if tool {
		value, ok := logFormatValue(object)
		if !ok {
			value = "-"
		}
		values = append(values, value)
	}
	return values
}

func memberValue(object apivalue.Object, name string) (string, bool) {
	switch name {
	case "AutoPause", "AutoResume":
		if value, ok := object[name]; ok && value != nil {
			return object.Display(name), true
		}
		return "", false
	}
	keepalive := object.Object("KeepaliveConfiguration")
	if keepalive == nil {
		return "", false
	}
	value, ok := keepalive[name]
	if !ok || value == nil {
		return "", false
	}
	return keepalive.Display(name), true
}

func logFormatValue(object apivalue.Object) (string, bool) {
	logConfiguration := object.Object("LogConfiguration")
	if logConfiguration == nil {
		return "", false
	}
	value, ok := logConfiguration["ContentFormat"]
	if !ok || value == nil {
		return "", false
	}
	return logConfiguration.Display("ContentFormat"), true
}
