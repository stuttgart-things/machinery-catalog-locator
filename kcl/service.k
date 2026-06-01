"""
Service for machinery-catalog-locator
"""

import labels

config = labels.config

service = {
    apiVersion: "v1"
    kind: "Service"
    metadata: {
        name: config.name
        namespace: config.namespace
        labels: labels.commonLabels
    }
    spec: {
        type: config.serviceType
        ports: [
            {
                name: "grpc"
                port: config.grpcPort
                targetPort: "grpc"
                protocol: "TCP"
                # Tells the Gateway the backend speaks HTTP/2 cleartext, so
                # gRPC is routed correctly once TLS is terminated at the edge.
                appProtocol: "kubernetes.io/h2c"
            }
        ] + ([
            {
                name: "http"
                port: config.httpPort
                targetPort: "http"
                protocol: "TCP"
            }
        ] if config.httpEnabled else [])
        selector: labels.selectorLabels
    }
}
