package api

import (
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseNetworks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{name: "empty means default", value: "", want: []string{DefaultNetworkName}},
		{name: "whitespace only means default", value: "  ,  ,", want: []string{DefaultNetworkName}},
		{name: "single", value: "my-network", want: []string{"my-network"}},
		{name: "sorted", value: "frontend,backend", want: []string{"backend", "frontend"}},
		{name: "trims spaces", value: " a , b ", want: []string{"a", "b"}},
		{name: "deduplicates", value: "a,b,a", want: []string{"a", "b"}},
		{name: "explicit default is kept alongside others", value: "backend,default",
			want: []string{"backend", "default"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ParseNetworks(tt.value))
		})
	}
}

func TestServiceContainerNetworks(t *testing.T) {
	t.Parallel()

	newContainer := func(labels map[string]string) ServiceContainer {
		c := ServiceContainer{}
		c.ContainerJSONBase = &container.ContainerJSONBase{}
		c.Config = &container.Config{Labels: labels}
		return c
	}

	// A container without the label belongs to the implicit default network, which keeps pre-existing
	// containers working after an upgrade.
	unlabelled := newContainer(map[string]string{})
	assert.Equal(t, []string{DefaultNetworkName}, unlabelled.Networks())

	labelled := newContainer(map[string]string{LabelNetworks: "frontend,backend"})
	assert.Equal(t, []string{"backend", "frontend"}, labelled.Networks())
}

func TestValidateUserLabels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		labels    map[string]string
		wantErr   bool
		errSubstr string
	}{
		{name: "nil", labels: nil},
		{name: "empty", labels: map[string]string{}},
		{name: "ordinary", labels: map[string]string{"com.example.team": "platform"}},
		{
			name:      "reserved uncloud namespace",
			labels:    map[string]string{LabelServiceName: "hijacked"},
			wantErr:   true,
			errSubstr: "reserved namespace",
		},
		{
			name:      "reserved networks label",
			labels:    map[string]string{LabelNetworks: "secret"},
			wantErr:   true,
			errSubstr: "reserved namespace",
		},
		{
			name:      "reserved daemon namespace",
			labels:    map[string]string{LabelDaemonManaged: ""},
			wantErr:   true,
			errSubstr: "reserved namespace",
		},
		{
			name:      "empty name",
			labels:    map[string]string{"": "value"},
			wantErr:   true,
			errSubstr: "cannot be empty",
		},
		{
			// "uncloudish" does not start with "uncloud." so it is not reserved.
			name:   "similar but not reserved",
			labels: map[string]string{"uncloudish.thing": "ok"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateUserLabels(tt.labels)
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errSubstr)
		})
	}
}

func TestServiceSpecNetworksSetDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{name: "nil stays nil", in: nil, want: nil},
		{name: "sole default is normalised to nil", in: []string{DefaultNetworkName}, want: nil},
		{name: "sorted and deduplicated", in: []string{"b", "a", "b"}, want: []string{"a", "b"}},
		{name: "default kept alongside others", in: []string{"backend", "default"},
			want: []string{"backend", "default"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			spec := ServiceSpec{
				Name:      "test",
				Container: ContainerSpec{Image: "nginx:latest"},
				Networks:  tt.in,
			}
			assert.Equal(t, tt.want, spec.SetDefaults().Networks)
		})
	}
}

func TestServiceSpecNetworksValidate(t *testing.T) {
	t.Parallel()

	spec := ServiceSpec{Name: "test", Container: ContainerSpec{Image: "nginx:latest"}}

	// Compose network names are not DNS names, so underscores and uppercase letters are valid.
	for _, valid := range [][]string{
		{"my-network", "backend"},
		{"my_network"},
		{"My_Network.v2"},
		{"net1"},
	} {
		spec.Networks = valid
		require.NoError(t, spec.Validate(), "networks %v must be valid", valid)
	}

	for _, invalid := range [][]string{
		{"Not Valid"},
		{"-leading-dash"},
		{"_leading_underscore"},
		{"has,comma"},
		{""},
	} {
		spec.Networks = invalid
		require.ErrorContains(t, spec.Validate(), "invalid network name", "networks %v must be rejected", invalid)
	}

	spec.Networks = []string{strings.Repeat("a", 64)}
	require.ErrorContains(t, spec.Validate(), "too long")
}

func TestContainerSpecCloneLabels(t *testing.T) {
	t.Parallel()

	orig := ContainerSpec{
		Image:  "nginx:latest",
		Labels: map[string]string{"com.example.team": "platform"},
	}
	clone := orig.Clone()
	clone.Labels["com.example.team"] = "mutated"

	// Mutating the clone must not affect the original.
	assert.Equal(t, "platform", orig.Labels["com.example.team"])
}

func TestContainerSpecEqualsLabels(t *testing.T) {
	t.Parallel()

	a := ContainerSpec{Image: "nginx:latest", Labels: map[string]string{"a": "1"}}
	b := ContainerSpec{Image: "nginx:latest", Labels: map[string]string{"a": "1"}}
	assert.True(t, a.Equals(b))

	b.Labels = map[string]string{"a": "2"}
	assert.False(t, a.Equals(b), "a label value change must be detected")

	b.Labels = map[string]string{"a": "1", "b": "2"}
	assert.False(t, a.Equals(b), "an added label must be detected")

	// An empty map and a nil map describe the same desired state.
	empty := ContainerSpec{Image: "nginx:latest", Labels: map[string]string{}}
	assert.True(t, empty.Equals(ContainerSpec{Image: "nginx:latest"}))
}
