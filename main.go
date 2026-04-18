package main

import (
	"time"

	"github.com/alecthomas/kong"
	"github.com/crossplane/function-sdk-go"

	"github.com/wompipomp/function-starlark/runtime"
	"github.com/wompipomp/function-starlark/runtime/oci"
)

// CLI represents the command-line interface for the function.
type CLI struct {
	Debug bool `help:"Emit debug logs in addition to info logs." short:"d"`

	Network string `help:"Network on which to listen for gRPC connections." default:"tcp" env:"FUNCTION_NETWORK"`
	Address string `help:"Address at which to listen for gRPC connections." default:":9443" env:"FUNCTION_ADDRESS"`

	TLSCertsDir string `help:"Directory containing TLS certificates." env:"TLS_SERVER_CERTS_DIR"`
	Insecure    bool   `help:"Run without mTLS credentials." env:"FUNCTION_INSECURE"`

	MaxRecvMessageSize int           `help:"Maximum message size in MB." default:"4" env:"MAX_RECV_MESSAGE_SIZE"`
	OCIPullPolicy      string        `help:"OCI pull policy (Kubernetes-style). 'IfNotPresent' (default): cache tag->digest for the pod lifetime; never revalidate. 'Always': revalidate via HEAD on each reconciliation (or once per OCICacheTTL window)." default:"IfNotPresent" enum:"IfNotPresent,Always" env:"STARLARK_OCI_PULL_POLICY"`
	OCICacheTTL        time.Duration `help:"TTL for OCI tag-to-digest cache entries. Only consulted when OCIPullPolicy=Always; ignored under IfNotPresent. 0 means revalidate on every reconciliation." default:"0" env:"STARLARK_OCI_CACHE_TTL"`
}

func (c *CLI) Run() error {
	log, err := function.NewLogger(c.Debug)
	if err != nil {
		return err
	}

	log.Info("Starting function-starlark", "debug", c.Debug, "address", c.Address, "insecure", c.Insecure, "ociPullPolicy", c.OCIPullPolicy, "ociCacheTTL", c.OCICacheTTL)

	rt := runtime.NewRuntime(log)
	cache := oci.NewCache(oci.PullPolicy(c.OCIPullPolicy), c.OCICacheTTL)

	return function.Serve(&Function{log: log, runtime: rt, scriptDir: "/scripts", ociCache: cache},
		function.Listen(c.Network, c.Address),
		function.MTLSCertificates(c.TLSCertsDir),
		function.Insecure(c.Insecure),
		function.MaxRecvMessageSize(c.MaxRecvMessageSize*1024*1024),
	)
}

func main() {
	ctx := kong.Parse(&CLI{})
	ctx.FatalIfErrorf(ctx.Run())
}
