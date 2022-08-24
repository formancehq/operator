# formance-operator

This operator is in charge of deploying a full or partial Formance OSS Stack.
It aims to simplify deployment and releases management of different parts of the Formance ecosystem.

## Getting Started

You’ll need a Kubernetes cluster to run against. 
Scripts of this repository are using [KIND](https://sigs.k8s.io/kind). You have to install it.

### Running on the cluster
1. Create the cluster:

```sh
task cluster:create
```

2. Deploy required resources:
	
```sh
task resources:install
```

This will automatically install all the required services by the stack :
* ElasticSearch
* MongoDB
* PostgreSQL
* RedPanda
* Traefik
* ...
	
3. Deploy the controller to the cluster:

```sh
task operator:deploy
```

### Uninstall CRDs
To delete the CRDs from the cluster:

```sh
task operator:uninstall
```

### Undeploy controller
UnDeploy the controller to the cluster:

```sh
task operator:undeploy
```

### How it works
This project aims to follow the Kubernetes [Operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)

It uses [Controllers](https://kubernetes.io/docs/concepts/architecture/controller/) 
which provides a reconcile function responsible for synchronizing resources until the desired state is reached on the cluster 

### Test It Out

You can install a full stack using the command:
```sh
kubectl apply -f example.yaml
```

## License

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

