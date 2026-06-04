package tooltags

import (
	"strings"

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
