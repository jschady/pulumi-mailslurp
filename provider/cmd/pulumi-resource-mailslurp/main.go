package main

import (
	"github.com/pulumi/pulumi/sdk/v3/go/common/util/cmdutil"

	"github.com/jschady/pulumi-mailslurp/provider"
)

func main() {
	if err := provider.Serve(); err != nil {
		cmdutil.ExitError(err.Error())
	}
}
