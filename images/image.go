package images

import (
	"context"
	"encoding/json"
	"io/ioutil"

	"github.com/containerd/containerd/content"
	"github.com/containerd/containerd/rootfs"
	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

// Image provides the model for how containerd views container images.
type Image struct {
	Name   string
	Target ocispec.Descriptor
}

type Store interface {
	Put(ctx context.Context, name string, desc ocispec.Descriptor) error
	Get(ctx context.Context, name string) (Image, error)
	List(ctx context.Context) ([]Image, error)
	Delete(ctx context.Context, name string) error
}

// TODO(stevvooe): Many of these functions make strong platform assumptions,
// which are untrue in a lot of cases. More refactoring must be done here to
// make this work in all cases.

// Config resolves the image configuration descriptor.
//
// The caller can then use the descriptor to resolve and process the
// configuration of the image.
func (image *Image) Config(ctx context.Context, provider content.Provider) (ocispec.Descriptor, error) {
	return Config(ctx, provider, image.Target)
}

// RootFS returns the unpacked diffids that make up and images rootfs.
//
// These are used to verify that a set of layers unpacked to the expected
// values.
func (image *Image) RootFS(ctx context.Context, provider content.Provider) ([]digest.Digest, error) {
	desc, err := image.Config(ctx, provider)
	if err != nil {
		return nil, err
	}
	return RootFS(ctx, provider, desc)
}

// Size returns the total size of an image's packed resources.
func (image *Image) Size(ctx context.Context, provider content.Provider) (int64, error) {
	var size int64
	return size, Walk(ctx, HandlerFunc(func(ctx context.Context, desc ocispec.Descriptor) ([]ocispec.Descriptor, error) {
		switch image.Target.MediaType {
		case MediaTypeDockerSchema2Manifest, ocispec.MediaTypeImageManifest:
			size += desc.Size
			rc, err := provider.Reader(ctx, image.Target.Digest)
			if err != nil {
				return nil, err
			}
			defer rc.Close()

			p, err := ioutil.ReadAll(rc)
			if err != nil {
				return nil, err
			}

			var manifest ocispec.Manifest
			if err := json.Unmarshal(p, &manifest); err != nil {
				return nil, err
			}

			size += manifest.Config.Size

			for _, layer := range manifest.Layers {
				size += layer.Size
			}

			return nil, nil
		default:
			return nil, errors.New("unsupported type")
		}

	}), image.Target)
}

func (image *Image) GetLayers(ctx context.Context, provider content.Provider) ([]rootfs.Layer, error) {
	p, err := content.ReadBlob(ctx, provider, image.Target.Digest)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read manifest blob")
	}

	var manifest ocispec.Manifest
	if err := json.Unmarshal(p, &manifest); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal manifest")
	}

	diffIDs, err := image.RootFS(ctx, provider)
	if err != nil {
		return nil, errors.Wrap(err, "failed to resolve rootfs")
	}

	if len(diffIDs) != len(manifest.Layers) {
		return nil, errors.Errorf("mismatched image rootfs and manifest layers")
	}

	layers := make([]rootfs.Layer, len(diffIDs))
	for i := range diffIDs {
		layers[i].Diff = ocispec.Descriptor{
			// TODO: derive media type from compressed type
			MediaType: ocispec.MediaTypeImageLayer,
			Digest:    diffIDs[i],
		}
		layers[i].Blob = manifest.Layers[i]
	}

	return layers, nil
}

func Config(ctx context.Context, provider content.Provider, image ocispec.Descriptor) (ocispec.Descriptor, error) {
	var configDesc ocispec.Descriptor
	return configDesc, Walk(ctx, HandlerFunc(func(ctx context.Context, desc ocispec.Descriptor) ([]ocispec.Descriptor, error) {
		switch image.MediaType {
		case MediaTypeDockerSchema2Manifest, ocispec.MediaTypeImageManifest:
			rc, err := provider.Reader(ctx, image.Digest)
			if err != nil {
				return nil, err
			}
			defer rc.Close()

			p, err := ioutil.ReadAll(rc)
			if err != nil {
				return nil, err
			}

			var manifest ocispec.Manifest
			if err := json.Unmarshal(p, &manifest); err != nil {
				return nil, err
			}

			configDesc = manifest.Config

			return nil, nil
		default:
			return nil, errors.New("could not resolve config")
		}

	}), image)
}

// RootFS returns the unpacked diffids that make up and images rootfs.
//
// These are used to verify that a set of layers unpacked to the expected
// values.
func RootFS(ctx context.Context, provider content.Provider, configDesc ocispec.Descriptor) ([]digest.Digest, error) {
	p, err := content.ReadBlob(ctx, provider, configDesc.Digest)
	if err != nil {
		return nil, err
	}

	var config ocispec.Image
	if err := json.Unmarshal(p, &config); err != nil {
		return nil, err
	}

	// TODO(stevvooe): Remove this bit when OCI structure uses correct type for
	// rootfs.DiffIDs.
	var diffIDs []digest.Digest
	for _, diffID := range config.RootFS.DiffIDs {
		diffIDs = append(diffIDs, digest.Digest(diffID))
	}

	return diffIDs, nil
}
