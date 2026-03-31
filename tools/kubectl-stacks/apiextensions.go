package main

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

var apiExtensionsGV = schema.GroupVersion{
	Group:   "apiextensions.k8s.io",
	Version: "v1",
}

// unstructuredNegotiator implements runtime.NegotiatedSerializer for raw JSON
// responses (used to query CRDs without importing apiextensions types).
type unstructuredNegotiator struct{}

func (unstructuredNegotiator) EncoderForVersion(e runtime.Encoder, _ runtime.GroupVersioner) runtime.Encoder {
	return e
}

func (unstructuredNegotiator) DecoderToVersion(d runtime.Decoder, _ runtime.GroupVersioner) runtime.Decoder {
	return d
}

func (u unstructuredNegotiator) SupportedMediaTypes() []runtime.SerializerInfo {
	scheme := runtime.NewScheme()
	codecs := serializer.NewCodecFactory(scheme)
	return codecs.SupportedMediaTypes()
}
