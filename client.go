package containerd

import (
	"context"
	"io/ioutil"
	"log"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/containerd/containerd/api/services/containers"
	contentapi "github.com/containerd/containerd/api/services/content"
	diffapi "github.com/containerd/containerd/api/services/diff"
	"github.com/containerd/containerd/api/services/execution"
	imagesapi "github.com/containerd/containerd/api/services/images"
	namespacesapi "github.com/containerd/containerd/api/services/namespaces"
	snapshotapi "github.com/containerd/containerd/api/services/snapshot"
	versionservice "github.com/containerd/containerd/api/services/version"
	"github.com/containerd/containerd/content"
	"github.com/containerd/containerd/images"
	"github.com/containerd/containerd/remotes"
	"github.com/containerd/containerd/remotes/docker"
	contentservice "github.com/containerd/containerd/services/content"
	"github.com/containerd/containerd/services/diff"
	diffservice "github.com/containerd/containerd/services/diff"
	imagesservice "github.com/containerd/containerd/services/images"
	snapshotservice "github.com/containerd/containerd/services/snapshot"
	"github.com/containerd/containerd/snapshot"
	pempty "github.com/golang/protobuf/ptypes/empty"
	"github.com/opencontainers/image-spec/identity"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/health/grpc_health_v1"
)

func init() {
	// reset the grpc logger so that it does not output in the STDIO of the calling process
	grpclog.SetLogger(log.New(ioutil.Discard, "", log.LstdFlags))
}

type NewClientOpts func(c *Client) error

// New returns a new containerd client that is connected to the containerd
// instance provided by address
func New(address string, opts ...NewClientOpts) (*Client, error) {
	gopts := []grpc.DialOption{
		grpc.WithInsecure(),
		grpc.WithTimeout(100 * time.Second),
		grpc.WithDialer(dialer),
	}
	conn, err := grpc.Dial(dialAddress(address), gopts...)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to dial %q", address)
	}
	c := &Client{
		conn:    conn,
		runtime: runtime.GOOS,
	}
	for _, o := range opts {
		if err := o(c); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// Client is the client to interact with containerd and its various services
// using a uniform interface
type Client struct {
	conn *grpc.ClientConn

	runtime string
}

func (c *Client) IsServing(ctx context.Context) (bool, error) {
	r, err := c.HealthService().Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		return false, err
	}
	return r.Status == grpc_health_v1.HealthCheckResponse_SERVING, nil
}

// Containers returns all containers created in containerd
func (c *Client) Containers(ctx context.Context) ([]Container, error) {
	r, err := c.ContainerService().List(ctx, &containers.ListContainersRequest{})
	if err != nil {
		return nil, err
	}
	var out []Container
	for _, container := range r.Containers {
		out = append(out, containerFromProto(c, container))
	}
	return out, nil
}

type NewContainerOpts func(ctx context.Context, client *Client, c *containers.Container) error

// WithContainerLabels adds the provided labels to the container
func WithContainerLabels(labels map[string]string) NewContainerOpts {
	return func(_ context.Context, _ *Client, c *containers.Container) error {
		c.Labels = labels
		return nil
	}
}

// WithExistingRootFS uses an existing root filesystem for the container
func WithExistingRootFS(id string) NewContainerOpts {
	return func(ctx context.Context, client *Client, c *containers.Container) error {
		// check that the snapshot exists, if not, fail on creation
		if _, err := client.SnapshotService().Mounts(ctx, id); err != nil {
			return err
		}
		c.RootFS = id
		return nil
	}
}

// WithNewRootFS allocates a new snapshot to be used by the container as the
// root filesystem in read-write mode
func WithNewRootFS(id string, i Image) NewContainerOpts {
	return func(ctx context.Context, client *Client, c *containers.Container) error {
		diffIDs, err := i.(*image).i.RootFS(ctx, client.ContentStore())
		if err != nil {
			return err
		}
		if _, err := client.SnapshotService().Prepare(ctx, id, identity.ChainID(diffIDs).String()); err != nil {
			return err
		}
		c.RootFS = id
		return nil
	}
}

// WithNewReadonlyRootFS allocates a new snapshot to be used by the container as the
// root filesystem in read-only mode
func WithNewReadonlyRootFS(id string, i Image) NewContainerOpts {
	return func(ctx context.Context, client *Client, c *containers.Container) error {
		diffIDs, err := i.(*image).i.RootFS(ctx, client.ContentStore())
		if err != nil {
			return err
		}
		if _, err := client.SnapshotService().View(ctx, id, identity.ChainID(diffIDs).String()); err != nil {
			return err
		}
		c.RootFS = id
		return nil
	}
}

