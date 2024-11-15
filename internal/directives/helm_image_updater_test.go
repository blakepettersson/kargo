package directives

import (
	"context"
	"fmt"
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	kargoapi "github.com/akuity/kargo/api/v1alpha1"
)

func Test_helmImageUpdater_validate(t *testing.T) {
	testCases := []struct {
		name             string
		config           Config
		expectedProblems []string
	}{
		{
			name:   "path is not specified",
			config: Config{},
			expectedProblems: []string{
				"(root): path is required",
			},
		},
		{
			name: "path is empty",
			config: Config{
				"path": "",
			},
			expectedProblems: []string{
				"path: String length must be greater than or equal to 1",
			},
		},
		{
			name:   "images is null",
			config: Config{},
			expectedProblems: []string{
				"(root): images is required",
			},
		},
		{
			name: "images is empty",
			config: Config{
				"images": []Config{},
			},
			expectedProblems: []string{
				"images: Array must have at least 1 items",
			},
		},
		{
			name: "key not specified",
			config: Config{
				"images": []Config{{}},
			},
			expectedProblems: []string{
				"images.0: key is required",
			},
		},
		{
			name: "key is empty",
			config: Config{
				"images": []Config{{
					"key": "",
				}},
			},
			expectedProblems: []string{
				"images.0.key: String length must be greater than or equal to 1",
			},
		},
		{
			name: "value not specified",
			config: Config{
				"images": []Config{{}},
			},
			expectedProblems: []string{
				"images.0: value is required",
			},
		},
		{
			name: "image and value both specified",
			config: Config{
				"images": []Config{{
					"image": "fake-image",
					"key":   "fake-key",
					"value": "fake-value",
				}},
			},
			expectedProblems: []string{
				"images.0: Must validate one and only one schema",
			},
		},
		{
			name: "valid kitchen sink",
			config: Config{
				"path": "fake-path",
				"images": []Config{
					{
						"image": "fake-image",
						"key":   "fake-key-0",
						"value": "ImageAndTag",
					},
					{
						"image": "fake-image",
						"key":   "fake-key-1",
						"value": "ImageAndTag",
						"fromOrigin": Config{
							"kind": Warehouse,
							"name": "fake-name",
						},
					},
					{
						"key":   "fake-key-2",
						"value": "fake-value",
					},
					{
						"image": "",
						"key":   "fake-key-3",
						"value": "fake-value",
					},
				},
			},
		},
	}

	r := newHelmImageUpdater()
	runner, ok := r.(*helmImageUpdater)
	require.True(t, ok)

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := runner.validate(testCase.config)
			if len(testCase.expectedProblems) == 0 {
				require.NoError(t, err)
			} else {
				for _, problem := range testCase.expectedProblems {
					require.ErrorContains(t, err, problem)
				}
			}
		})
	}
}

