# Building RESTful API using Kubernetes

## Init kubebuilder projects
```
$ kubebuilder init --domain johnliu55.tw --repo johnliu55.tw/todo
```

## Create API using kubebuilder
```
$ kubebuilder create api --group todo --version v1 --kind Todo
```

- Create Resource: y
- Create Controller: y

## Update CRD scheme
