package apimeta

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func BenchmarkApplyAPIPatch(b *testing.B) {
	base, err := os.ReadFile("../../api/ags/v20250920/api.json")
	if err != nil {
		b.Fatal(err)
	}
	for _, count := range []int{50, 200, 800} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			ops := make([]map[string]any, count)
			for i := range ops {
				ops[i] = map[string]any{"op": "add", "path": fmt.Sprintf("/objects/Benchmark%d", i), "value": map[string]any{"type": "object", "members": []map[string]any{{"name": "Value", "type": "string", "member": "string"}}}}
			}
			patch, err := json.Marshal(ops)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := ApplyAPIPatch(base, patch); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkLoadPreviewContract(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if _, err := LoadContract("../../api/ags/v20250920", Preview); err != nil {
			b.Fatal(err)
		}
	}
}
