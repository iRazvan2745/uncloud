package compose

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/psviderski/uncloud/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeComposeFile writes the given Compose YAML to a temporary file and returns its path.
func writeComposeFile(t *testing.T, yaml string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "compose.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))

	return path
}

func TestServiceSpecFromCompose_Networks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		composeYAML string
		want        map[string][]string
	}{
		{
			name: "no networks declared means implicit default",
			composeYAML: `services:
  app:
    image: myapp:latest
`,
			want: map[string][]string{"app": nil},
		},
		{
			name: "explicit sole default is equivalent to declaring nothing",
			composeYAML: `services:
  app:
    image: myapp:latest
    networks:
      - default
networks:
  default:
`,
			want: map[string][]string{"app": nil},
		},
		{
			name: "shared custom network",
			composeYAML: `services:
  s1:
    image: myapp:latest
    networks:
      - my-network
  s2:
    image: myapp:latest
    networks:
      - my-network
  s3:
    image: myapp:latest
networks:
  my-network:
`,
			want: map[string][]string{
				"s1": {"my-network"},
				"s2": {"my-network"},
				"s3": nil,
			},
		},
		{
			name: "multiple networks are sorted",
			composeYAML: `services:
  app:
    image: myapp:latest
    networks:
      - frontend
      - backend
networks:
  frontend:
  backend:
`,
			want: map[string][]string{"app": {"backend", "frontend"}},
		},
		{
			name: "custom network alongside default is preserved",
			composeYAML: `services:
  app:
    image: myapp:latest
    networks:
      - default
      - backend
networks:
  backend:
`,
			want: map[string][]string{"app": {"backend", "default"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			project, err := LoadProject(context.Background(), []string{writeComposeFile(t, tt.composeYAML)})
			require.NoError(t, err)

			for name, want := range tt.want {
				spec, err := ServiceSpecFromCompose(project, name)
				require.NoError(t, err)
				assert.Equal(t, want, spec.Networks, "service %q", name)

				// The membership must survive normalisation so it isn't lost on deploy.
				assert.Equal(t, want, spec.SetDefaults().Networks, "service %q after SetDefaults", name)
				require.NoError(t, spec.Validate(), "service %q", name)
			}
		})
	}
}

func TestServiceSpecFromCompose_Labels(t *testing.T) {
	t.Parallel()

	composeYAML := `services:
  app:
    image: myapp:latest
    labels:
      com.example.team: platform
      com.example.tier: "1"
  bare:
    image: myapp:latest
`

	project, err := LoadProject(context.Background(), []string{writeComposeFile(t, composeYAML)})
	require.NoError(t, err)

	spec, err := ServiceSpecFromCompose(project, "app")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"com.example.team": "platform",
		"com.example.tier": "1",
	}, spec.Container.Labels)
	require.NoError(t, spec.Validate())

	// Compose adds project metadata to CustomLabels. Those must not leak into the spec, otherwise moving the
	// Compose file would recreate the container.
	bare, err := ServiceSpecFromCompose(project, "bare")
	require.NoError(t, err)
	assert.Nil(t, bare.Container.Labels)
}

func TestServiceSpecFromCompose_ReservedLabelRejected(t *testing.T) {
	t.Parallel()

	composeYAML := `services:
  app:
    image: myapp:latest
    labels:
      uncloud.service.name: hijacked
`

	project, err := LoadProject(context.Background(), []string{writeComposeFile(t, composeYAML)})
	require.NoError(t, err)

	spec, err := ServiceSpecFromCompose(project, "app")
	require.NoError(t, err)

	err = spec.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reserved namespace")
	assert.Contains(t, err.Error(), api.LabelServiceName)
}
