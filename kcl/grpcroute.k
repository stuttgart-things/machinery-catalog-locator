"""
GRPCRoute (Gateway API) for machinery-catalog-locator
"""

import labels

config = labels.config

_hostname = config.grpcRouteHostname
_parentRefName = config.grpcRouteParentRefName or config.httpRouteParentRefName
_parentRefNamespace = config.grpcRouteParentRefNamespace or config.httpRouteParentRefNamespace

grpcroute = {
    apiVersion: "gateway.networking.k8s.io/v1"
    kind: "GRPCRoute"
    metadata: {
        name: config.name
        namespace: config.namespace
        labels: labels.commonLabels
        if config.annotations or config.grpcRouteAnnotations:
            annotations: {
                **config.annotations
                **config.grpcRouteAnnotations
            }
    }
    spec: {
        parentRefs: [
            {
                name: _parentRefName
                if _parentRefNamespace:
                    namespace: _parentRefNamespace
            }
        ]
        if _hostname:
            hostnames: [_hostname]
        rules: [
            {
                backendRefs: [
                    {
                        name: config.name
                        port: config.grpcPort
                    }
                ]
            }
        ]
    }
} if config.grpcRouteEnabled else None
