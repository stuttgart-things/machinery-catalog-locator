"""
ConfigMap for machinery-catalog-locator

envConfigMap holds the non-secret scalar env vars consumed via envFrom
(ports, log level, git identity, and the GitHub App IDs). Unlike machinery,
catalog-locator reads Git rather than the cluster API, so there is no
cluster-watch JSON config file here — secret material (GITHUB_TOKEN, the
GitHub App PEM) is wired straight into the Pod from Secrets in deploy.k.
"""

import labels

config = labels.config

envConfigMap = {
    apiVersion: "v1"
    kind: "ConfigMap"
    metadata: {
        name: "{}-config".format(config.name)
        namespace: config.namespace
        labels: labels.commonLabels
    }
    data: {
        LOG_LEVEL: "info"
        GRPC_PORT: str(config.grpcPort)
        HTTP_PORT: str(config.httpPort)
        GIT_AUTHOR_NAME: config.gitAuthorName
        GIT_AUTHOR_EMAIL: config.gitAuthorEmail
        if config.githubAppID:
            GITHUB_APP_ID: config.githubAppID
        if config.githubInstallationID:
            GITHUB_INSTALLATION_ID: config.githubInstallationID
        if config.githubPrivateKeySecret:
            GITHUB_PRIVATE_KEY_PATH: config.githubPrivateKeyMountPath
        **config.extraEnvVars
    }
}
