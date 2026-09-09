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

// TestSensorSpecs_StrongTyping 统一后 specs 为强类型：已知键类型错误在解析期即被拒绝。
func TestSensorSpecs_StrongTyping(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "合法数值量程", raw: `{"min":-40,"max":125,"step":0.1}`, wantErr: false},
		{name: "整数形式数字", raw: `{"min":1,"max":10}`, wantErr: false},
		{name: "枚举 values", raw: `{"values":["auto","manual"]}`, wantErr: false},
		{name: "maxLen 整数", raw: `{"maxLen":32}`, wantErr: false},
		{name: "阈值 + 扩展键", raw: `{"thresholds":{"min":0,"max":100,"alarm":true},"gpio":4}`, wantErr: false},
		{name: "min 传字符串", raw: `{"min":"abc"}`, wantErr: true},
		{name: "step 传数组", raw: `{"step":[1]}`, wantErr: true},
		{name: "values 元素非字符串", raw: `{"values":[1,2]}`, wantErr: true},
		{name: "maxLen 传小数", raw: `{"maxLen":3.5}`, wantErr: true},
		{name: "thresholds 非对象", raw: `{"thresholds":"high"}`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var specs SensorSpecs
			err := json.Unmarshal([]byte(tc.raw), &specs)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestSensorSpecs_ExtrasFlatMarshal 扩展键（原 attrs 能力）平铺进 specs，往返无损。
func TestSensorSpecs_ExtrasFlatMarshal(t *testing.T) {
	specs := SensorSpecs{
		Min:  fp(-40),
		Max:  fp(125),
		Step: fp(0.1),
		Thresholds: &SpecsThresholds{
			Min:   fp(0),
			Max:   fp(100),
			Extra: map[string]any{"alarm": true},
		},
		Extra: map[string]any{"gpio": 4, "driver": "dht22"},
	}
	raw, err := json.Marshal(specs)
	assert.NoError(t, err)

	// 扩展键平铺在顶层、阈值内置为对象
	var obj map[string]any
	assert.NoError(t, json.Unmarshal(raw, &obj))
	assert.Equal(t, 4.0, obj["gpio"])
	assert.Equal(t, "dht22", obj["driver"])
	assert.Equal(t, 125.0, obj["max"])
	th, ok := obj["thresholds"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, true, th["alarm"])

	// 往返：反序列化后复序列化结果一致（无信息丢失）
	var back SensorSpecs
	assert.NoError(t, json.Unmarshal(raw, &back))
	raw2, err := json.Marshal(back)
	assert.NoError(t, err)
	assert.JSONEq(t, string(raw), string(raw2))
}

// TestSensorSpecs_EmptyOmitted 空定义序列化为 {}（落库层再归一化为空串），整对象省略。
func TestSensorSpecs_EmptyOmitted(t *testing.T) {
	raw, err := json.Marshal(SensorSpecs{})
	assert.NoError(t, err)
	assert.Equal(t, `{}`, string(raw))

	// Sensor DTO：空 specs 字段整体省略（okens 裁剪）
	dto, err := json.Marshal(Sensor{ID: "temperature", Name: "温度", DataType: "float"})
	assert.NoError(t, err)
	assert.NotContains(t, string(dto), "specs")
	assert.NotContains(t, string(dto), "reportInterval")
}

// TestSensorSpecs_Validate 范围约束校验
func TestSensorSpecs_Validate(t *testing.T) {
	low, high := 0.0, 100.0
	assert.NoError(t, (&SensorSpecs{Min: &low, Max: &high}).Validate())
	assert.Error(t, (&SensorSpecs{Min: &high, Max: &low}).Validate()) // min > max
	assert.Error(t, (&SensorSpecs{Step: fp(-1)}).Validate())
	assert.Error(t, (&SensorSpecs{MaxLen: ip(0)}).Validate())
	assert.Error(t, (&SensorSpecs{
		Min:        &low,
		Max:        &high,
		Thresholds: &SpecsThresholds{Min: fp(120)}, // 阈值下限超出量程
	}).Validate())
}

// TestValidateSensorValue 上报值类型与 dataType 对齐校验
func TestValidateSensorValue(t *testing.T) {
	assert.NoError(t, ValidateSensorValue("float", 25.5, nil))
	assert.NoError(t, ValidateSensorValue("int", 3, nil))
	assert.Error(t, ValidateSensorValue("float", "400ppm", nil)) // 类型错乱
	assert.NoError(t, ValidateSensorValue("bool", true, nil))
	assert.Error(t, ValidateSensorValue("bool", 1, nil))
	assert.NoError(t, ValidateSensorValue("text", "ok", nil))
	assert.Error(t, ValidateSensorValue("text", 123, nil))

	enumSpecs := &SensorSpecs{Values: []string{"auto", "manual"}}
	assert.NoError(t, ValidateSensorValue("enum", "auto", enumSpecs))
	assert.Error(t, ValidateSensorValue("enum", "turbo", enumSpecs)) // 不在 values 内
	assert.NoError(t, ValidateSensorValue("enum", "anything", nil))  // 未定义 values 不限制
}

// TestSensorUpdateRequest_ReportIntervalNull 传 null 恢复继承全局周期，非法值拒绝。
func TestSensorUpdateRequest_ReportIntervalNull(t *testing.T) {
	nullRaw := json.RawMessage(`null`)
	req := SensorUpdateRequest{ReportInterval: &nullRaw}
	assert.NoError(t, req.Validate())

	zeroRaw := json.RawMessage(`0`)
	err := (SensorUpdateRequest{ReportInterval: &zeroRaw}).Validate()
	assert.Error(t, err)

	negRaw := json.RawMessage(`-5`)
	err = (SensorUpdateRequest{ReportInterval: &negRaw}).Validate()
	assert.Error(t, err)
}

// fp / ip 指针构造助手
func fp(v float64) *float64 { return &v }
func ip(v int) *int         { return &v }
