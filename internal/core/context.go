package core

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Context is now a standard context.Context alias.
// Core values (Client, Scheme, APIReader, Platform) are stored via context.WithValue.
type Context = context.Context

type contextValuesKey struct{}

type contextValues struct {
	client   client.Client
	scheme   *runtime.Scheme
	reader   client.Reader
	platform Platform
}

func NewContext(mgr Manager, ctx context.Context) context.Context {
	return context.WithValue(ctx, contextValuesKey{}, &contextValues{
		client:   mgr.GetClient(),
		scheme:   mgr.GetScheme(),
		reader:   mgr.GetAPIReader(),
		platform: mgr.GetPlatform(),
	})
}

func getContextValues(ctx context.Context) *contextValues {
	return ctx.Value(contextValuesKey{}).(*contextValues)
}

func GetClient(ctx context.Context) client.Client {
	return getContextValues(ctx).client
}

func GetScheme(ctx context.Context) *runtime.Scheme {
	return getContextValues(ctx).scheme
}

func GetAPIReader(ctx context.Context) client.Reader {
	return getContextValues(ctx).reader
}

func GetPlatform(ctx context.Context) Platform {
	return getContextValues(ctx).platform
}
