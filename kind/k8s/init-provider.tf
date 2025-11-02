terraform {
  required_version = ">= 1.6.1"

  required_providers {
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "2.31.0"
    }

    helm = {
      source  = "hashicorp/helm"
      version = "2.9.0"
    }
  }
}

provider "kubernetes" {
  config_path = "../${var.cluster_name}_kubeconfig.yaml"
}

provider "helm" {
  kubernetes {
    config_path = "../${var.cluster_name}_kubeconfig.yaml"
  }
}