func Test_helmImageUpdater_runPromotionStep(t *testing.T) {
	tests := []struct {
		name       string
		objects    []client.Object
		stepCtx    *PromotionStepContext
		cfg        HelmUpdateImageConfig
		files      map[string]string
		assertions func(*testing.T, string, PromotionStepResult, error)
	}{
		{
			name: "successful run with image updates",
			objects: []client.Object{
				&kargoapi.Warehouse{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-warehouse",
						Namespace: "test-project",
					},
					Spec: kargoapi.WarehouseSpec{
						Subscriptions: []kargoapi.RepoSubscription{
							{
								Image: &kargoapi.ImageSubscription{
									RepoURL: "docker.io/library/nginx",
								},
							},
						},
					},
				},
			},
			stepCtx: &PromotionStepContext{
				Project: "test-project",
				Freight: kargoapi.FreightCollection{
					Freight: map[string]kargoapi.FreightReference{
						"Warehouse/test-warehouse": {
							Origin: kargoapi.FreightOrigin{Kind: "Warehouse", Name: "test-warehouse"},
							Images: []kargoapi.Image{
								{RepoURL: "docker.io/library/nginx", Tag: "1.19.0"},
							},
						},
					},
				},
				FreightRequests: []kargoapi.FreightRequest{
					{
						Origin: kargoapi.FreightOrigin{Kind: "Warehouse", Name: "test-warehouse"},
					},
				},
			},
			cfg: HelmUpdateImageConfig{
				Path: "values.yaml",
				Images: []HelmUpdateImageConfigImage{
					{Key: "image.tag", Image: "docker.io/library/nginx", Value: Tag},
				},
			},
			files: map[string]string{
				"values.yaml": "image:\n  tag: oldtag\n",
			},
			assertions: func(t *testing.T, workDir string, result PromotionStepResult, err error) {
				assert.NoError(t, err)
				assert.Equal(t, PromotionStepResult{
					Status: kargoapi.PromotionPhaseSucceeded,
					Output: map[string]any{
						"commitMessage": "Updated values.yaml\n\n- image.tag: \"1.19.0\"",
					},
				}, result)
				content, err := os.ReadFile(path.Join(workDir, "values.yaml"))
				require.NoError(t, err)
				assert.Contains(t, string(content), "tag: 1.19.0")
			},
		},
		{
			name: "no image updates",
			stepCtx: &PromotionStepContext{
				Project:         "test-project",
				Freight:         kargoapi.FreightCollection{},
				FreightRequests: []kargoapi.FreightRequest{},
			},
			cfg: HelmUpdateImageConfig{
				Path: "values.yaml",
				Images: []HelmUpdateImageConfigImage{
					{Key: "image.tag", Image: "docker.io/library/non-existent", Value: Tag},
				},
			},
			files: map[string]string{
				"values.yaml": "image:\n  tag: oldtag\n",
			},
			assertions: func(t *testing.T, _ string, _ PromotionStepResult, err error) {
				assert.ErrorContains(t, err, "not found in referenced Freight")
			},
		},

		{
			name: "failed to generate image updates",
			stepCtx: &PromotionStepContext{
				KargoClient: fake.NewClientBuilder().WithInterceptorFuncs(interceptor.Funcs{
					Get: func(
						context.Context,
						client.WithWatch,
						client.ObjectKey,
						client.Object,
						...client.GetOption,
					) error {
						return fmt.Errorf("something went wrong")
					},
				}).Build(),
				Project: "test-project",
				FreightRequests: []kargoapi.FreightRequest{
					{
						Origin: kargoapi.FreightOrigin{Kind: "Warehouse", Name: "non-existent-warehouse"},
					},
				},
			},
			cfg: HelmUpdateImageConfig{
				Path: "values.yaml",
				Images: []HelmUpdateImageConfigImage{
					{
						Key:   "image.tag",
						Image: "docker.io/library/nginx",
						Value: Tag,
					},
				},
			},
			assertions: func(t *testing.T, _ string, result PromotionStepResult, err error) {
				require.ErrorContains(t, err, "failed to generate image updates")
				require.Errorf(t, err, "something went wrong")
				assert.Equal(t, PromotionStepResult{Status: kargoapi.PromotionPhaseErrored}, result)
			},
		},
		{
			name: "failed to update values file",
			objects: []client.Object{
				&kargoapi.Warehouse{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-warehouse",
						Namespace: "test-project",
					},
					Spec: kargoapi.WarehouseSpec{
						Subscriptions: []kargoapi.RepoSubscription{
							{
								Image: &kargoapi.ImageSubscription{
									RepoURL: "docker.io/library/nginx",
								},
							},
						},
					},
				},
			},
			stepCtx: &PromotionStepContext{
				Project: "test-project",
				Freight: kargoapi.FreightCollection{
					Freight: map[string]kargoapi.FreightReference{
						"Warehouse/test-warehouse": {
							Origin: kargoapi.FreightOrigin{Kind: "Warehouse", Name: "test-warehouse"},
							Images: []kargoapi.Image{
								{RepoURL: "docker.io/library/nginx", Tag: "1.19.0"},
							},
						},
					},
				},
				FreightRequests: []kargoapi.FreightRequest{
					{
						Origin: kargoapi.FreightOrigin{Kind: "Warehouse", Name: "test-warehouse"},
					},
				},
			},
			cfg: HelmUpdateImageConfig{
				Path: "non-existent/values.yaml",
				Images: []HelmUpdateImageConfigImage{
					{Key: "image.tag", Image: "docker.io/library/nginx", Value: Tag},
				},
			},
			assertions: func(t *testing.T, _ string, result PromotionStepResult, err error) {
				assert.Error(t, err)
				assert.Equal(t, PromotionStepResult{Status: kargoapi.PromotionPhaseErrored}, result)
				assert.Contains(t, err.Error(), "values file update failed")
			},
		},
	}

	runner := &helmImageUpdater{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stepCtx := tt.stepCtx

			stepCtx.WorkDir = t.TempDir()
			for p, c := range tt.files {
				require.NoError(t, os.MkdirAll(path.Join(stepCtx.WorkDir, path.Dir(p)), 0o700))
				require.NoError(t, os.WriteFile(path.Join(stepCtx.WorkDir, p), []byte(c), 0o600))
			}

			if stepCtx.KargoClient == nil {
				scheme := runtime.NewScheme()
				require.NoError(t, kargoapi.AddToScheme(scheme))
				stepCtx.KargoClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.objects...).Build()
			}

			result, err := runner.runPromotionStep(context.Background(), stepCtx, tt.cfg)
			tt.assertions(t, stepCtx.WorkDir, result, err)
		})
	}
}

