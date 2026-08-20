package internal

import (
	"context"
	"fmt"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

// emptyRequiredInputMessage refuses a required property that carries no value.
const emptyRequiredInputMessage = "The `%s` property is empty. Set a value and try again."

// isEmptyRequiredValue reports whether one written value means nothing: a string of spaces, or a
// list with no element. A value the program computes is unknown here, and a preview must keep it.
func isEmptyRequiredValue(value property.Value) bool {
	switch {
	case value.IsString():
		return blankIdentifier(value.AsString())
	case value.IsArray():
		return value.AsArray().Len() == 0
	default:
		return false
	}
}

// refuseEmptyRequiredInputs answers one failure per required property that carries no value. A
// blank value reaches the API as a missing one, and a diff of it plans a removal the schema forbids.
func refuseEmptyRequiredInputs(inputs property.Map, required []string) []p.CheckFailure {
	var failures []p.CheckFailure
	for _, name := range required {
		value, ok := inputs.GetOk(name)
		if !ok || !isEmptyRequiredValue(value) {
			continue
		}
		failures = append(failures, p.CheckFailure{
			Property: name,
			Reason:   fmt.Sprintf(emptyRequiredInputMessage, name),
		})
	}
	return failures
}

// checkRequiredInputs wraps the default check, so the defaults and the secrets of the framework
// still apply, and adds the blank-value refusal for the named required properties.
func checkRequiredInputs[I any](
	ctx context.Context, req infer.CheckRequest, required ...string,
) (infer.CheckResponse[I], error) {
	inputs, failures, err := infer.DefaultCheck[I](ctx, req.NewInputs)
	if err != nil {
		return infer.CheckResponse[I]{Inputs: inputs, Failures: failures}, err
	}
	return infer.CheckResponse[I]{
		Inputs:   inputs,
		Failures: append(failures, refuseEmptyRequiredInputs(req.NewInputs, required)...),
	}, nil
}
