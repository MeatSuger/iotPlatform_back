package common

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDateTimeNow(t *testing.T) {
	now := DateTimeNow()
	assert.False(t, now.IsZero())
	assert.WithinDuration(t, time.Now(), now.Time, 2*time.Second)
}

func TestDateTimeFrom(t *testing.T) {
	tm := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	dt := DateTimeFrom(tm)
	assert.Equal(t, tm, dt.Time)
}

func TestDateTime_MarshalJSON(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	tm := time.Date(2024, 1, 15, 10, 30, 0, 0, loc)
	dt := DateTimeFrom(tm)

	b, err := json.Marshal(dt)
	assert.NoError(t, err)
	// Should contain date time with timezone offset
	assert.Contains(t, string(b), "2024-01-15")
	assert.Contains(t, string(b), "10:30:00")
}

func TestDateTime_MarshalJSON_Zero(t *testing.T) {
	dt := DateTime{}
	b, err := json.Marshal(dt)
	assert.NoError(t, err)
	assert.Equal(t, "null", string(b))
}

func TestDateTime_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"RFC 3339 with ms + zone", `"2024-01-15T10:30:00.000+08:00"`, false},
		{"RFC 3339 no ms", `"2024-01-15T10:30:00+08:00"`, false},
		{"ms no zone", `"2024-01-15T10:30:00.000"`, false},
		{"space with zone (legacy)", `"2024-01-15 10:30:00.000+08:00"`, false},
		{"space no ms (legacy)", `"2024-01-15 10:30:00+08:00"`, false},
		{"no timezone", `"2024-01-15 10:30:00"`, false},
		{"null", `null`, false},
		{"empty", `""`, false},
		{"space offset (legacy)", `"2024-01-15 10:30:00 +0800"`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var dt DateTime
			err := json.Unmarshal([]byte(tt.input), &dt)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDateTime_UnmarshalJSON_Invalid(t *testing.T) {
	var dt DateTime
	err := json.Unmarshal([]byte(`"invalid-date"`), &dt)
	assert.Error(t, err)
}

func TestDateTime_Value(t *testing.T) {
	// Non-zero
	tm := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	dt := DateTimeFrom(tm)
	v, err := dt.Value()
	assert.NoError(t, err)
	assert.Equal(t, tm, v)

	// Zero
	zero := DateTime{}
	v, err = zero.Value()
	assert.NoError(t, err)
	assert.Nil(t, v)
}

func TestDateTime_Scan(t *testing.T) {
	tests := []struct {
		name    string
		value   interface{}
		wantErr bool
	}{
		{"time.Time", time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC), false},
		{"nil", nil, false},
		{"string", "2024-01-15 10:30:00", false},
		{"[]byte", []byte("2024-01-15 10:30:00"), false},
		{"int (unsupported)", 12345, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var dt DateTime
			err := dt.Scan(tt.value)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDateTimeFormat(t *testing.T) {
	assert.Equal(t, "2006-01-02 15:04:05", DateTimeFormat)
	assert.Equal(t, "2006-01-02T15:04:05.000-07:00", DateTimeFormatWithZone)
}