func Test_helmImageUpdater_generateImageUpdates(t *testing.T) {
	tests := []struct {
		name       string
		objects    []client.Object
		stepCtx    *PromotionStepContext
		cfg        HelmUpdateImageConfig
		assertions func(*testing.T, map[string]string, error)
	}{
		{
			name: "finds image update",
			objects: []client.Object{
				&kargoapi.Warehouse{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-warehouse",
						Namespace: "test-project",
					},
					Spec: kargoapi.WarehouseSpec{
						Subscriptions: []kargoapi.RepoSubscription{
							{
								Image: &kargoapi.ImageSubscription{
									RepoURL: "docker.io/library/nginx",
								},
							},
						},
					},
				},
			},
			stepCtx: &PromotionStepContext{
				Project: "test-project",
				Freight: kargoapi.FreightCollection{
					Freight: map[string]kargoapi.FreightReference{
						"Warehouse/test-warehouse": {
							Origin: kargoapi.FreightOrigin{Kind: "Warehouse", Name: "test-warehouse"},
							Images: []kargoapi.Image{
								{RepoURL: "docker.io/library/nginx", Tag: "1.19.0"},
							},
						},
					},
				},
				FreightRequests: []kargoapi.FreightRequest{
					{
						Origin: kargoapi.FreightOrigin{Kind: "Warehouse", Name: "test-warehouse"},
					},
				},
			},
			cfg: HelmUpdateImageConfig{
				Images: []HelmUpdateImageConfigImage{
					{Key: "image.tag", Image: "docker.io/library/nginx", Value: Tag},
				},
			},
			assertions: func(t *testing.T, changes map[string]string, err error) {
				assert.NoError(t, err)
				assert.Equal(t, map[string]string{"image.tag": "1.19.0"}, changes)
			},
		},
		{
			name: "image not found",
			stepCtx: &PromotionStepContext{
				Project:         "test-project",
				Freight:         kargoapi.FreightCollection{},
				FreightRequests: []kargoapi.FreightRequest{},
			},
			cfg: HelmUpdateImageConfig{
				Images: []HelmUpdateImageConfigImage{
					{Key: "image.tag", Image: "docker.io/library/non-existent", Value: Tag},
				},
			},
			assertions: func(t *testing.T, _ map[string]string, err error) {
				assert.ErrorContains(t, err, "not found in referenced Freight")
			},
		},
		{
			name: "multiple images, one not found",
			objects: []client.Object{
				&kargoapi.Warehouse{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-warehouse",
						Namespace: "test-project",
					},
					Spec: kargoapi.WarehouseSpec{
						Subscriptions: []kargoapi.RepoSubscription{
							{
								Image: &kargoapi.ImageSubscription{
									RepoURL: "docker.io/library/nginx",
								},
							},
						},
					},
				},
			},
			stepCtx: &PromotionStepContext{
				Project: "test-project",
				Freight: kargoapi.FreightCollection{
					Freight: map[string]kargoapi.FreightReference{
						"Warehouse/test-warehouse": {
							Origin: kargoapi.FreightOrigin{Kind: "Warehouse", Name: "test-warehouse"},
							Images: []kargoapi.Image{
								{RepoURL: "docker.io/library/nginx", Tag: "1.19.0"},
							},
						},
					},
				},
				FreightRequests: []kargoapi.FreightRequest{
					{
						Origin: kargoapi.FreightOrigin{Kind: "Warehouse", Name: "test-warehouse"},
					},
				},
			},
			cfg: HelmUpdateImageConfig{
				Images: []HelmUpdateImageConfigImage{
					{Key: "image1.tag", Image: "docker.io/library/nginx", Value: Tag},
					{Key: "image2.tag", Image: "docker.io/library/non-existent", Value: Tag},
				},
			},
			assertions: func(t *testing.T, _ map[string]string, err error) {
				assert.ErrorContains(t, err, "not found in referenced Freight")
			},
		},
		{
			name: "image with FromOrigin specified",
			objects: []client.Object{
				&kargoapi.Warehouse{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-warehouse",
						Namespace: "test-project",
					},
					Spec: kargoapi.WarehouseSpec{
						Subscriptions: []kargoapi.RepoSubscription{
							{
								Image: &kargoapi.ImageSubscription{
									RepoURL: "docker.io/library/origin-image",
								},
							},
						},
					},
				},
			},
			stepCtx: &PromotionStepContext{
				Project: "test-project",
				Freight: kargoapi.FreightCollection{
					Freight: map[string]kargoapi.FreightReference{
						"Warehouse/test-warehouse": {
							Origin: kargoapi.FreightOrigin{Kind: "Warehouse", Name: "test-warehouse"},
							Images: []kargoapi.Image{
								{RepoURL: "docker.io/library/origin-image", Tag: "2.0.0"},
							},
						},
					},
				},
				FreightRequests: []kargoapi.FreightRequest{
					{
						Origin: kargoapi.FreightOrigin{Kind: "Warehouse", Name: "test-warehouse"},
					},
				},
			},
			cfg: HelmUpdateImageConfig{
				Images: []HelmUpdateImageConfigImage{
					{
						Key:        "image.tag",
						Image:      "docker.io/library/origin-image",
						Value:      Tag,
						FromOrigin: &ChartFromOrigin{Kind: "Warehouse", Name: "test-warehouse"},
					},
				},
			},
			assertions: func(t *testing.T, changes map[string]string, err error) {
				assert.NoError(t, err)
				assert.Equal(t, map[string]string{"image.tag": "2.0.0"}, changes)
			},
		},
		{
			name: "value specified directly",
			stepCtx: &PromotionStepContext{
				Project: "test-project",
			},
			cfg: HelmUpdateImageConfig{
				Images: []HelmUpdateImageConfigImage{{
					Key:   "image.tag",
					Value: "fake-tag",
				}},
			},
			assertions: func(t *testing.T, changes map[string]string, err error) {
				assert.NoError(t, err)
				assert.Equal(t, map[string]string{"image.tag": "fake-tag"}, changes)
			},
		},
	}

	runner := &helmImageUpdater{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, kargoapi.AddToScheme(scheme))

			stepCtx := tt.stepCtx
			stepCtx.KargoClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.objects...).Build()

			changes, err := runner.generateImageUpdates(context.Background(), stepCtx, tt.cfg)
			tt.assertions(t, changes, err)
		})
	}
}

