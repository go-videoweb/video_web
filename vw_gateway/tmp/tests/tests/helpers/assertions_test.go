package helpers

import "testing"

type emptyStruct struct{}

type nestedEmptyStruct struct {
	Inner map[string]any
}

type nonEmptyStruct struct {
	Name string
}

func TestIsStructurallyEmpty(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  bool
	}{
		{
			name:  "nil 视为空",
			value: nil,
			want:  true,
		},
		{
			name:  "空 map 视为空",
			value: map[string]any{},
			want:  true,
		},
		{
			name:  "所有子项都为空的 map 视为空",
			value: map[string]any{"model": map[string]any{}},
			want:  true,
		},
		{
			name:  "空结构体视为空",
			value: emptyStruct{},
			want:  true,
		},
		{
			name:  "字段递归为空的结构体视为空",
			value: nestedEmptyStruct{Inner: map[string]any{}},
			want:  true,
		},
		{
			name:  "非空字符串字段的结构体不为空",
			value: nonEmptyStruct{Name: "PID"},
			want:  false,
		},
		{
			name:  "包含非空子项的 map 不为空",
			value: map[string]any{"model": map[string]any{"graphics": 1}},
			want:  false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := IsStructurallyEmpty(tt.value)
			if got != tt.want {
				t.Fatalf("IsStructurallyEmpty(%#v) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}
