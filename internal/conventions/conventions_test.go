package conventions

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConventions(t *testing.T) {
	assert.Equal(t, "ephematool.io/", K8sLabelPrefix)
	assert.Equal(t, "ephematool.env.", OTelAttrPrefix)
	assert.Equal(t, "ephematool.io/env-id", LabelEnvID)
	assert.Equal(t, "ephematool.env.id", AttrEnvID)
}
