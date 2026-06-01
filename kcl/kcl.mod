[package]
name = "deploy-machinery-catalog-locator"
version = "0.1.0"
description = "KCL module for deploying machinery-catalog-locator gRPC + HTMX server on Kubernetes"

[dependencies]
k8s = "1.31"

[profile]
entries = [
    "main.k"
]