func Test_helmImageUpdater_getDesiredOrigin(t *testing.T) {
	tests := []struct {
		name       string
		fromOrigin *ChartFromOrigin
		assertions func(*testing.T, *kargoapi.FreightOrigin)
	}{
		{
			name:       "nil origin",
			fromOrigin: nil,
			assertions: func(t *testing.T, origin *kargoapi.FreightOrigin) {
				assert.Nil(t, origin)
			},
		},
		{
			name: "valid origin",
			fromOrigin: &ChartFromOrigin{
				Kind: "Repository",
				Name: "test-repo",
			},
			assertions: func(t *testing.T, origin *kargoapi.FreightOrigin) {
				require.NotNil(t, origin)
				assert.Equal(t, kargoapi.FreightOriginKind("Repository"), origin.Kind)
				assert.Equal(t, "test-repo", origin.Name)
			},
		},
	}

	runner := &helmImageUpdater{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origin := runner.getDesiredOrigin(tt.fromOrigin)
			tt.assertions(t, origin)
		})
	}
}

func Test_helmImageUpdater_getValue(t *testing.T) {
	tests := []struct {
		name     string
		image    *kargoapi.Image
		inValue  string
		expected string
	}{
		{
			name: "image and tag",
			image: &kargoapi.Image{
				RepoURL: "docker.io/library/nginx",
				Tag:     "1.19",
			},
			inValue:  ImageAndTag,
			expected: "docker.io/library/nginx:1.19",
		},
		{
			name: "tag only",
			image: &kargoapi.Image{
				RepoURL: "docker.io/library/nginx",
				Tag:     "1.19",
			},
			inValue:  Tag,
			expected: "1.19",
		},
		{
			name: "image and digest",
			image: &kargoapi.Image{
				RepoURL: "docker.io/library/nginx",
				Digest:  "sha256:abcdef1234567890",
			},
			inValue:  ImageAndDigest,
			expected: "docker.io/library/nginx@sha256:abcdef1234567890",
		},
		{
			name: "digest only",
			image: &kargoapi.Image{
				RepoURL: "docker.io/library/nginx",
				Digest:  "sha256:abcdef1234567890",
			},
			inValue:  Digest,
			expected: "sha256:abcdef1234567890",
		},
		{
			name:     "any other value",
			image:    &kargoapi.Image{},
			inValue:  "fake-value",
			expected: "fake-value",
		},
	}

	runner := &helmImageUpdater{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, runner.getValue(tt.image, tt.inValue))
		})
	}
}

