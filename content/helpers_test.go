package content

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	_ "crypto/sha256"
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	digest "github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

const (
	configStr = `{
    "created": "2015-10-31T22:22:56.015925234Z",
    "author": "Alyssa P. Hacker <alyspdev@example.com>",
    "architecture": "amd64",
    "os": "linux",
    "config": {
        "User": "alice",
        "ExposedPorts": {
            "8080/tcp": {}
        },
        "Env": [
            "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
            "FOO=oci_is_a",
            "BAR=well_written_spec"
        ],
        "Entrypoint": [
            "/bin/my-app-binary"
        ],
        "Cmd": [
            "--foreground",
            "--config",
            "/etc/my-app.d/default.cfg"
        ],
        "Volumes": {
            "/var/job-result-data": {},
            "/var/log/my-app-logs": {}
        },
        "WorkingDir": "/home/alice",
        "Labels": {
            "com.example.project.git.url": "https://example.com/project.git",
            "com.example.project.git.commit": "45a939b2999782a3f005621a8d0f29aa387e1d6b"
        }
    },
    "rootfs": {
      "diff_ids": [
        "",
        ""
      ],
      "type": "layers"
    },
    "history": [
      {
        "created": "2015-10-31T22:22:54.690851953Z",
        "created_by": "/bin/sh -c #(nop) ADD file:a3bc1e842b69636f9df5256c49c5374fb4eef1e281fe3f282c65fb853ee171c5 in /"
      },
      {
        "created": "2015-10-31T22:22:55.613815829Z",
        "created_by": "/bin/sh -c #(nop) CMD [\"sh\"]",
        "empty_layer": true
      }
    ]
}
`
)

var (
	configDesc ocispec.Descriptor

	expManifest ocispec.Manifest

	expDiffIDs = []digest.Digest{
		"sha256:c6f988f4874bb0add23a778f753c65efe992244e148a1d2ec2a8b664fb66bbd1",
		"sha256:5f70bf18a086007016e948b04aed3b82103a36bea41755b6cddfaf10ace3c6ef",
	}
)

func TestReadBlob(t *testing.T) {
	ctx, _, cs, manifest, _, _, cleanup := setupImageStore(t)
	defer cleanup()

	p, err := ReadBlob(ctx, cs, manifest.Digest)
	if err != nil {
		t.Fatal(err)
	}

	expected, err := json.Marshal(&expManifest)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(p, expected) {
		t.Fatalf("read blob result[%+v] not match to the expected[%+v]!", p, expected)
	}
}

func setupImageStore(t checker) (ctx context.Context, root string, cs Store,
	manifest, config ocispec.Descriptor, layers []ocispec.Descriptor,
	clean func()) {

	var (
		err           error
		configContent ocispec.Image
	)

	pc, _, _, ok := runtime.Caller(1)
	if !ok {
		t.Fatal("failed to resolve caller")
	}
	fn := runtime.FuncForPC(pc)

	root, err = ioutil.TempDir("", filepath.Base(fn.Name())+"-")
	if err != nil {
		t.Fatal(err)
	}

	cs, err = NewStore(root)
	if err != nil {
		os.RemoveAll(root)
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	clean = func() {
		cancel()
		os.RemoveAll(root)
	}

	// create image layer blob file.
	for _, ref := range []string{
		"layer01",
		"layer02",
	} {
		p := make([]byte, (4 << 20))
		if _, err := rand.Read(p); err != nil {
			t.Fatal(err)
		}
		layer, err := contentWriter(ctx, cs, ref, p)
		if err != nil {
			clean()
			t.Fatal(err)
		}
		layer.MediaType = ocispec.MediaTypeImageLayer
		layers = append(layers, layer)
	}

	// create image configuration blob file.
	err = json.Unmarshal([]byte(configStr), &configContent)
	if err != nil {
		clean()
		t.Fatal(err)
	}
	configContent.RootFS.DiffIDs = expDiffIDs
	p, err := json.Marshal(&configContent)
	if err != nil {
		clean()
		t.Fatal(err)
	}
	config, err = contentWriter(ctx, cs, "config", p)
	if err != nil {
		clean()
		t.Fatal(err)
	}
	config.MediaType = ocispec.MediaTypeImageConfig

	// create image manifest blob file.
	expManifest = ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		Config:    config,
		Layers:    layers,
	}
	p, err = json.Marshal(&expManifest)
	if err != nil {
		clean()
		t.Fatal(err)
	}
	manifest, err = contentWriter(ctx, cs, "manifest", p)
	if err != nil {
		clean()
		t.Fatal(err)
	}
	manifest.MediaType = ocispec.MediaTypeImageManifest

	return
}

func contentWriter(ctx context.Context, cs Store, ref string, p []byte) (ocispec.Descriptor, error) {
	var (
		size     = int64(len(p))
		expected = digest.FromBytes(p)
	)

	cw, err := cs.Writer(ctx, ref, 0, "")
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	err = Copy(cw, bufio.NewReader(ioutil.NopCloser(bytes.NewReader(p))), size, expected)
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	err = cw.Close()
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	return ocispec.Descriptor{Digest: expected, Size: size}, nil
}
