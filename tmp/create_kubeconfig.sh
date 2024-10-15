#!/bin/bash

server=https://k8s-dev01.service.ams1o.consul:6443
name=admin-serviceaccount-token
ns=crossplane-system

ca=$(kubectl get secret $name -n $ns -o jsonpath='{.data.ca\.crt}')
token=$(kubectl get secret/$name -n $ns -o jsonpath='{.data.token}' | base64 --decode)
namespace=$(kubectl get secret/$name -n $ns -o jsonpath='{.data.namespace}' | base64 --decode)

echo "
apiVersion: v1
kind: Config
clusters:
- name: default-cluster
  cluster:
    certificate-authority-data: ${ca}
    server: ${server}
contexts:
- name: default-context@a
  context:
    cluster: default-cluster
    namespace: ${ns}
    user: default-user
current-context: default-context@a
users:
- name: default-user
  user:
    token: ${token}
" > sa.kubeconfig
