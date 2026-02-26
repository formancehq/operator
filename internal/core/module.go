package core

import (
	"strings"

	"github.com/iancoleman/strcase"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func LowerCamelCaseKind(ctx Context, ob client.Object) string {
	kinds, _, err := ctx.GetScheme().ObjectKinds(ob)
	if err != nil {
		panic(err)
	}
	return strcase.ToLowerCamel(kinds[0].Kind)
}

func LowerCaseKind(ctx Context, ob client.Object) string {
	kinds, _, err := ctx.GetScheme().ObjectKinds(ob)
	if err != nil {
		panic(err)
	}
	return strings.ToLower(kinds[0].Kind)
}