func Test_helmImageUpdater_updateValuesFile(t *testing.T) {
	tests := []struct {
		name          string
		valuesContent string
		changes       map[string]string
		assertions    func(*testing.T, string, error)
	}{
		{
			name:          "successful update",
			valuesContent: "key: value\n",
			changes:       map[string]string{"key": "newvalue"},
			assertions: func(t *testing.T, valuesFilePath string, err error) {
				require.NoError(t, err)

				require.FileExists(t, valuesFilePath)
				content, err := os.ReadFile(valuesFilePath)
				require.NoError(t, err)
				assert.Contains(t, string(content), "key: newvalue")
			},
		},
		{
			name:          "file does not exist",
			valuesContent: "",
			changes:       map[string]string{"key": "value"},
			assertions: func(t *testing.T, valuesFilePath string, err error) {
				require.ErrorContains(t, err, "no such file or directory")
				require.NoFileExists(t, valuesFilePath)
			},
		},
		{
			name:          "empty changes",
			valuesContent: "key: value\n",
			changes:       map[string]string{},
			assertions: func(t *testing.T, valuesFilePath string, err error) {
				require.NoError(t, err)
				require.FileExists(t, valuesFilePath)
				content, err := os.ReadFile(valuesFilePath)
				require.NoError(t, err)
				assert.Equal(t, "key: value\n", string(content))
			},
		},
	}

	runner := &helmImageUpdater{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workDir := t.TempDir()
			valuesFile := path.Join(workDir, "values.yaml")

			if tt.valuesContent != "" {
				err := os.WriteFile(valuesFile, []byte(tt.valuesContent), 0o600)
				require.NoError(t, err)
			}

			err := runner.updateValuesFile(workDir, path.Base(valuesFile), tt.changes)
			tt.assertions(t, valuesFile, err)
		})
	}
}

func Test_helmImageUpdater_generateCommitMessage(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		changes    map[string]string
		assertions func(*testing.T, string)
	}{
		{
			name: "no changes",
			path: "values.yaml",
			assertions: func(t *testing.T, result string) {
				assert.Empty(t, result)
			},
		},
		{
			name:    "single change",
			path:    "values.yaml",
			changes: map[string]string{"image": "repo/image:tag1"},
			assertions: func(t *testing.T, result string) {
				assert.Equal(t, `Updated values.yaml

- image: "repo/image:tag1"`, result)
			},
		},
		{
			name: "multiple changes",
			path: "chart/values.yaml",
			changes: map[string]string{
				"image1": "repo1/image1:tag1",
				"image2": "repo2/image2:tag2",
			},
			assertions: func(t *testing.T, result string) {
				assert.Equal(t, `Updated chart/values.yaml

- image1: "repo1/image1:tag1"
- image2: "repo2/image2:tag2"`, result)
			},
		},
	}

	runner := &helmImageUpdater{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runner.generateCommitMessage(tt.path, tt.changes)
			tt.assertions(t, result)
		})
	}
}
