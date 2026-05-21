package core

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Context interface {
	context.Context
	GetClient() client.Client
	GetScheme() *runtime.Scheme
	GetAPIReader() client.Reader
	GetPlatform() Platform
}

type defaultContext struct {
	context.Context
	mgr Manager
}

func (d defaultContext) GetAPIReader() client.Reader {
	return d.mgr.GetAPIReader()
}

func (d defaultContext) GetPlatform() Platform {
	return d.mgr.GetPlatform()
}

func (d defaultContext) GetClient() client.Client {
	return d.mgr.GetClient()
}

func (d defaultContext) GetScheme() *runtime.Scheme {
	return d.mgr.GetScheme()
}

var _ Context = &defaultContext{}

type platformContext struct {
	Context
	platform Platform
}

func (p platformContext) GetPlatform() Platform {
	return p.platform
}

func WithPlatform(ctx Context, platform Platform) Context {
	return platformContext{
		Context:  ctx,
		platform: platform,
	}
}

func NewContext(mgr Manager, ctx context.Context) *defaultContext {
	return &defaultContext{
		Context: ctx,
		mgr:     mgr,
	}
}