func WithRuntime(name string) NewContainerOpts {
	return func(ctx context.Context, client *Client, c *containers.Container) error {
		c.Runtime = name
		return nil
	}
}

func WithImage(i Image) NewContainerOpts {
	return func(ctx context.Context, client *Client, c *containers.Container) error {
		c.Image = i.Name()
		return nil
	}
}

// NewContainer will create a new container in container with the provided id
// the id must be unique within the namespace
func (c *Client) NewContainer(ctx context.Context, id string, opts ...NewContainerOpts) (Container, error) {
	container := containers.Container{
		ID:      id,
		Runtime: c.runtime,
	}
	for _, o := range opts {
		if err := o(ctx, c, &container); err != nil {
			return nil, err
		}
	}
	r, err := c.ContainerService().Create(ctx, &containers.CreateContainerRequest{
		Container: container,
	})
	if err != nil {
		return nil, err
	}
	return containerFromProto(c, r.Container), nil
}

func (c *Client) LoadContainer(ctx context.Context, id string) (Container, error) {
	response, err := c.ContainerService().Get(ctx, &containers.GetContainerRequest{
		ID: id,
	})
	if err != nil {
		return nil, err
	}
	return containerFromProto(c, response.Container), nil
}

type RemoteOpts func(*Client, *RemoteContext) error

// RemoteContext is used to configure object resolutions and transfers with
// remote content stores and image providers.
type RemoteContext struct {
	// Resolver is used to resolve names to objects, fetchers, and pushers.
	// If no resolver is provided, defaults to Docker registry resolver.
	Resolver remotes.Resolver

	// Unpack is done after an image is pulled to extract into a snapshotter.
	// If an image is not unpacked on pull, it can be unpacked any time
	// afterwards. Unpacking is required to run an image.
	Unpack bool

	// PushWrapper allows hooking into the push method. This can be used
	// track content that is being sent to the remote.
	PushWrapper func(remotes.Pusher) remotes.Pusher

	// BaseHandlers are a set of handlers which get are called on dispatch.
	// These handlers always get called before any operation specific
	// handlers.
	BaseHandlers []images.Handler
}

func defaultRemoteContext() *RemoteContext {
	return &RemoteContext{
		Resolver: docker.NewResolver(docker.ResolverOptions{
			Client: http.DefaultClient,
		}),
	}
}

// WithPullUnpack is used to unpack an image after pull. This
// uses the snapshotter, content store, and diff service
// configured for the client.
func WithPullUnpack(client *Client, c *RemoteContext) error {
	c.Unpack = true
	return nil
}

// WithResolver specifies the resolver to use.
func WithResolver(resolver remotes.Resolver) RemoteOpts {
	return func(client *Client, c *RemoteContext) error {
		c.Resolver = resolver
		return nil
	}
}

// WithImageHandler adds a base handler to be called on dispatch.
func WithImageHandler(h images.Handler) RemoteOpts {
	return func(client *Client, c *RemoteContext) error {
		c.BaseHandlers = append(c.BaseHandlers, h)
		return nil
	}
}

// WithPushWrapper is used to wrap a pusher to hook into
// the push content as it is sent to a remote.
func WithPushWrapper(w func(remotes.Pusher) remotes.Pusher) RemoteOpts {
	return func(client *Client, c *RemoteContext) error {
		c.PushWrapper = w
		return nil
	}
}

func (c *Client) Pull(ctx context.Context, ref string, opts ...RemoteOpts) (Image, error) {
	pullCtx := defaultRemoteContext()
	for _, o := range opts {
		if err := o(c, pullCtx); err != nil {
			return nil, err
		}
	}
	store := c.ContentStore()

	name, desc, err := pullCtx.Resolver.Resolve(ctx, ref)
	if err != nil {
		return nil, err
	}
	fetcher, err := pullCtx.Resolver.Fetcher(ctx, name)
	if err != nil {
		return nil, err
	}

	handlers := append(pullCtx.BaseHandlers,
		remotes.FetchHandler(store, fetcher),
		images.ChildrenHandler(store),
	)
	if err := images.Dispatch(ctx, images.Handlers(handlers...), desc); err != nil {
		return nil, err
	}
	is := c.ImageService()
	if err := is.Put(ctx, name, desc); err != nil {
		return nil, err
	}
	i, err := is.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	img := &image{
		client: c,
		i:      i,
	}
	if pullCtx.Unpack {
		if err := img.Unpack(ctx); err != nil {
			return nil, err
		}
	}
	return img, nil
}

