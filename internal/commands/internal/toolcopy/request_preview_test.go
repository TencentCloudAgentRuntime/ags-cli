//go:build preview

package toolcopy

import "testing"

func TestRequestCanonicalizesVolumeReferences(t *testing.T) {
	request, err := Request(map[string]any{
		"StorageMounts": []any{
			map[string]any{
				"Name": "resolved", "MountPath": "/resolved",
				"Volume": map[string]any{"VolumeId": "vol-123", "VolumeName": "resolved-name"},
			},
			map[string]any{
				"Name": "named", "MountPath": "/named",
				"Volume": map[string]any{"VolumeName": "named-volume"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mounts, ok := request["StorageMounts"].([]map[string]any)
	if !ok || len(mounts) != 2 {
		t.Fatalf("StorageMounts = %#v", request["StorageMounts"])
	}
	resolved := mounts[0]["Volume"].(map[string]any)
	if resolved["VolumeId"] != "vol-123" {
		t.Fatalf("resolved VolumeId = %#v", resolved["VolumeId"])
	}
	if _, exists := resolved["VolumeName"]; exists {
		t.Fatalf("resolved reference retained VolumeName: %#v", resolved)
	}
	named := mounts[1]["Volume"].(map[string]any)
	if named["VolumeName"] != "named-volume" {
		t.Fatalf("named VolumeName = %#v", named["VolumeName"])
	}
	if _, exists := named["VolumeId"]; exists {
		t.Fatalf("named reference retained VolumeId: %#v", named)
	}
}
