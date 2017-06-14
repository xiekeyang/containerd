package images

import (
	"reflect"
	"testing"

	digest "github.com/opencontainers/go-digest"
)

const (
	testImage = "docker.io/library/alpine:latest"
)

var (
	expDiffIDs = []digest.Digest{
		"sha256:c6f988f4874bb0add23a778f753c65efe992244e148a1d2ec2a8b664fb66bbd1",
		"sha256:5f70bf18a086007016e948b04aed3b82103a36bea41755b6cddfaf10ace3c6ef",
	}
)

func TestImageMemberConfig(t *testing.T) {
	ctx, _, cs, manifest, expConfig, _, cleanup := setupImageStore(t)
	defer cleanup()

	image := Image{Name: testImage, Target: manifest}

	config, err := image.Config(ctx, cs)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(config, expConfig) {
		t.Fatalf("config descriptors[%+v] not match to the expected[%+v]!", config, expConfig)
	}
}

func TestImageMemberRootFS(t *testing.T) {
	ctx, _, cs, manifest, _, _, cleanup := setupImageStore(t)
	defer cleanup()

	image := Image{Name: testImage, Target: manifest}

	diffIDs, err := image.RootFS(ctx, cs)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(diffIDs, expDiffIDs) {
		t.Fatalf("diff ids descriptors[%+v] not match to the expected[%+v]!", diffIDs, expDiffIDs)
	}
}

func TestImageMemberSize(t *testing.T) {
	var expected int64 = 0
	ctx, _, cs, manifest, config, layers, cleanup := setupImageStore(t)
	defer cleanup()

	image := Image{Name: testImage, Target: manifest}
	size, err := image.Size(ctx, cs)
	if err != nil {
		t.Fatal(err)
	}
	expected = manifest.Size + config.Size
	for _, layer := range layers {
		expected += layer.Size
	}
	if size != expected {
		t.Fatalf("image size[%d] not equal to the expected[%d]!", size, expected)
	}
}

func TestImageMemberGetLayers(t *testing.T) {
	ctx, _, cs, manifest, _, layers, cleanup := setupImageStore(t)
	defer cleanup()

	image := Image{Name: testImage, Target: manifest}
	rootfsLayers, err := image.GetLayers(ctx, cs)
	if err != nil {
		t.Fatal(err)
	}

	if len(rootfsLayers) != len(layers) {
		t.Fatalf("layer length [%d] not equal to the expected[%d]!", len(rootfsLayers), len(layers))
	}
	for i, rootfsLayer := range rootfsLayers {
		if rootfsLayer.Diff.Digest != expDiffIDs[i] {
			t.Fatalf("layer diffid[%v] not equal to the expected[%v]!", rootfsLayer.Diff.Digest, expDiffIDs[i])
		}
		if rootfsLayer.Blob.Digest != layers[i].Digest {
			t.Fatalf("layer blob digest[%v] not equal to the expected[%v]!", rootfsLayer.Blob.Digest, layers[i].Digest)
		}
	}
}

func TestImageConfig(t *testing.T) {
	ctx, _, cs, image, expConfig, _, cleanup := setupImageStore(t)
	defer cleanup()

	config, err := Config(ctx, cs, image)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(config, expConfig) {
		t.Fatalf("config descriptors[%+v] not match to the expected[%+v]!", config, expConfig)
	}
}

func TestImageRootFS(t *testing.T) {
	ctx, _, cs, _, expConfig, _, cleanup := setupImageStore(t)
	defer cleanup()

	diffIDs, err := RootFS(ctx, cs, expConfig)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(diffIDs, expDiffIDs) {
		t.Fatalf("diff ids descriptors[%+v] not match to the expected[%+v]!", diffIDs, expDiffIDs)
	}
}
