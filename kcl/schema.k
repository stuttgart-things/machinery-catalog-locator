"""
Schema definition for machinery-catalog-locator deployment configuration
"""

schema CatalogLocator:
    """Configuration for machinery-catalog-locator Kubernetes deployment"""

    # Metadata
    name: str = "machinery-catalog-locator"
    namespace: str = "machinery-catalog-locator"

    # Image
    image: str = "ghcr.io/stuttgart-things/machinery-catalog-locator:latest"
    imagePullPolicy: str = "IfNotPresent"

    # Replicas and resources
    replicas: int = 1
    cpuRequest: str = "100m"
    cpuLimit: str = "500m"
    memoryRequest: str = "128Mi"
    memoryLimit: str = "512Mi"

    # gRPC server
    grpcPort: int = 50051

    # HTTP server (HTMX dashboard) — catalog-locator serves it by default.
    httpPort: int = 8080
    httpEnabled: bool = True

    # Service
    serviceType: str = "ClusterIP"

    # HTTPRoute (Gateway API)
    httpRouteEnabled: bool = False
    httpRouteParentRefName: str = ""
    httpRouteParentRefNamespace: str = ""
    httpRouteHostname: str = ""
    httpRouteAnnotations: {str:str} = {}

    # GRPCRoute (Gateway API)
    grpcRouteEnabled: bool = False
    grpcRouteParentRefName: str = ""
    grpcRouteParentRefNamespace: str = ""
    grpcRouteHostname: str = ""
    grpcRouteAnnotations: {str:str} = {}

    # GitHub access — catalog-locator reads Git, not the cluster API
    # (see internal/config/config.go). Two mutually-exclusive credential
    # paths, both materialized as Secrets externally (ESO / Kyverno /
    # kubectl create secret); this schema only wires them into the Pod.
    #
    # GitHub App (preferred): set githubAppID + githubInstallationID and
    # mount the PEM via githubPrivateKeySecret. The key is surfaced to the
    # binary as GITHUB_PRIVATE_KEY_PATH (= githubPrivateKeyMountPath).
    githubAppID: str = ""
    githubInstallationID: str = ""
    githubPrivateKeySecret: str = ""
    githubPrivateKeySecretKey: str = "private-key.pem"
    githubPrivateKeyMountPath: str = "/var/run/github-app/private-key.pem"

    # PAT fallback (dev): a Secret carrying the token, surfaced as
    # GITHUB_TOKEN. Leave githubTokenSecret empty to skip.
    githubTokenSecret: str = ""
    githubTokenSecretKey: str = "token"

    # Commit identity used when opening PRs (GIT_AUTHOR_NAME/EMAIL).
    gitAuthorName: str = "machinery-catalog-locator"
    gitAuthorEmail: str = "machinery-catalog-locator@local"

    # Extra environment variables
    extraEnvVars: {str:str} = {}

    # Labels and annotations
    labels: {str:str} = {}
    annotations: {str:str} = {}

    check:
        replicas >= 0, "replicas must be >= 0"
        grpcPort >= 1 and grpcPort <= 65535, "grpcPort must be valid"
        httpPort >= 1 and httpPort <= 65535, "httpPort must be valid"
        name != "", "name must not be empty"
        namespace != "", "namespace must not be empty"
