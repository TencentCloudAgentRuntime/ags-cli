package tooltags

import (
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

const internalTagPrefix = "qcs"

// FilterInheritedTags drops platform-internal tags before cloning a source tool.
func FilterInheritedTags(tags []*ags.Tag) []*ags.Tag {
	if len(tags) == 0 {
		return nil
	}
	filtered := make([]*ags.Tag, 0, len(tags))
	for _, tag := range tags {
		if tag != nil && tag.Key != nil && strings.HasPrefix(*tag.Key, internalTagPrefix) {
			continue
		}
		filtered = append(filtered, tag)
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

// FilterInheritedValue keeps the complete tag objects while dropping platform tags.
func FilterInheritedValue(value any) []map[string]any {
	wrapper, _ := apivalue.Decode(map[string]any{"Tags": value})
	var result []map[string]any
	for _, tag := range wrapper.Objects("Tags") {
		if !strings.HasPrefix(tag.String("Key"), internalTagPrefix) {
			result = append(result, map[string]any(tag))
		}
	}
	return result
}
