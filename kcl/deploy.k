"""
Deployment for machinery-catalog-locator
"""

import labels

config = labels.config

deployment = {
    apiVersion: "apps/v1"
    kind: "Deployment"
    metadata: {
        name: config.name
        namespace: config.namespace
        labels: labels.commonLabels
    }
    spec: {
        replicas: config.replicas
        selector: {
            matchLabels: labels.selectorLabels
        }
        strategy: {
            type: "RollingUpdate"
            rollingUpdate: {
                maxSurge: 1
                maxUnavailable: 0
            }
        }
        template: {
            metadata: {
                labels: labels.commonLabels
            }
            spec: {
                serviceAccountName: config.name
                containers: [
                    {
                        name: config.name
                        image: config.image
                        imagePullPolicy: config.imagePullPolicy
                        ports: [
                            {
                                name: "grpc"
                                containerPort: config.grpcPort
                                protocol: "TCP"
                            }
                        ] + ([
                            {
                                name: "http"
                                containerPort: config.httpPort
                                protocol: "TCP"
                            }
                        ] if config.httpEnabled else [])
                        env: [
                            # PAT fallback — sourced from a Secret, never the ConfigMap.
                            if config.githubTokenSecret:
                                {
                                    name: "GITHUB_TOKEN"
                                    valueFrom: {
                                        secretKeyRef: {
                                            name: config.githubTokenSecret
                                            key: config.githubTokenSecretKey
                                        }
                                    }
                                }
                        ]
                        envFrom: [
                            {
                                configMapRef: {
                                    name: "{}-config".format(config.name)
                                }
                            }
                        ]
                        resources: {
                            requests: {
                                cpu: config.cpuRequest
                                memory: config.memoryRequest
                            }
                            limits: {
                                cpu: config.cpuLimit
                                memory: config.memoryLimit
                            }
                        }
                        livenessProbe: {
                            grpc: {
                                port: config.grpcPort
                            }
                            initialDelaySeconds: 10
                            periodSeconds: 10
                            timeoutSeconds: 5
                            failureThreshold: 3
                        }
                        readinessProbe: {
                            grpc: {
                                port: config.grpcPort
                            }
                            initialDelaySeconds: 5
                            periodSeconds: 5
                            timeoutSeconds: 3
                            failureThreshold: 3
                        }
                        securityContext: {
                            readOnlyRootFilesystem: True
                            runAsNonRoot: True
                            allowPrivilegeEscalation: False
                            capabilities: {
                                drop: ["ALL"]
                            }
                        }
                        volumeMounts: [
                            {
                                name: "tmp"
                                mountPath: "/tmp"
                            }
                        ] + ([
                            {
                                name: "github-app-key"
                                mountPath: config.githubPrivateKeyMountPath
                                subPath: "private-key.pem"
                                readOnly: True
                            }
                        ] if config.githubPrivateKeySecret else [])
                    }
                ]
                securityContext: {
                    runAsNonRoot: True
                    runAsUser: 65532
                    runAsGroup: 65532
                    fsGroup: 65532
                    seccompProfile: {
                        type: "RuntimeDefault"
                    }
                }
                volumes: [
                    {
                        name: "tmp"
                        emptyDir: {}
                    }
                ] + ([
                    {
                        name: "github-app-key"
                        secret: {
                            secretName: config.githubPrivateKeySecret
                            items: [
                                {
                                    key: config.githubPrivateKeySecretKey
                                    path: "private-key.pem"
                                }
                            ]
                        }
                    }
                ] if config.githubPrivateKeySecret else [])
                affinity: {
                    podAntiAffinity: {
                        preferredDuringSchedulingIgnoredDuringExecution: [
                            {
                                weight: 100
                                podAffinityTerm: {
                                    labelSelector: {
                                        matchLabels: labels.selectorLabels
                                    }
                                    topologyKey: "kubernetes.io/hostname"
                                }
                            }
                        ]
                    }
                }
            }
        }
    }
}
