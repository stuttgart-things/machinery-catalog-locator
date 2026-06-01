"""
Common labels for machinery-catalog-locator resources
"""

import schema

# Get command-line options
_image = option("config.image")
_namespace = option("config.namespace")
_grpcPort = option("config.grpcPort")
_httpPort = option("config.httpPort")
_httpEnabled = option("config.httpEnabled")
_httpRouteEnabled = option("config.httpRouteEnabled")
_httpRouteParentRefName = option("config.httpRouteParentRefName")
_httpRouteParentRefNamespace = option("config.httpRouteParentRefNamespace")
_httpRouteHostname = option("config.httpRouteHostname")
_httpRouteAnnotations = option("config.httpRouteAnnotations") or {}
_grpcRouteEnabled = option("config.grpcRouteEnabled")
_grpcRouteParentRefName = option("config.grpcRouteParentRefName")
_grpcRouteParentRefNamespace = option("config.grpcRouteParentRefNamespace")
_grpcRouteHostname = option("config.grpcRouteHostname")
_grpcRouteAnnotations = option("config.grpcRouteAnnotations") or {}
_githubAppID = option("config.githubAppID")
_githubInstallationID = option("config.githubInstallationID")
_githubPrivateKeySecret = option("config.githubPrivateKeySecret")
_githubPrivateKeySecretKey = option("config.githubPrivateKeySecretKey")
_githubPrivateKeyMountPath = option("config.githubPrivateKeyMountPath")
_githubTokenSecret = option("config.githubTokenSecret")
_githubTokenSecretKey = option("config.githubTokenSecretKey")
_gitAuthorName = option("config.gitAuthorName")
_gitAuthorEmail = option("config.gitAuthorEmail")
_extraEnvVars = option("config.extraEnvVars") or {}

# Config instance with command-line overrides
config: schema.CatalogLocator = schema.CatalogLocator {
    if _image:
        image = _image
    if _namespace:
        namespace = _namespace
    if _grpcPort:
        grpcPort = int(_grpcPort)
    if _httpPort:
        httpPort = int(_httpPort)
    if _httpEnabled:
        httpEnabled = _httpEnabled in [True, "True", "true"]
    if _httpRouteEnabled:
        httpRouteEnabled = _httpRouteEnabled in [True, "True", "true"]
    if _httpRouteParentRefName:
        httpRouteParentRefName = _httpRouteParentRefName
    if _httpRouteParentRefNamespace:
        httpRouteParentRefNamespace = _httpRouteParentRefNamespace
    if _httpRouteHostname:
        httpRouteHostname = _httpRouteHostname
    if _httpRouteAnnotations:
        httpRouteAnnotations = _httpRouteAnnotations
    if _grpcRouteEnabled:
        grpcRouteEnabled = _grpcRouteEnabled in [True, "True", "true"]
    if _grpcRouteParentRefName:
        grpcRouteParentRefName = _grpcRouteParentRefName
    if _grpcRouteParentRefNamespace:
        grpcRouteParentRefNamespace = _grpcRouteParentRefNamespace
    if _grpcRouteHostname:
        grpcRouteHostname = _grpcRouteHostname
    if _grpcRouteAnnotations:
        grpcRouteAnnotations = _grpcRouteAnnotations
    if _githubAppID:
        githubAppID = _githubAppID
    if _githubInstallationID:
        githubInstallationID = _githubInstallationID
    if _githubPrivateKeySecret:
        githubPrivateKeySecret = _githubPrivateKeySecret
    if _githubPrivateKeySecretKey:
        githubPrivateKeySecretKey = _githubPrivateKeySecretKey
    if _githubPrivateKeyMountPath:
        githubPrivateKeyMountPath = _githubPrivateKeyMountPath
    if _githubTokenSecret:
        githubTokenSecret = _githubTokenSecret
    if _githubTokenSecretKey:
        githubTokenSecretKey = _githubTokenSecretKey
    if _gitAuthorName:
        gitAuthorName = _gitAuthorName
    if _gitAuthorEmail:
        gitAuthorEmail = _gitAuthorEmail
    if _extraEnvVars:
        extraEnvVars = _extraEnvVars
}

# Common labels applied to all resources
commonLabels = {
    "app.kubernetes.io/name": config.name
    "app.kubernetes.io/instance": config.name
    "app.kubernetes.io/component": "grpc-server"
    "app.kubernetes.io/part-of": "stuttgart-things"
    **config.labels
}

# Selector labels for pods
selectorLabels = {
    "app.kubernetes.io/name": config.name
    "app.kubernetes.io/instance": config.name
}
