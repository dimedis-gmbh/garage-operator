# https://medium.com/@charled.breteche/manage-ssl-certificates-for-local-kubernetes-clusters-with-cert-manager-9037ba39c799

resource "helm_release" "cert-manager" {
  name       = "cert-manager"
  repository = "https://charts.jetstack.io"
  chart      = "cert-manager"
  namespace  = "cert-manager"
  values     = [file("yaml/k8s-base/helm-cert-manager.yml")]
  create_namespace = true
}

resource "kubernetes_secret" "cert-manager-root-ca" {
  depends_on = [helm_release.cert-manager]

  metadata {
    name      = "root-ca"
    namespace = "cert-manager"
  }

  data = {
    "tls.crt" = file("../infra/.ssl/root-ca.pem")
    "tls.key" = file("../infra/.ssl/root-ca-key.pem")
  }

  type = "kubernetes.io/tls"
}

resource "null_resource" "cert-manager-cluster-issuer" {
  depends_on = [kubernetes_secret.cert-manager-root-ca]
  provisioner "local-exec" {
    command = <<EOT
cat <<EOF | kubectl create -f -
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-prod
  namespace: cert-manager
spec:
  ca:
    secretName: root-ca
EOF
EOT
  }
}
