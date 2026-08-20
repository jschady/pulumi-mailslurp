package provider

import (
	gp "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/provider"
	rpc "github.com/pulumi/pulumi/sdk/v3/proto/go"

	"github.com/jschady/pulumi-mailslurp/provider/internal"
	"github.com/jschady/pulumi-mailslurp/provider/pkg/version"
)

// Name matches $PACK in the Makefile and the schema package name.
const Name string = "mailslurp"

// Serve launches the gRPC server for the resource provider.
func Serve() error {
	return provider.Main(Name, New)
}

// New creates the provider server; upgrade tests attach to it in process.
func New(host *provider.HostClient) (rpc.ResourceProviderServer, error) {
	prov, err := internal.NewProvider(internal.RealClientF)
	if err != nil {
		return nil, err
	}
	return gp.RawServer(Name, version.Version, prov)(host)
}