func (c *Client) Push(ctx context.Context, ref string, desc ocispec.Descriptor, opts ...RemoteOpts) error {
	pushCtx := defaultRemoteContext()
	for _, o := range opts {
		if err := o(c, pushCtx); err != nil {
			return err
		}
	}

	pusher, err := pushCtx.Resolver.Pusher(ctx, ref)
	if err != nil {
		return err
	}

	if pushCtx.PushWrapper != nil {
		pusher = pushCtx.PushWrapper(pusher)
	}

	var m sync.Mutex
	manifestStack := []ocispec.Descriptor{}

	filterHandler := images.HandlerFunc(func(ctx context.Context, desc ocispec.Descriptor) ([]ocispec.Descriptor, error) {
		switch desc.MediaType {
		case images.MediaTypeDockerSchema2Manifest, ocispec.MediaTypeImageManifest,
			images.MediaTypeDockerSchema2ManifestList, ocispec.MediaTypeImageIndex:
			m.Lock()
			manifestStack = append(manifestStack, desc)
			m.Unlock()
			return nil, images.StopHandler
		default:
			return nil, nil
		}
	})

	cs := c.ContentStore()
	pushHandler := remotes.PushHandler(cs, pusher)

	handlers := append(pushCtx.BaseHandlers,
		images.ChildrenHandler(cs),
		filterHandler,
		pushHandler,
	)

	if err := images.Dispatch(ctx, images.Handlers(handlers...), desc); err != nil {
		return err
	}

	// Iterate in reverse order as seen, parent always uploaded after child
	for i := len(manifestStack) - 1; i >= 0; i-- {
		_, err := pushHandler(ctx, manifestStack[i])
		if err != nil {
			return err
		}
	}
	return nil
}

// GetImage returns an existing image
func (c *Client) GetImage(ctx context.Context, ref string) (Image, error) {
	i, err := c.ImageService().Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	return &image{
		client: c,
		i:      i,
	}, nil
}

// ListImages returns all existing images
func (c *Client) ListImages(ctx context.Context) ([]Image, error) {
	imgs, err := c.ImageService().List(ctx)
	if err != nil {
		return nil, err
	}
	images := make([]Image, len(imgs))
	for i, img := range imgs {
		images[i] = &image{
			client: c,
			i:      img,
		}
	}
	return images, nil
}

// Close closes the clients connection to containerd
func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) NamespaceService() namespacesapi.NamespacesClient {
	return namespacesapi.NewNamespacesClient(c.conn)
}

func (c *Client) ContainerService() containers.ContainersClient {
	return containers.NewContainersClient(c.conn)
}

func (c *Client) ContentStore() content.Store {
	return contentservice.NewStoreFromClient(contentapi.NewContentClient(c.conn))
}

func (c *Client) SnapshotService() snapshot.Snapshotter {
	return snapshotservice.NewSnapshotterFromClient(snapshotapi.NewSnapshotClient(c.conn))
}

func (c *Client) TaskService() execution.TasksClient {
	return execution.NewTasksClient(c.conn)
}

func (c *Client) ImageService() images.Store {
	return imagesservice.NewStoreFromClient(imagesapi.NewImagesClient(c.conn))
}

func (c *Client) DiffService() diff.DiffService {
	return diffservice.NewDiffServiceFromClient(diffapi.NewDiffClient(c.conn))
}

func (c *Client) HealthService() grpc_health_v1.HealthClient {
	return grpc_health_v1.NewHealthClient(c.conn)
}

func (c *Client) VersionService() versionservice.VersionClient {
	return versionservice.NewVersionClient(c.conn)
}

type Version struct {
	Version  string
	Revision string
}

func (c *Client) Version(ctx context.Context) (Version, error) {
	response, err := c.VersionService().Version(ctx, &pempty.Empty{})
	if err != nil {
		return Version{}, err
	}
	return Version{
		Version:  response.Version,
		Revision: response.Revision,
	}, nil
}
