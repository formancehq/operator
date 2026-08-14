/*
Copyright 2023.

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

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DatabaseSpec struct {
	StackDependency `json:",inline"`
	// Service is a discriminator for the created database.
	// In practice, it is the module name (ledger, payments...).
	// Therefore, the created database will be named `<stack-name>-<service>`
	Service string `json:"service"`
	// +kubebuilder:default:=false
	Debug bool `json:"debug,omitempty"`
}

type DatabaseStatus struct {
	Status `json:",inline"`
	//+optional
	URI *URI `json:"uri,omitempty"`
	//+optional
	// The generated database name
	Database string `json:"database,omitempty"`
	//+optional
	// OutOfSync indicates that a setting changed the URI of the postgres server
	// The Database object must be removed so that it can be recreated
	OutOfSync bool `json:"outOfSync,omitempty"`
}

// Database represents a concrete database on a PostgreSQL server. Modules that require a database create it ([Ledger](#ledger), for example).
//
// It uses the settings `postgres.<module-name>.uri` which must have the following URI format: `postgresql://[<username>:<password>@]<host>[:<port>]`.
// The database name is not part of the setting: the operator derives it from the stack and the service.
// Additionally, the URI can define a query param `secret` indicating a k8s secret that must be used to retrieve database credentials.
// Credentials in the secret are expected to be URL-encoded by default. Set `secretCredentialsEncoding=raw` to let the operator encode them.
//
// On creation, the reconciler behind the Database object will create the database on the postgresql server using a k8s job.
// On Deletion, by default, the reconciler leaves the database untouched.
// You can allow the reconciler to drop the database on the server by using the [Settings](#settings) `clear-database` with the value `true`.
// If you use that setting, the reconciler will use another job to drop the database.
// Be careful: no backup is performed!
//
// Database resource honors `aws.service-account` setting, so, you can create databases on an AWS server if you need.
// See [AWS accounts](#aws-account)
//
// Once a database is fully configured, it retains the postgres URI used.
// If the setting that specifies the server URI changes, the Database object will set the field `.status.outOfSync` to true
// and will not change anything.
//
// Therefore, to switch to a new server, you must change the setting value, then drop the Database object.
// It will be recreated with the correct URI.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Stack",type=string,JSONPath=".spec.stack",description="Stack"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.ready",description="Ready"
// +kubebuilder:printcolumn:name="Out of sync",type=string,JSONPath=".status.outOfSync",description="Is the databse configuration out of sync"
// +kubebuilder:printcolumn:name="Info",type=string,JSONPath=".status.info",description="Info"
type Database struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DatabaseSpec   `json:"spec,omitempty"`
	Status DatabaseStatus `json:"status,omitempty"`
}

func (in *Database) SetReady(b bool) {
	in.Status.SetReady(b)
}

func (in *Database) IsReady() bool {
	return in.Status.Ready
}

func (in *Database) SetError(s string) {
	in.Status.SetError(s)
}

func (a *Database) GetStack() string {
	return a.Spec.Stack
}

func (a *Database) isResource() {}

func (in *Database) GetConditions() *Conditions {
	return &in.Status.Conditions
}

//+kubebuilder:object:root=true

// DatabaseList contains a list of Database
type DatabaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Database `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Database{}, &DatabaseList{})
}
