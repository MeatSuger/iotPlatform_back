package entity

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// 物模型 JSON 对象字段（specs/thresholds/attrs/config）校验语义：
// 不传=不更新；传 {} = 显式清空；null/非法 JSON = 拒绝。
func TestValidateJSONObject(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "空 JSON 对象（显式清空）", raw: `{}`, wantErr: false},
		{name: "普通对象", raw: `{"min":0,"max":100}`, wantErr: false},
		{name: "嵌套对象", raw: `{"alarm":{"level":1}}`, wantErr: false},
		{name: "null 视为非法", raw: `null`, wantErr: true},
		{name: "数组非法（需对象）", raw: `[1,2]`, wantErr: true},
		{name: "标量非法", raw: `123`, wantErr: true},
		{name: "非法 JSON", raw: `{"a":`, wantErr: true},
		{name: "空串为字段缺失", raw: "", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateJSONObject(json.RawMessage(tc.raw))
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// null 经 SensorUpdateRequest.Validate 链路也应被拒绝（AIP：`{}` 才是显式清空）。
func TestSensorUpdateRequest_RejectsNullSpecs(t *testing.T) {
	raw := json.RawMessage(`null`)
	req := SensorUpdateRequest{Specs: &raw}
	err := req.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "null")
}

func TestSensorUpdateRequest_AcceptsEmptyObjectSpecs(t *testing.T) {
	raw := json.RawMessage(`{}`)
	req := SensorUpdateRequest{Specs: &raw}
	err := req.Validate()
	assert.NoError(t, err)
}