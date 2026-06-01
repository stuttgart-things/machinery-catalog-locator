"""
Main entry point for machinery-catalog-locator Kubernetes deployment
Imports all resources and exports manifests
"""

import namespace
import serviceaccount
import configmap
import deploy
import service
import httproute
import grpcroute

# Export all manifests (filter out None values)
manifests = [
    m for m in [
        namespace.namespace
        serviceaccount.serviceAccount
        configmap.envConfigMap
        deploy.deployment
        service.service
        httproute.httproute
        grpcroute.grpcroute
    ] if m
]
