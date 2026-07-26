package mqtt

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTopicRequest_JSON(t *testing.T) {
	jsonBody := `{"topic":"iot/sensors/#"}`
	var req TopicRequest
	err := json.Unmarshal([]byte(jsonBody), &req)
	assert.NoError(t, err)
	assert.Equal(t, "iot/sensors/#", req.Topic)
}

func TestTopicRequest_Empty(t *testing.T) {
	var req TopicRequest
	assert.Empty(t, req.Topic)
}
