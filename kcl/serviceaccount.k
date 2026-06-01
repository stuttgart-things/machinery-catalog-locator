"""
ServiceAccount for machinery-catalog-locator

catalog-locator reads Git, not the cluster API, so there is no ClusterRole /
ClusterRoleBinding here (machinery's cluster-read RBAC is dropped).
"""

import labels

config = labels.config

serviceAccount = {
    apiVersion: "v1"
    kind: "ServiceAccount"
    metadata: {
        name: config.name
        namespace: config.namespace
        labels: labels.commonLabels
    }
}
