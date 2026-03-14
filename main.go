package main

import (
	"github.com/alecthomas/kong"
	"github.com/crossplane/function-sdk-go"

	"github.com/wompipomp/function-starlark/runtime"
)

// CLI represents the command-line interface for the function.
type CLI struct {
	Debug bool `help:"Emit debug logs in addition to info logs." short:"d"`

	Network string `help:"Network on which to listen for gRPC connections." default:"tcp" env:"FUNCTION_NETWORK"`
	Address string `help:"Address at which to listen for gRPC connections." default:":9443" env:"FUNCTION_ADDRESS"`

	TLSCertsDir string `help:"Directory containing TLS certificates." env:"TLS_SERVER_CERTS_DIR"`
	Insecure    bool   `help:"Run without mTLS credentials." env:"FUNCTION_INSECURE"`

	MaxRecvMessageSize int `help:"Maximum message size in MB." default:"4" env:"MAX_RECV_MESSAGE_SIZE"`
}

func (c *CLI) Run() error {
	log, err := function.NewLogger(c.Debug)
	if err != nil {
		return err
	}

	rt := runtime.NewRuntime(log)

	return function.Serve(&Function{log: log, runtime: rt, scriptDir: "/scripts"},
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
