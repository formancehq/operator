//go:build !ignore_autogenerated
// +build !ignore_autogenerated

/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by controller-gen. DO NOT EDIT.

package v1beta1

import (
	"encoding/json"
	"github.com/numary/operator/apis/sharedtypes"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Auth) DeepCopyInto(out *Auth) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Auth.
func (in *Auth) DeepCopy() *Auth {
	if in == nil {
		return nil
	}
	out := new(Auth)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Auth) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AuthList) DeepCopyInto(out *AuthList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Auth, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AuthList.
func (in *AuthList) DeepCopy() *AuthList {
	if in == nil {
		return nil
	}
	out := new(AuthList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *AuthList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AuthSpec) DeepCopyInto(out *AuthSpec) {
	*out = *in
	in.Scalable.DeepCopyInto(&out.Scalable)
	in.ImageHolder.DeepCopyInto(&out.ImageHolder)
	in.Postgres.DeepCopyInto(&out.Postgres)
	if in.Ingress != nil {
		in, out := &in.Ingress, &out.Ingress
		*out = new(sharedtypes.IngressSpec)
		(*in).DeepCopyInto(*out)
	}
	in.DelegatedOIDCServer.DeepCopyInto(&out.DelegatedOIDCServer)
	if in.Monitoring != nil {
		in, out := &in.Monitoring, &out.Monitoring
		*out = new(sharedtypes.MonitoringSpec)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AuthSpec.
func (in *AuthSpec) DeepCopy() *AuthSpec {
	if in == nil {
		return nil
	}
	out := new(AuthSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CollectorConfig) DeepCopyInto(out *CollectorConfig) {
	*out = *in
	in.KafkaConfig.DeepCopyInto(&out.KafkaConfig)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CollectorConfig.
func (in *CollectorConfig) DeepCopy() *CollectorConfig {
	if in == nil {
		return nil
	}
	out := new(CollectorConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Control) DeepCopyInto(out *Control) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Control.
func (in *Control) DeepCopy() *Control {
	if in == nil {
		return nil
	}
	out := new(Control)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Control) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ControlList) DeepCopyInto(out *ControlList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Control, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ControlList.
func (in *ControlList) DeepCopy() *ControlList {
	if in == nil {
		return nil
	}
	out := new(ControlList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ControlList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ControlSpec) DeepCopyInto(out *ControlSpec) {
	*out = *in
	in.Scalable.DeepCopyInto(&out.Scalable)
	in.ImageHolder.DeepCopyInto(&out.ImageHolder)
	if in.Ingress != nil {
		in, out := &in.Ingress, &out.Ingress
		*out = new(sharedtypes.IngressSpec)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ControlSpec.
func (in *ControlSpec) DeepCopy() *ControlSpec {
	if in == nil {
		return nil
	}
	out := new(ControlSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DelegatedOIDCServerConfiguration) DeepCopyInto(out *DelegatedOIDCServerConfiguration) {
	*out = *in
	if in.IssuerFrom != nil {
		in, out := &in.IssuerFrom, &out.IssuerFrom
		*out = new(sharedtypes.ConfigSource)
		(*in).DeepCopyInto(*out)
	}
	if in.ClientIDFrom != nil {
		in, out := &in.ClientIDFrom, &out.ClientIDFrom
		*out = new(sharedtypes.ConfigSource)
		(*in).DeepCopyInto(*out)
	}
	if in.ClientSecretFrom != nil {
		in, out := &in.ClientSecretFrom, &out.ClientSecretFrom
		*out = new(sharedtypes.ConfigSource)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DelegatedOIDCServerConfiguration.
func (in *DelegatedOIDCServerConfiguration) DeepCopy() *DelegatedOIDCServerConfiguration {
	if in == nil {
		return nil
	}
	out := new(DelegatedOIDCServerConfiguration)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ElasticSearchBasicAuthConfig) DeepCopyInto(out *ElasticSearchBasicAuthConfig) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ElasticSearchBasicAuthConfig.
func (in *ElasticSearchBasicAuthConfig) DeepCopy() *ElasticSearchBasicAuthConfig {
	if in == nil {
		return nil
	}
	out := new(ElasticSearchBasicAuthConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ElasticSearchConfig) DeepCopyInto(out *ElasticSearchConfig) {
	*out = *in
	if in.HostFrom != nil {
		in, out := &in.HostFrom, &out.HostFrom
		*out = new(sharedtypes.ConfigSource)
		(*in).DeepCopyInto(*out)
	}
	if in.PortFrom != nil {
		in, out := &in.PortFrom, &out.PortFrom
		*out = new(sharedtypes.ConfigSource)
		(*in).DeepCopyInto(*out)
	}
	out.TLS = in.TLS
	if in.BasicAuth != nil {
		in, out := &in.BasicAuth, &out.BasicAuth
		*out = new(ElasticSearchBasicAuthConfig)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ElasticSearchConfig.
func (in *ElasticSearchConfig) DeepCopy() *ElasticSearchConfig {
	if in == nil {
		return nil
	}
	out := new(ElasticSearchConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ElasticSearchTLSConfig) DeepCopyInto(out *ElasticSearchTLSConfig) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ElasticSearchTLSConfig.
func (in *ElasticSearchTLSConfig) DeepCopy() *ElasticSearchTLSConfig {
	if in == nil {
		return nil
	}
	out := new(ElasticSearchTLSConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Ledger) DeepCopyInto(out *Ledger) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Ledger.
func (in *Ledger) DeepCopy() *Ledger {
	if in == nil {
		return nil
	}
	out := new(Ledger)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Ledger) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *LedgerList) DeepCopyInto(out *LedgerList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Ledger, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new LedgerList.
func (in *LedgerList) DeepCopy() *LedgerList {
	if in == nil {
		return nil
	}
	out := new(LedgerList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *LedgerList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *LedgerSpec) DeepCopyInto(out *LedgerSpec) {
	*out = *in
	in.Scalable.DeepCopyInto(&out.Scalable)
	in.ImageHolder.DeepCopyInto(&out.ImageHolder)
	if in.Ingress != nil {
		in, out := &in.Ingress, &out.Ingress
		*out = new(sharedtypes.IngressSpec)
		(*in).DeepCopyInto(*out)
	}
	in.Postgres.DeepCopyInto(&out.Postgres)
	if in.Auth != nil {
		in, out := &in.Auth, &out.Auth
		*out = new(sharedtypes.AuthConfigSpec)
		(*in).DeepCopyInto(*out)
	}
	if in.Monitoring != nil {
		in, out := &in.Monitoring, &out.Monitoring
		*out = new(sharedtypes.MonitoringSpec)
		(*in).DeepCopyInto(*out)
	}
	if in.Collector != nil {
		in, out := &in.Collector, &out.Collector
		*out = new(CollectorConfig)
		(*in).DeepCopyInto(*out)
	}
	in.LockingStrategy.DeepCopyInto(&out.LockingStrategy)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new LedgerSpec.
func (in *LedgerSpec) DeepCopy() *LedgerSpec {
	if in == nil {
		return nil
	}
	out := new(LedgerSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *LockingStrategy) DeepCopyInto(out *LockingStrategy) {
	*out = *in
	if in.Redis != nil {
		in, out := &in.Redis, &out.Redis
		*out = new(LockingStrategyRedisConfig)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new LockingStrategy.
func (in *LockingStrategy) DeepCopy() *LockingStrategy {
	if in == nil {
		return nil
	}
	out := new(LockingStrategy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *LockingStrategyRedisConfig) DeepCopyInto(out *LockingStrategyRedisConfig) {
	*out = *in
	if in.UriFrom != nil {
		in, out := &in.UriFrom, &out.UriFrom
		*out = new(sharedtypes.ConfigSource)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new LockingStrategyRedisConfig.
func (in *LockingStrategyRedisConfig) DeepCopy() *LockingStrategyRedisConfig {
	if in == nil {
		return nil
	}
	out := new(LockingStrategyRedisConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MongoDBConfig) DeepCopyInto(out *MongoDBConfig) {
	*out = *in
	if in.HostFrom != nil {
		in, out := &in.HostFrom, &out.HostFrom
		*out = new(sharedtypes.ConfigSource)
		(*in).DeepCopyInto(*out)
	}
	if in.PortFrom != nil {
		in, out := &in.PortFrom, &out.PortFrom
		*out = new(sharedtypes.ConfigSource)
		(*in).DeepCopyInto(*out)
	}
	if in.UsernameFrom != nil {
		in, out := &in.UsernameFrom, &out.UsernameFrom
		*out = new(sharedtypes.ConfigSource)
		(*in).DeepCopyInto(*out)
	}
	if in.PasswordFrom != nil {
		in, out := &in.PasswordFrom, &out.PasswordFrom
		*out = new(sharedtypes.ConfigSource)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MongoDBConfig.
func (in *MongoDBConfig) DeepCopy() *MongoDBConfig {
	if in == nil {
		return nil
	}
	out := new(MongoDBConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Payments) DeepCopyInto(out *Payments) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Payments.
func (in *Payments) DeepCopy() *Payments {
	if in == nil {
		return nil
	}
	out := new(Payments)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Payments) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PaymentsList) DeepCopyInto(out *PaymentsList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Payments, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PaymentsList.
func (in *PaymentsList) DeepCopy() *PaymentsList {
	if in == nil {
		return nil
	}
	out := new(PaymentsList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *PaymentsList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PaymentsSpec) DeepCopyInto(out *PaymentsSpec) {
	*out = *in
	in.ImageHolder.DeepCopyInto(&out.ImageHolder)
	if in.Ingress != nil {
		in, out := &in.Ingress, &out.Ingress
		*out = new(sharedtypes.IngressSpec)
		(*in).DeepCopyInto(*out)
	}
	if in.Auth != nil {
		in, out := &in.Auth, &out.Auth
		*out = new(sharedtypes.AuthConfigSpec)
		(*in).DeepCopyInto(*out)
	}
	if in.Monitoring != nil {
		in, out := &in.Monitoring, &out.Monitoring
		*out = new(sharedtypes.MonitoringSpec)
		(*in).DeepCopyInto(*out)
	}
	if in.Collector != nil {
		in, out := &in.Collector, &out.Collector
		*out = new(CollectorConfig)
		(*in).DeepCopyInto(*out)
	}
	in.MongoDB.DeepCopyInto(&out.MongoDB)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PaymentsSpec.
func (in *PaymentsSpec) DeepCopy() *PaymentsSpec {
	if in == nil {
		return nil
	}
	out := new(PaymentsSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PostgresConfigCreateDatabase) DeepCopyInto(out *PostgresConfigCreateDatabase) {
	*out = *in
	in.PostgresConfigWithDatabase.DeepCopyInto(&out.PostgresConfigWithDatabase)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PostgresConfigCreateDatabase.
func (in *PostgresConfigCreateDatabase) DeepCopy() *PostgresConfigCreateDatabase {
	if in == nil {
		return nil
	}
	out := new(PostgresConfigCreateDatabase)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Search) DeepCopyInto(out *Search) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Search.
func (in *Search) DeepCopy() *Search {
	if in == nil {
		return nil
	}
	out := new(Search)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Search) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SearchIngester) DeepCopyInto(out *SearchIngester) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SearchIngester.
func (in *SearchIngester) DeepCopy() *SearchIngester {
	if in == nil {
		return nil
	}
	out := new(SearchIngester)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *SearchIngester) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SearchIngesterList) DeepCopyInto(out *SearchIngesterList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]SearchIngester, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SearchIngesterList.
func (in *SearchIngesterList) DeepCopy() *SearchIngesterList {
	if in == nil {
		return nil
	}
	out := new(SearchIngesterList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *SearchIngesterList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SearchIngesterSpec) DeepCopyInto(out *SearchIngesterSpec) {
	*out = *in
	if in.Pipeline != nil {
		in, out := &in.Pipeline, &out.Pipeline
		*out = make(json.RawMessage, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SearchIngesterSpec.
func (in *SearchIngesterSpec) DeepCopy() *SearchIngesterSpec {
	if in == nil {
		return nil
	}
	out := new(SearchIngesterSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SearchList) DeepCopyInto(out *SearchList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Search, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SearchList.
func (in *SearchList) DeepCopy() *SearchList {
	if in == nil {
		return nil
	}
	out := new(SearchList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *SearchList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SearchSpec) DeepCopyInto(out *SearchSpec) {
	*out = *in
	in.Scalable.DeepCopyInto(&out.Scalable)
	in.ImageHolder.DeepCopyInto(&out.ImageHolder)
	if in.Ingress != nil {
		in, out := &in.Ingress, &out.Ingress
		*out = new(sharedtypes.IngressSpec)
		(*in).DeepCopyInto(*out)
	}
	if in.Auth != nil {
		in, out := &in.Auth, &out.Auth
		*out = new(sharedtypes.AuthConfigSpec)
		(*in).DeepCopyInto(*out)
	}
	if in.Monitoring != nil {
		in, out := &in.Monitoring, &out.Monitoring
		*out = new(sharedtypes.MonitoringSpec)
		(*in).DeepCopyInto(*out)
	}
	in.ElasticSearch.DeepCopyInto(&out.ElasticSearch)
	in.KafkaConfig.DeepCopyInto(&out.KafkaConfig)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SearchSpec.
func (in *SearchSpec) DeepCopy() *SearchSpec {
	if in == nil {
		return nil
	}
	out := new(SearchSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Webhooks) DeepCopyInto(out *Webhooks) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Webhooks.
func (in *Webhooks) DeepCopy() *Webhooks {
	if in == nil {
		return nil
	}
	out := new(Webhooks)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Webhooks) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *WebhooksList) DeepCopyInto(out *WebhooksList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Webhooks, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new WebhooksList.
func (in *WebhooksList) DeepCopy() *WebhooksList {
	if in == nil {
		return nil
	}
	out := new(WebhooksList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *WebhooksList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *WebhooksSpec) DeepCopyInto(out *WebhooksSpec) {
	*out = *in
	in.ImageHolder.DeepCopyInto(&out.ImageHolder)
	if in.Ingress != nil {
		in, out := &in.Ingress, &out.Ingress
		*out = new(sharedtypes.IngressSpec)
		(*in).DeepCopyInto(*out)
	}
	if in.Auth != nil {
		in, out := &in.Auth, &out.Auth
		*out = new(sharedtypes.AuthConfigSpec)
		(*in).DeepCopyInto(*out)
	}
	if in.Monitoring != nil {
		in, out := &in.Monitoring, &out.Monitoring
		*out = new(sharedtypes.MonitoringSpec)
		(*in).DeepCopyInto(*out)
	}
	if in.Collector != nil {
		in, out := &in.Collector, &out.Collector
		*out = new(CollectorConfig)
		(*in).DeepCopyInto(*out)
	}
	in.MongoDB.DeepCopyInto(&out.MongoDB)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new WebhooksSpec.
func (in *WebhooksSpec) DeepCopy() *WebhooksSpec {
	if in == nil {
		return nil
	}
	out := new(WebhooksSpec)
	in.DeepCopyInto(out)
	return out
}
