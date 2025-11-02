#!/bin/bash
set -e

if [ -f ../k8s/terraform.tfstate ]
then
  echo "* Removing Terraform state from k8s folder"
  rm -f ../k8s/terraform.tfstate*
fi

if [ -d $HOME/.local/share/mkcert -a ! -f .ssl/root-ca-key.pem ]
then
  echo "* Using existing mkcert root CA certificate"
  mkdir -p .ssl
  cp $HOME/.local/share/mkcert/rootCA-key.pem .ssl/root-ca-key.pem
  cp $HOME/.local/share/mkcert/rootCA.pem .ssl/root-ca.pem
fi

if [ ! -f .ssl/root-ca.pem ]
then
  echo "* Generating SSL certificate"
  mkdir -p .ssl
  openssl genrsa -out .ssl/root-ca-key.pem 2048 
  openssl req -x509 -new -nodes -key .ssl/root-ca-key.pem \
    -days 3650 -sha256 -out .ssl/root-ca.pem -subj "/CN=kube-ca"

  echo "* Installing SSL certificate to Linux trust store (requires sudo)"
  sudo cp -a root-ca.pem /usr/local/share/ca-certificates/kind-root-ca.crt
  sudo update-ca-certificates
  
  echo "* Installing SSL certificate to browser trust store"
  certutil -d sql:$HOME/.pki/nssdb -A -t "C,," -n kind-root-ca -i .ssl/root-ca.pem
fi

kind create cluster \
  --config=yaml/cluster.yaml \
  --kubeconfig=../${KIND_CLUSTER_NAME}_kubeconfig.yaml

echo "* Waiting for all nodes to be ready..."
kubectl wait --for=condition=ready nodes --all --timeout=300s

# For Kind Ingress refer to: https://kind.sigs.k8s.io/docs/user/ingress/
#
# Used https://kind.sigs.k8s.io/examples/ingress/deploy-ingress-nginx.yaml
# and patched for Controller & both Jobs
# - nodeSelector: ingress-ready: "true"
# - tolerations: for Jobs as for Controller (to run on control plane)

echo
echo "* Applying Ingress configuration"
kubectl apply -f yaml/deploy-ingress-nginx.yaml

echo    "* Waiting on Ingress to be ready ..."
echo -n "  [-] "
kubectl wait --namespace ingress-nginx \
  --for=condition=ready pod \
  --selector=app.kubernetes.io/component=controller \
  --timeout=90s
